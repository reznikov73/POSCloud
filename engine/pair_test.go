package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseConnectionCode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ConnectionInfo
		bad  bool
	}{
		{
			name: "tls",
			raw:  "poscloud://connect?v=1&host=26.72.161.50&port=8099&tls=1&fp=AABB&code=pcp_secret",
			want: ConnectionInfo{Host: "26.72.161.50", Port: 8099, TLS: true, Fingerprint: "AABB", Code: "pcp_secret"},
		},
		{
			name: "no tls",
			raw:  "poscloud://connect?v=1&host=localhost&port=8090&tls=0&code=pcp_secret",
			want: ConnectionInfo{Host: "localhost", Port: 8090, TLS: false, Code: "pcp_secret"},
		},
		{name: "wrong scheme", raw: "https://connect?v=1&host=x&port=1&code=x", bad: true},
		{name: "wrong version", raw: "poscloud://connect?v=2&host=x&port=1&code=x", bad: true},
		{name: "bad port", raw: "poscloud://connect?v=1&host=x&port=0&code=x", bad: true},
		{name: "missing host", raw: "poscloud://connect?v=1&port=1&code=x", bad: true},
		{name: "tls without fp", raw: "poscloud://connect?v=1&host=x&port=1&tls=1&code=x", bad: true},
		{name: "missing code", raw: "poscloud://connect?v=1&host=x&port=1", bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseConnectionCode(tc.raw)
			if tc.bad {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPair(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pair" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(PairResult{
			Token: "pcs_new", UserID: "home", UserName: "Дом",
		})
	}))
	defer srv.Close()

	res, err := Pair(srv.URL, " pcp_one ", " WORK-PC ")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if res.Token != "pcs_new" || res.UserID != "home" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if got["code"] != "pcp_one" || got["deviceName"] != "WORK-PC" {
		t.Fatalf("unexpected request: %+v", got)
	}
}

func TestPairErrors(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":"код недействителен"}`, "код недействителен"},
		{429, ``, "слишком много"},
		{500, `{"error":"boom"}`, "boom"},
	}
	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := Pair(srv.URL, "x", "")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("status %d: got %v, want %q", tc.status, err, tc.want)
		}
	}
}
