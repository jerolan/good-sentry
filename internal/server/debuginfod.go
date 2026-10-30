package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goodsentry/internal/native"
	"goodsentry/internal/nettarget"
)

// fetchDebuginfod uses only operator-configured sources and never persists the
// returned files. Its budget includes the debug files already loaded locally.
func (s *Server) fetchDebuginfod(ctx context.Context, modules []native.Module, local []native.DebugSymbolFile, symbols native.SymbolSet, unwind native.UnwindSet) []native.DebugSymbolFile {
	if len(s.cfg.DebugInfodURLs) == 0 || len(local) >= 25 {
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
	transport := &http.Transport{DialContext: (nettarget.Dialer{AllowPrivate: s.cfg.DebugInfodAllowPrivate}).DialContext, DisableCompression: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	seen := make(map[string]bool)
	var fetched []native.DebugSymbolFile
	attempts := 0
	for _, module := range modules {
		id := strings.ToLower(module.CodeID)
		if module.DebugFormat != "elf" || len(id) == 0 || len(id) > 128 || len(id)%2 != 0 || seen[id] {
			continue
		}
		if _, err := hex.DecodeString(id); err != nil {
			continue
		}
		seen[id] = true
		key := native.DebugKey(id)
		if _, ok := symbols[key]; ok {
			continue
		}
		if _, ok := unwind[key]; ok {
			continue
		}
		for _, source := range s.cfg.DebugInfodURLs {
			if ctx.Err() != nil || attempts >= 25 || remaining <= 0 || len(local)+len(fetched) >= 25 {
				return fetched
			}
			target, err := url.JoinPath(source, "buildid", id, "debuginfo")
			if err != nil {
				continue
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				continue
			}
			attempts++
			resp, err := client.Do(req)
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
			if readErr != nil || len(data) < 4 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) || !strings.EqualFold(native.ObjectDebugID(data), id) {
				continue
			}
			fetched = append(fetched, native.DebugSymbolFile{Name: "debuginfo.elf", DebugID: id, Content: data})
			break
		}
	}
	return fetched
}
