package protocol

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// Decode reads a bounded request body. Legacy store requests produce an
// envelope with one event item, preserving the submitted event JSON bytes.
func Decode(r io.Reader, contentEncoding string, legacy bool, limits Limits) (Envelope, error) {
	limits = limits.defaults()
	encoding := strings.ToLower(strings.TrimSpace(contentEncoding))
	switch encoding {
	case "", "identity", "gzip", "deflate", "br", "zstd":
	default:
		return Envelope{}, ErrEncoding
	}
	body, err := readBounded(r, limits.MaxCompressed)
	if err != nil {
		return Envelope{}, err
	}
	if encoding != "" && encoding != "identity" {
		body, err = decompress(body, encoding, limits.MaxDecompressed)
		if err != nil {
			return Envelope{}, err
		}
	}
	if !legacy {
		return ParseEnvelope(body, limits)
	}
	// Historical store clients send base64(zlib(JSON)) without a content
	// encoding. JSON objects can be recognized without interpreting fields.
	if (encoding == "" || encoding == "identity") && !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		compressed, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
		if decodeErr != nil {
			return Envelope{}, fmt.Errorf("%w: invalid legacy base64", ErrMalformed)
		}
		body, err = decompress(compressed, "deflate", limits.MaxDecompressed)
		if err != nil {
			return Envelope{}, err
		}
	}
	if int64(len(body)) > limits.MaxDecompressed || int64(len(body)) > limits.MaxItemBytes {
		return Envelope{}, ErrTooLarge
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(body) {
		return Envelope{}, fmt.Errorf("%w: store payload must be a JSON object", ErrMalformed)
	}
	return Envelope{Header: map[string]json.RawMessage{}, Items: []Item{{Header: map[string]json.RawMessage{"type": json.RawMessage(`"event"`)}, Type: "event", Payload: body}}}, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %w", ErrMalformed, err)
	}
	if int64(len(body)) == limit {
		var extra [1]byte
		if n, err := io.ReadFull(r, extra[:]); n > 0 {
			return nil, ErrTooLarge
		} else if err != io.EOF {
			return nil, fmt.Errorf("%w: read body: %w", ErrMalformed, err)
		}
	}
	return body, nil
}

func decompress(body []byte, encoding string, limit int64) ([]byte, error) {
	input := bytes.NewReader(body)
	var reader io.Reader
	var closeReader func()
	var err error
	switch encoding {
	case "gzip":
		var gz *gzip.Reader
		gz, err = gzip.NewReader(input)
		if err == nil {
			reader = gz
			closeReader = func() { _ = gz.Close() }
		}
	case "deflate":
		var zr io.ReadCloser
		zr, err = zlib.NewReader(input)
		if err == nil {
			reader = zr
			closeReader = func() { _ = zr.Close() }
		}
	case "br":
		reader = brotli.NewReader(input)
	case "zstd":
		// Limit the decoder's window allocation as well as its output. Small
		// bodies still need a working window; concurrency one bounds buffers.
		memory := uint64(max(limit, 1<<20))
		var decoder *zstd.Decoder
		decoder, err = zstd.NewReader(input, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(memory))
		if err == nil {
			reader = decoder
			closeReader = decoder.Close
		}
	}
	if err != nil {
		return nil, compressionError(err)
	}
	if closeReader != nil {
		defer closeReader()
	}
	decoded, err := readBounded(reader, limit)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, err
		}
		return nil, compressionError(err)
	}
	if input.Len() != 0 {
		return nil, fmt.Errorf("%w: trailing compressed bytes", ErrMalformed)
	}
	return decoded, nil
}

func compressionError(err error) error {
	if errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
		return ErrTooLarge
	}
	return fmt.Errorf("%w: invalid compressed body: %v", ErrMalformed, err)
}

// Authenticate validates every supplied SDK credential source and returns the
// agreed public key. The caller must authorize it against projectID in storage.
func Authenticate(r *http.Request, header map[string]json.RawMessage, projectID string) (string, error) {
	if projectID == "" {
		return "", ErrAuth
	}
	var agreed string
	accept := func(key string) error {
		if !validKey(key) || (agreed != "" && key != agreed) {
			return ErrAuth
		}
		agreed = key
		return nil
	}
	for _, value := range r.Header.Values("X-Sentry-Auth") {
		fields, err := parseAuthHeader(value)
		if err != nil {
			return "", err
		}
		key, err := authFields(fields)
		if err != nil {
			return "", err
		}
		if err := accept(key); err != nil {
			return "", err
		}
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", ErrAuth
	}
	queryAuth := false
	for name := range query {
		if strings.HasPrefix(name, "sentry_") {
			queryAuth = true
			break
		}
	}
	if queryAuth {
		key, err := authFields(query)
		if err != nil {
			return "", err
		}
		if err := accept(key); err != nil {
			return "", err
		}
	}
	if raw, exists := header["dsn"]; exists {
		var dsn string
		if err := json.Unmarshal(raw, &dsn); err != nil || dsn == "" {
			return "", ErrAuth
		}
		u, err := url.Parse(dsn)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User == nil || u.RawQuery != "" || u.Fragment != "" {
			return "", ErrAuth
		}
		// The final path segment is the project. Earlier segments belong to
		// the deployment prefix and must not be interpreted as project IDs.
		path := strings.TrimSuffix(u.EscapedPath(), "/")
		_, segment, found := strings.Cut(path, "/")
		if !found {
			return "", ErrAuth
		}
		if i := strings.LastIndexByte(segment, '/'); i >= 0 {
			segment = segment[i+1:]
		}
		project, err := url.PathUnescape(segment)
		if err != nil || project != projectID || strings.ContainsAny(project, "/\\") || project == "." || project == ".." {
			return "", ErrAuth
		}
		if err := accept(u.User.Username()); err != nil {
			return "", err
		}
	}
	if agreed == "" {
		return "", ErrAuth
	}
	return agreed, nil
}

func validKey(key string) bool {
	return key != "" && !strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == ',' || r == '=' })
}

func authFields(fields url.Values) (string, error) {
	keys, versions := fields["sentry_key"], fields["sentry_version"]
	if len(keys) == 0 || len(versions) == 0 {
		return "", ErrAuth
	}
	for _, key := range keys {
		if !validKey(key) || key != keys[0] {
			return "", ErrAuth
		}
	}
	for _, version := range versions {
		if version != "7" {
			return "", ErrAuth
		}
	}
	return keys[0], nil
}

func parseAuthHeader(value string) (url.Values, error) {
	scheme, params, found := strings.Cut(strings.TrimSpace(value), " ")
	if !found || !strings.EqualFold(scheme, "Sentry") {
		return nil, ErrAuth
	}
	fields := make(url.Values)
	// Split at commas outside quoted values so client names may be quoted.
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(params); i++ {
		if i < len(params) {
			if escaped {
				escaped = false
				continue
			}
			if quoted && params[i] == '\\' {
				escaped = true
				continue
			}
			if params[i] == '"' {
				quoted = !quoted
				continue
			}
			if params[i] != ',' || quoted {
				continue
			}
		}
		if quoted || escaped {
			return nil, ErrAuth
		}
		field := strings.TrimSpace(params[start:i])
		start = i + 1
		name, val, ok := strings.Cut(field, "=")
		name, val = strings.TrimSpace(name), strings.TrimSpace(val)
		if !ok || name == "" || val == "" {
			return nil, ErrAuth
		}
		if strings.HasPrefix(val, `"`) {
			var err error
			val, err = strconv.Unquote(val)
			if err != nil {
				return nil, ErrAuth
			}
		}
		fields.Add(name, val)
	}
	return fields, nil
}
