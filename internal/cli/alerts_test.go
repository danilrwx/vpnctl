package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPostAlert: the probe and `alerts test` send the same thing, so the header
// names are pinned here — ntfy reads the token from Authorization and the title
// from Title, and a typo shows up as a message on a phone only.
func TestPostAlert(t *testing.T) {
	var (
		got  http.Header
		body string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		body = string(b[:n])

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	status, err := postAlert(srv.URL+"/topic", "tok-123", "hello", "high")
	if err != nil {
		t.Fatal(err)
	}

	if status != 200 {
		t.Fatalf("status %d", status)
	}

	if got.Get("Authorization") != "Bearer tok-123" {
		t.Errorf("Authorization: %q", got.Get("Authorization"))
	}

	if got.Get("Title") != "vpnctl" || got.Get("Priority") != "high" {
		t.Errorf("headers: %v", got)
	}

	if body != "hello" {
		t.Errorf("body %q", body)
	}
}

func TestPostAlertRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := postAlert(srv.URL+"/topic", "", "hello", "default"); err == nil {
		t.Fatal("a 401 was reported as success")
	}
}
