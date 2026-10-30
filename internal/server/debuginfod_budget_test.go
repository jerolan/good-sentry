package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/native"
	"goodsentry/internal/symbolserver"
)

func TestDebuginfodRequestBudgetAndDeduplication(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer source.Close()
	s := symbolserver.New(config.Config{DebugInfodURLs: []string{source.URL}, DebugInfodAllowPrivate: true})
	modules := []native.Module{{DebugFormat: "elf", CodeID: "0000"}, {DebugFormat: "elf", CodeID: "0000"}}
	for i := 1; i < 30; i++ {
		modules = append(modules, native.Module{DebugFormat: "elf", CodeID: fmt.Sprintf("%04x", i)})
	}
	if files := s.Fetch(context.Background(), modules, nil, nil, nil); len(files) != 0 {
		t.Fatalf("unexpected files: %d", len(files))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 25 {
		t.Fatalf("got %d requests, want 25: %v", len(paths), paths)
	}
	for i, path := range paths {
		want := fmt.Sprintf("/buildid/%04x/debuginfo", i)
		if path != want {
			t.Fatalf("request %d = %q, want %q", i, path, want)
		}
	}
}

func TestDebuginfodSharesLocalByteBudget(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		// Flush before writing to prevent a Content-Length header and exercise
		// the bounded reader against a server that streams too much data.
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("abcd"))
	}))
	defer source.Close()
	s := symbolserver.New(config.Config{DebugInfodURLs: []string{source.URL}, DebugInfodAllowPrivate: true})
	local := []native.DebugSymbolFile{{Content: make([]byte, (64<<20)-3)}}
	modules := []native.Module{{DebugFormat: "elf", CodeID: "01"}, {DebugFormat: "elf", CodeID: "02"}}
	if files := s.Fetch(context.Background(), modules, local, nil, nil); len(files) != 0 {
		t.Fatalf("accepted an oversized response: %d files", len(files))
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatalf("got %d requests after exhausting the shared byte budget, want 1", requests)
	}
}

func TestDebuginfodUsesOneDeadlineForCapture(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			select {
			case <-time.After(3 * time.Second):
				http.NotFound(w, r)
			case <-r.Context().Done():
			}
			return
		}
		<-r.Context().Done()
	}))
	defer source.Close()
	s := symbolserver.New(config.Config{DebugInfodURLs: []string{source.URL}, DebugInfodAllowPrivate: true})
	modules := []native.Module{{DebugFormat: "elf", CodeID: "01"}, {DebugFormat: "elf", CodeID: "02"}}
	start := time.Now()
	files := s.Fetch(context.Background(), modules, nil, nil, nil)
	elapsed := time.Since(start)
	if len(files) != 0 {
		t.Fatalf("unexpected files: %d", len(files))
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
	if elapsed < 4500*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("capture took %v; want about 5 seconds total", elapsed)
	}
}

func TestDebuginfodSharesLocalFileBudget(t *testing.T) {
	const firstID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	const secondID = "67cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	first, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	originalID, _ := hex.DecodeString(firstID)
	replacementID, _ := hex.DecodeString(secondID)
	if bytes.Count(first, originalID) != 1 {
		t.Fatal("fixture must contain one GNU build ID")
	}
	second := bytes.Replace(first, originalID, replacementID, 1)
	var mu sync.Mutex
	var paths []string
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/buildid/" + firstID + "/debuginfo":
			_, _ = w.Write(first)
		case "/buildid/" + secondID + "/debuginfo":
			_, _ = w.Write(second)
		default:
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	s := symbolserver.New(config.Config{DebugInfodURLs: []string{source.URL}, DebugInfodAllowPrivate: true})
	modules := []native.Module{{DebugFormat: "elf", CodeID: firstID}, {DebugFormat: "elf", CodeID: secondID}}
	// The control proves that both independently identified ELF files are usable.
	if files := s.Fetch(context.Background(), modules, make([]native.DebugSymbolFile, 23), nil, nil); len(files) != 2 {
		t.Fatalf("with two slots available got %d files, want 2", len(files))
	}
	mu.Lock()
	paths = nil
	mu.Unlock()
	files := s.Fetch(context.Background(), modules, make([]native.DebugSymbolFile, 24), nil, nil)
	if len(files) != 1 || files[0].DebugID != firstID {
		t.Fatalf("with one slot available got %v, want first ELF only", files)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/buildid/"+firstID+"/debuginfo" {
		t.Fatalf("requests after one slot became available: %v", paths)
	}
}
