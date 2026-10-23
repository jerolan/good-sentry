package spanarchive

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestS3ObjectsUsePathStyleAndRoundTrip(t *testing.T) {
	const expectedPath = "/good-sentry-bucket/archive-v1/projects/9/cohort.parquet"
	wantBody := []byte("PAR1\x00lossless\xff")
	var stored []byte
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.Path != expectedPath {
			t.Errorf("request path=%q, want %q", r.URL.Path, expectedPath)
		}
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") != "*" {
				t.Errorf("PUT If-None-Match=%q, want *", r.Header.Get("If-None-Match"))
			}
			if r.Header.Get("Content-Type") != "application/vnd.apache.parquet" {
				t.Errorf("PUT content type=%q", r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read PUT body: %v", err)
			}
			if r.ContentLength != int64(len(body)) {
				t.Errorf("PUT content length=%d, want %d", r.ContentLength, len(body))
			}
			if len(stored) != 0 {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = io.WriteString(w, "<Error><Code>PreconditionFailed</Code><Message>Object exists</Message></Error>")
				return
			}
			stored = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Length", strconv.Itoa(len(stored)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected S3 method %q", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	objects, err := NewS3Objects(context.Background(), "us-east-1", "good-sentry-bucket", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	key, err := PrefixedKey("/archive-v1/", "projects/9/cohort.parquet")
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Put(context.Background(), key, wantBody); err != nil {
		t.Fatal(err)
	}
	if err := objects.Put(context.Background(), key, wantBody); err != nil {
		t.Fatalf("same-key S3 retry: %v", err)
	}
	if err := objects.Put(context.Background(), key, []byte("different object")); err == nil {
		t.Fatal("S3 retry replaced an immutable archive object")
	}
	if !bytes.Equal(stored, wantBody) {
		t.Fatalf("PUT body=%q, want %q", stored, wantBody)
	}
	got, err := objects.Get(context.Background(), key)
	if err != nil || !bytes.Equal(got, wantBody) {
		t.Fatalf("GET body=%q err=%v", got, err)
	}
	if err := objects.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{
		http.MethodPut, http.MethodPut, http.MethodGet, http.MethodPut, http.MethodGet,
		http.MethodGet, http.MethodDelete,
	}) {
		t.Fatalf("S3 methods=%v", methods)
	}
}

func TestS3ObjectsRejectInvalidSettingsKeysAndSizes(t *testing.T) {
	if _, err := NewS3Objects(context.Background(), "", "bucket", ""); err == nil {
		t.Fatal("empty S3 settings were accepted")
	}
	objects := &S3Objects{bucket: "bucket"}
	for _, key := range []string{"", "../outside", "/absolute", `nested\outside`} {
		if err := objects.Put(context.Background(), key, []byte("body")); err == nil {
			t.Errorf("invalid key %q was accepted", key)
		}
	}
	if err := objects.Put(context.Background(), "oversized.parquet", make([]byte, MaxObjectBytes+1)); err == nil {
		t.Fatal("oversized S3 object was accepted")
	}
}
