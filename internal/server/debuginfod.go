package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/native"
	"goodsentry/internal/nettarget"
)

// fetchSymbolServers uses only operator-configured sources and never persists
// the returned files. Local files and both protocols share one resource budget.
func (s *Server) fetchSymbolServers(ctx context.Context, modules []native.Module, local []native.DebugSymbolFile, symbols native.SymbolSet, unwind native.UnwindSet) []native.DebugSymbolFile {
	if (len(s.cfg.DebugInfodURLs) == 0 && len(s.cfg.SymStoreURLs) == 0) || len(local) >= 25 {
		return nil
	}
	remaining := int64(64 << 20)
	for _, file := range local {
		remaining -= int64(len(file.Content))
	}
	if remaining <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := func(allowPrivate bool) (*http.Client, *http.Transport) {
		transport := &http.Transport{DialContext: (nettarget.Dialer{AllowPrivate: allowPrivate}).DialContext, DisableCompression: true}
		return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport
	}
	linux, linuxTransport := client(s.cfg.DebugInfodAllowPrivate)
	windows, windowsTransport := client(s.cfg.SymStoreAllowPrivate)
	defer linuxTransport.CloseIdleConnections()
	defer windowsTransport.CloseIdleConnections()
	seen := make(map[string]bool)
	var fetched []native.DebugSymbolFile
	attempts := 0
	for _, module := range modules {
		var id, name, seenKey string
		var sources []string
		var requester *http.Client
		switch module.DebugFormat {
		case "elf":
			id = strings.ToLower(module.CodeID)
			if len(id) == 0 || len(id) > 128 || len(id)%2 != 0 {
				continue
			}
			if _, err := hex.DecodeString(id); err != nil {
				continue
			}
			name, seenKey, sources, requester = "debuginfo.elf", "elf:"+id, s.cfg.DebugInfodURLs, linux
		case "pe":
			id = module.DebugID
			name = module.DebugFile
			if len(id) < 33 || len(id) > 40 || !safeSymStoreName(name) {
				continue
			}
			if _, err := hex.DecodeString(id[:32]); err != nil {
				continue
			}
			age, err := strconv.ParseUint(id[32:], 16, 32)
			if err != nil {
				continue
			}
			id = strings.ToUpper(id[:32]) + fmt.Sprintf("%X", age)
			seenKey, sources, requester = "pe:"+id+":"+name, s.cfg.SymStoreURLs, windows
		default:
			continue
		}
		if len(sources) == 0 || seen[seenKey] {
			continue
		}
		seen[seenKey] = true
		key := native.DebugKey(id)
		if _, ok := symbols[key]; ok {
			continue
		}
		if _, ok := unwind[key]; ok {
			continue
		}
		for _, source := range sources {
			if ctx.Err() != nil || attempts >= 25 || remaining <= 0 || len(local)+len(fetched) >= 25 {
				return fetched
			}
			var target string
			var err error
			if module.DebugFormat == "elf" {
				target, err = url.JoinPath(source, "buildid", id, "debuginfo")
			} else {
				component := url.PathEscape(name)
				target, err = url.JoinPath(source, component, id, component)
			}
			if err != nil {
				continue
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				continue
			}
			attempts++
			resp, err := requester.Do(req)
			if err != nil {
				continue
			}
			if resp.StatusCode != http.StatusOK || resp.ContentLength > remaining {
				resp.Body.Close()
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, remaining+1))
			resp.Body.Close()
			if int64(len(data)) > remaining {
				return fetched
			}
			remaining -= int64(len(data))
			if readErr != nil || !symbolServerIdentityMatches(module.DebugFormat, name, id, data) {
				continue
			}
			fetched = append(fetched, native.DebugSymbolFile{Name: name, DebugID: id, Content: data})
			break
		}
	}
	return fetched
}

func safeSymStoreName(name string) bool {
	if name == "" || len(name) > 255 || name == "." || name == ".." || !utf8.ValidString(name) || strings.ContainsAny(name, `/\\:`) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func symbolServerIdentityMatches(format, name, id string, data []byte) bool {
	if format == "elf" {
		return len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) && strings.EqualFold(native.ObjectDebugID(data), id)
	}
	if !bytes.HasPrefix(data, []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS\x00\x00\x00")) {
		return false
	}
	objects := native.ParseDebugObjects(name, data)
	return len(objects) == 1 && native.DebugKey(objects[0].DebugID) == native.DebugKey(id)
}
