package spanarchive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLocalObjectsPublishReadAndDelete(t *testing.T) {
	store, err := NewLocalObjects(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "projects/17/cohort.parquet"
	body := []byte("parquet body\x00\xff")
	if err := store.Put(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, key, body); err != nil {
		t.Fatalf("same-key retry: %v", err)
	}
	if err := store.Put(ctx, key, []byte("different")); err == nil {
		t.Fatal("same key accepted different contents")
	}
	got, err := store.Get(ctx, key)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read body=%q err=%v", got, err)
	}
	info, err := os.Stat(filepath.Join(store.root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("object permissions=%o, want 600", info.Mode().Perm())
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read after delete error=%v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

func TestLocalObjectsRejectUnsafeAndOversizedInput(t *testing.T) {
	store, err := NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "/absolute", "../outside", "nested/../outside", `nested\outside`, "nested//outside"} {
		if err := store.Put(context.Background(), key, []byte("data")); err == nil {
			t.Errorf("unsafe key %q was accepted", key)
		}
	}
	if err := store.Put(context.Background(), "large.parquet", make([]byte, MaxObjectBytes+1)); err == nil {
		t.Fatal("oversized object was accepted")
	}
	if _, err := NewLocalObjects(" "); err == nil {
		t.Fatal("empty directory was accepted")
	}
}

func TestLocalObjectsConcurrentSameKeyPublication(t *testing.T) {
	store, err := NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	body := bytes.Repeat([]byte("cohort"), 4096)
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- store.Put(context.Background(), "retry/object.parquet", body)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Get(context.Background(), "retry/object.parquet")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("published body size=%d err=%v", len(got), err)
	}
}

func TestLocalObjectsHonorCanceledContext(t *testing.T) {
	store, err := NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "cancelled.parquet", []byte("data")); !errors.Is(err, context.Canceled) {
		t.Fatalf("put error=%v", err)
	}
	if _, err := store.Get(ctx, "cancelled.parquet"); !errors.Is(err, context.Canceled) {
		t.Fatalf("get error=%v", err)
	}
	if err := store.Delete(ctx, "cancelled.parquet"); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete error=%v", err)
	}
}

func TestCohortKeyIsStableForTheSameImmutableRows(t *testing.T) {
	first, err := CohortKey(17, []int64{91, 34, 67})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CohortKey(17, []int64{34, 67, 91})
	if err != nil || first != second {
		t.Fatalf("reordered cohort key=%q err=%v, want %q", second, err, first)
	}
	otherProject, err := CohortKey(18, []int64{34, 67, 91})
	if err != nil || first == otherProject {
		t.Fatalf("project-specific key=%q err=%v", otherProject, err)
	}
	otherRows, err := CohortKey(17, []int64{34, 67, 92})
	if err != nil || first == otherRows {
		t.Fatalf("cohort-specific key=%q err=%v", otherRows, err)
	}
	for _, tc := range []struct {
		project int64
		ids     []int64
	}{
		{project: 0, ids: []int64{1}},
		{project: 1},
		{project: 1, ids: []int64{1, 1}},
		{project: 1, ids: []int64{0}},
		{project: 1, ids: make([]int64, MaxRows+1)},
	} {
		if _, err := CohortKey(tc.project, tc.ids); err == nil {
			t.Errorf("invalid cohort key accepted: project=%d ids=%v", tc.project, tc.ids)
		}
	}
}
