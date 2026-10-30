package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/native"
	"goodsentry/internal/testfixture/nativepdb"
)

const budgetELFID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
const budgetPDBID = "5A9832E5287241C1838ED98914E9B7FF1"

func budgetModules() []native.Module {
	return []native.Module{
		{DebugFormat: "elf", CodeID: budgetELFID},
		{DebugFormat: "pe", DebugID: budgetPDBID, DebugFile: "crash.pdb"},
	}
}

func TestSymbolServersShareRequestBudget(t *testing.T) {
	var mu sync.Mutex
	counts := [2]int{}
	server := func(i int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[i]++
			mu.Unlock()
			http.NotFound(w, r)
		}))
	}
	linux, windows := server(0), server(1)
	defer linux.Close()
	defer windows.Close()
	s := &Server{cfg: config.Config{DebugInfodURLs: []string{linux.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{windows.URL}, SymStoreAllowPrivate: true}}
	var modules []native.Module
	for i := 0; i < 15; i++ {
		modules = append(modules, native.Module{DebugFormat: "elf", CodeID: fmt.Sprintf("%040x", i+1)})
		modules = append(modules, native.Module{DebugFormat: "pe", DebugID: fmt.Sprintf("%032X1", i+1), DebugFile: "crash.pdb"})
	}
	s.fetchSymbolServers(context.Background(), modules, nil, nil, nil)
	mu.Lock()
	defer mu.Unlock()
	if counts[0] == 0 || counts[1] == 0 || counts[0]+counts[1] != 25 {
		t.Fatalf("Linux and Windows requests = %v, want both protocols and 25 total", counts)
	}
}

func TestSymbolServersShareFileBudget(t *testing.T) {
	elfBytes, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	pdbBytes := nativepdb.Minimal().Data
	var mu sync.Mutex
	counts := [2]int{}
	server := func(i int, data []byte) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[i]++
			mu.Unlock()
			_, _ = w.Write(data)
		}))
	}
	linux, windows := server(0, elfBytes), server(1, pdbBytes)
	defer linux.Close()
	defer windows.Close()
	s := &Server{cfg: config.Config{DebugInfodURLs: []string{linux.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{windows.URL}, SymStoreAllowPrivate: true}}
	if got := s.fetchSymbolServers(context.Background(), budgetModules(), make([]native.DebugSymbolFile, 23), nil, nil); len(got) != 2 {
		t.Fatalf("two free slots must accept both valid identities; got %d files", len(got))
	}
	mu.Lock()
	counts = [2]int{}
	mu.Unlock()
	if got := s.fetchSymbolServers(context.Background(), budgetModules(), make([]native.DebugSymbolFile, 24), nil, nil); len(got) != 1 {
		t.Fatalf("one free slot must accept one file; got %d", len(got))
	}
	mu.Lock()
	defer mu.Unlock()
	if counts[0]+counts[1] != 1 {
		t.Fatalf("requests after consuming final file slot = %v, want one total", counts)
	}
}

func TestSymbolServersShareByteBudget(t *testing.T) {
	elfBytes, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	pdbBytes := nativepdb.Minimal().Data
	linux := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(elfBytes) }))
	defer linux.Close()
	windows := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pdbBytes) }))
	defer windows.Close()
	s := &Server{cfg: config.Config{DebugInfodURLs: []string{linux.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{windows.URL}, SymStoreAllowPrivate: true}}
	localSize := (64 << 20) - len(elfBytes) - len(pdbBytes)
	local := []native.DebugSymbolFile{{Content: make([]byte, localSize+1)}}
	local[0].Content = local[0].Content[:localSize]
	if got := s.fetchSymbolServers(context.Background(), budgetModules(), local, nil, nil); len(got) != 2 {
		t.Fatalf("both files fit exactly: got %d", len(got))
	}
	local[0].Content = local[0].Content[:localSize+1]
	if got := s.fetchSymbolServers(context.Background(), budgetModules(), local, nil, nil); len(got) != 1 || got[0].DebugID != budgetELFID {
		t.Fatalf("shared byte budget should reject the second file: got %d", len(got))
	}
}

func TestSymbolServersShareDeadline(t *testing.T) {
	var mu sync.Mutex
	counts := [2]int{}
	server := func(i int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[i]++
			n := counts[0] + counts[1]
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
	}
	linux, windows := server(0), server(1)
	defer linux.Close()
	defer windows.Close()
	s := &Server{cfg: config.Config{DebugInfodURLs: []string{linux.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{windows.URL}, SymStoreAllowPrivate: true}}
	start := time.Now()
	s.fetchSymbolServers(context.Background(), budgetModules(), nil, nil, nil)
	elapsed := time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	if counts != [2]int{1, 1} {
		t.Fatalf("requests by protocol = %v, want one each", counts)
	}
	if elapsed < 4500*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("both protocols took %v; want about 5 seconds total", elapsed)
	}
}
