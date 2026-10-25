package protocol_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"goodsentry/internal/protocol"
)

// These tests own the SDK wire boundary: independently specified framing,
// compression limits and credential consistency. They introduce no test seams.
func TestEnvelopeFraming(t *testing.T) {
	binary := []byte{0, 255, '\n', 'x', '\r', '\n'}
	wire := append([]byte("{\"dsn\":\"https://key@example.test/prefix/project\",\"future\":{\"v\":2}}\n{\"type\":\"future_item\",\"length\":6,\"extra\":true}\n"), binary...)
	wire = append(wire, []byte("\n{\"type\":\"attachment\",\"length\":0}\n\n{\"type\":\"event\"}\n{\"message\":\"café\"}")...)
	for _, terminal := range []string{"", "\n"} {
		t.Run("binary and unknown items finalLF="+strconvBool(terminal != ""), func(t *testing.T) {
			env, err := protocol.ParseEnvelope(append(append([]byte(nil), wire...), terminal...), protocol.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if len(env.Items) != 3 || !bytes.Equal(env.Items[0].Payload, binary) || len(env.Items[1].Payload) != 0 || string(env.Items[2].Payload) != `{"message":"café"}` {
				t.Fatalf("items corrupted: %#v", env.Items)
			}
			if env.Items[0].Type != "future_item" || string(env.Items[0].Header["extra"]) != "true" || string(env.Header["future"]) != `{"v":2}` {
				t.Fatalf("unknown metadata lost: %#v", env)
			}
		})
	}
	for _, wire := range []string{"{}", "{}\n", "{}\n{\"type\":\"attachment\",\"length\":0}\n", "{}\n{\"type\":\"attachment\"}\n"} {
		if _, err := protocol.ParseEnvelope([]byte(wire), protocol.Limits{}); err != nil {
			t.Errorf("valid envelope %q: %v", wire, err)
		}
	}
	if env, err := protocol.ParseEnvelope([]byte("{}\n{\"type\":\"attachment\",\"length\":2}\né"), protocol.Limits{}); err != nil || string(env.Items[0].Payload) != "é" {
		t.Fatalf("length must count UTF-8 bytes: %#v %v", env, err)
	}
}

func TestEnvelopeRejectsMalformedFraming(t *testing.T) {
	for name, wire := range map[string]string{
		"empty": "", "not object": "[]", "padded header": " {}\n", "invalid UTF8": "{\"x\":\"\xff\"}\n", "duplicate identity": "{\"dsn\":\"a\",\"dsn\":\"b\"}\n", "multiline header": "{\n}\n", "missing payload separator": "{}\n{\"type\":\"event\"}", "missing type": "{}\n{}\n{}", "null type": "{}\n{\"type\":null}\n{}", "negative length": "{}\n{\"type\":\"event\",\"length\":-1}\n", "length overflow": "{}\n{\"type\":\"event\",\"length\":9223372036854775808}\n", "fraction length": "{}\n{\"type\":\"event\",\"length\":1.1}\na", "null length": "{}\n{\"type\":\"event\",\"length\":null}\n", "truncated": "{}\n{\"type\":\"event\",\"length\":3}\n{}", "trailing bytes": "{}\n{\"type\":\"event\",\"length\":2}\n{}x", "extra newline": "{}\n\n", "terminal whitespace": "{}\n{\"type\":\"event\"}\n{}\n ",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := protocol.ParseEnvelope([]byte(wire), protocol.Limits{})
			if !errors.Is(err, protocol.ErrMalformed) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestEnvelopeLimits(t *testing.T) {
	wire := []byte("{}\n{\"type\":\"attachment\",\"length\":3}\nabc\n{\"type\":\"event\"}\n{}")
	for name, limits := range map[string]protocol.Limits{
		"total bytes": {MaxDecompressed: int64(len(wire) - 1)}, "item count": {MaxItems: 1}, "item bytes": {MaxItemBytes: 2},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := protocol.ParseEnvelope(wire, limits)
			if !errors.Is(err, protocol.ErrTooLarge) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := protocol.ParseEnvelope(wire, protocol.Limits{MaxItems: 2, MaxDecompressed: int64(len(wire)), MaxItemBytes: 3}); err != nil {
		t.Fatalf("exact limits: %v", err)
	}
}

func TestDecodeHTTPCompression(t *testing.T) {
	wire := []byte("{}\n{\"type\":\"event\"}\n{\"message\":\"hello\"}")
	for _, encoding := range []string{"identity", "gzip", "deflate", "br", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			body := compressBody(t, encoding, wire)
			env, err := protocol.Decode(bytes.NewReader(body), encoding, false, protocol.Limits{MaxCompressed: int64(len(body)), MaxDecompressed: int64(len(wire))})
			if err != nil || len(env.Items) != 1 || string(env.Items[0].Payload) != `{"message":"hello"}` {
				t.Fatalf("decoded %#v: %v", env, err)
			}
			_, err = protocol.Decode(bytes.NewReader(body), encoding, false, protocol.Limits{MaxCompressed: int64(len(body) - 1)})
			if !errors.Is(err, protocol.ErrTooLarge) {
				t.Fatalf("compressed byte bound: %v", err)
			}
			_, err = protocol.Decode(bytes.NewReader(body), encoding, false, protocol.Limits{MaxDecompressed: int64(len(wire) - 1)})
			if !errors.Is(err, protocol.ErrTooLarge) {
				t.Fatalf("decompressed byte bound: %v", err)
			}
		})
	}
	if _, err := protocol.Decode(strings.NewReader("{}"), "snappy", false, protocol.Limits{}); !errors.Is(err, protocol.ErrEncoding) {
		t.Fatalf("unknown encoding: %v", err)
	}
	for _, encoding := range []string{"gzip", "deflate", "br", "zstd"} {
		body := compressBody(t, encoding, wire)
		if _, err := protocol.Decode(bytes.NewReader(body[:len(body)-1]), encoding, false, protocol.Limits{}); !errors.Is(err, protocol.ErrMalformed) {
			t.Errorf("truncated %s: %v", encoding, err)
		}
	}
}

func TestDecodeBoundsCompressionExpansionAndWindow(t *testing.T) {
	// A tiny compressed request may expand far past the configured body limit.
	wire := []byte("{}\n{\"type\":\"attachment\",\"length\":100000}\n" + strings.Repeat("x", 100000))
	for _, encoding := range []string{"gzip", "deflate", "br", "zstd"} {
		body := compressBody(t, encoding, wire)
		if _, err := protocol.Decode(bytes.NewReader(body), encoding, false, protocol.Limits{MaxDecompressed: 1024}); !errors.Is(err, protocol.ErrTooLarge) {
			t.Errorf("%s expansion: %v", encoding, err)
		}
	}
	// RFC 8878 frame: no content size, an 8 MiB window and an empty last block.
	windowFrame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x68, 0x01, 0x00, 0x00}
	if _, err := protocol.Decode(bytes.NewReader(windowFrame), "zstd", false, protocol.Limits{MaxDecompressed: 1024}); !errors.Is(err, protocol.ErrTooLarge) {
		t.Fatalf("decoder window limit: %v", err)
	}
}

func TestDecodeLegacyStore(t *testing.T) {
	raw := []byte("{\n\"event_id\":\"abc\",\"message\":\"old client\"\n}")
	encoded := base64.StdEncoding.EncodeToString(compressBody(t, "deflate", raw))
	for _, body := range []string{string(raw), encoded} {
		env, err := protocol.Decode(strings.NewReader(body), "", true, protocol.Limits{})
		if err != nil || len(env.Items) != 1 || env.Items[0].Type != "event" || !bytes.Equal(env.Items[0].Payload, raw) {
			t.Fatalf("legacy store lost event: %#v %v", env, err)
		}
	}
	if _, err := protocol.Decode(strings.NewReader(encoded), "", true, protocol.Limits{MaxDecompressed: int64(len(raw) - 1)}); !errors.Is(err, protocol.ErrTooLarge) {
		t.Fatalf("legacy decompression bound: %v", err)
	}
	for _, body := range []string{"[]", "null", "{broken}", "eJtruncated"} {
		if _, err := protocol.Decode(strings.NewReader(body), "", true, protocol.Limits{}); !errors.Is(err, protocol.ErrMalformed) {
			t.Errorf("legacy %q: %v", body, err)
		}
	}
}

func TestAuthenticateAllSuppliedIdentities(t *testing.T) {
	for _, tc := range []struct {
		name, path, auth, dsn string
		wantErr               bool
	}{
		{"header", "/prefix/api/42/envelope/", "Sentry sentry_version=7, sentry_key=public", "", false},
		{"query", "/prefix/api/42/envelope/?sentry_key=public&sentry_version=7", "", "", false},
		{"query missing version", "/prefix/api/42/envelope/?sentry_key=public", "", "", true},
		{"dsn prefix", "/prefix/api/42/envelope/", "", "https://public@example.test/prefix/42", false},
		{"all match", "/prefix/api/42/envelope/?sentry_key=public&sentry_version=7", "Sentry sentry_version=7, sentry_key=public", "https://public:oldsecret@example.test/prefix/42", false},
		{"absent", "/api/42/envelope/", "", "", true},
		{"wrong project", "/api/42/envelope/", "", "https://public@example.test/43", true},
		{"keys disagree", "/api/42/envelope/?sentry_key=other&sentry_version=7", "Sentry sentry_version=7, sentry_key=public", "", true},
		{"bad header valid dsn", "/api/42/envelope/", "broken", "https://public@example.test/42", true},
		{"missing version valid dsn", "/api/42/envelope/", "Sentry sentry_key=public", "https://public@example.test/42", true},
		{"bad version", "/api/42/envelope/", "Sentry sentry_version=8, sentry_key=public", "", true},
		{"empty key", "/api/42/envelope/?sentry_key=&sentry_version=7", "", "https://public@example.test/42", true},
		{"duplicate disagreement", "/api/42/envelope/?sentry_key=public&sentry_key=other&sentry_version=7", "", "", true},
		{"malformed query valid header", "/api/42/envelope/?sentry_key=%zz&sentry_version=7", "Sentry sentry_version=7, sentry_key=public", "", true},
		{"dsn missing key", "/api/42/envelope/", "Sentry sentry_version=7, sentry_key=public", "https://example.test/42", true},
		{"dsn missing host", "/api/42/envelope/", "", "https://public@/42", true},
		{"dsn missing project", "/api/42/envelope/", "", "https://public@example.test/", true},
		{"dsn query", "/api/42/envelope/", "", "https://public@example.test/42?other=1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, nil)
			if tc.auth != "" {
				r.Header.Set("X-Sentry-Auth", tc.auth)
			}
			header := map[string]json.RawMessage{}
			if tc.dsn != "" {
				header["dsn"], _ = json.Marshal(tc.dsn)
			}
			key, err := protocol.Authenticate(r, header, "42")
			if tc.wantErr {
				if !errors.Is(err, protocol.ErrAuth) {
					t.Fatalf("key=%q err=%v", key, err)
				}
			} else if err != nil || key != "public" {
				t.Fatalf("key=%q err=%v", key, err)
			}
		})
	}
	r := httptest.NewRequest("POST", "/api/42/envelope/", nil)
	r.Header.Add("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=public")
	r.Header.Add("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=other")
	if _, err := protocol.Authenticate(r, nil, "42"); !errors.Is(err, protocol.ErrAuth) {
		t.Fatalf("multiple header identities: %v", err)
	}
	for _, dsn := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`42`), json.RawMessage(`""`)} {
		r.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=public")
		if _, err := protocol.Authenticate(r, map[string]json.RawMessage{"dsn": dsn}, "42"); !errors.Is(err, protocol.ErrAuth) {
			t.Fatalf("invalid dsn with valid header: %s %v", dsn, err)
		}
	}
}

func TestAuthenticateMinidumpAllowsCrashpadQuery(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		wantErr    bool
	}{
		{"Crashpad query", "/api/42/minidump/?sentry_client=sentry.native/0.17.4&sentry_key=public", false},
		{"explicit supported version", "/api/42/minidump/?sentry_key=public&sentry_version=7", false},
		{"unsupported version", "/api/42/minidump/?sentry_key=public&sentry_version=8", true},
		{"missing key", "/api/42/minidump/?sentry_client=sentry.native/0.17.4", true},
		{"disagreeing keys", "/api/42/minidump/?sentry_key=public&sentry_key=other", true},
		{"malformed query", "/api/42/minidump/?sentry_key=%zz", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, nil)
			key, err := protocol.AuthenticateMinidump(r, "42")
			if tc.wantErr {
				if !errors.Is(err, protocol.ErrAuth) {
					t.Fatalf("key=%q err=%v", key, err)
				}
			} else if err != nil || key != "public" {
				t.Fatalf("key=%q err=%v", key, err)
			}
		})
	}
}

func FuzzParseEnvelope(f *testing.F) {
	for _, seed := range []string{"{}", "{}\n{\"type\":\"attachment\",\"length\":2}\n\x00\xff", "{}\n{\"type\":\"event\"}\n{}\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		env, err := protocol.ParseEnvelope(wire, protocol.Limits{MaxDecompressed: 4096, MaxItems: 8, MaxItemBytes: 1024})
		if err != nil {
			return
		}
		if len(env.Items) > 8 {
			t.Fatal("accepted excess items")
		}
		for _, item := range env.Items {
			if len(item.Payload) > 1024 {
				t.Fatal("accepted excess item bytes")
			}
		}
		// A successful parse retains payloads and headers under an independent
		// length-prefixed serialization, including binary and implicit items.
		canonical, _ := json.Marshal(env.Header)
		for _, item := range env.Items {
			h := map[string]json.RawMessage{}
			for k, v := range item.Header {
				h[k] = v
			}
			h["length"], _ = json.Marshal(len(item.Payload))
			line, _ := json.Marshal(h)
			canonical = append(canonical, '\n')
			canonical = append(canonical, line...)
			canonical = append(canonical, '\n')
			canonical = append(canonical, item.Payload...)
		}
		round, err := protocol.ParseEnvelope(canonical, protocol.Limits{MaxDecompressed: 1 << 20, MaxItems: 8, MaxItemBytes: 1024})
		if err != nil {
			t.Fatalf("canonical envelope rejected: %v", err)
		}
		if len(round.Items) != len(env.Items) {
			t.Fatal("lost items")
		}
		for i := range env.Items {
			if !bytes.Equal(round.Items[i].Payload, env.Items[i].Payload) || round.Items[i].Type != env.Items[i].Type {
				t.Fatal("lost payload or item identity")
			}
		}
	})
}

func compressBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	var writer io.WriteCloser
	switch encoding {
	case "identity":
		return body
	case "gzip":
		writer = gzip.NewWriter(&out)
	case "deflate":
		var err error
		writer, err = zlib.NewWriterLevel(&out, zlib.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
	case "br":
		writer = brotli.NewWriter(&out)
	case "zstd":
		var err error
		writer, err = zstd.NewWriter(&out)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func strconvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
