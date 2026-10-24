package engine

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchChangesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	_, _, err := FetchChanges(srv.URL, "bad-token", 0)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ожидалась ErrUnauthorized, получено: %v", err)
	}
}

func TestFetchManifestUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	_, _, err := FetchManifest(srv.URL, "bad-token")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ожидалась ErrUnauthorized, получено: %v", err)
	}
}
