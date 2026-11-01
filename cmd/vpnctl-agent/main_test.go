package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/agent"
)

// TestHostNameFrom: the name a request is signed for comes from the file the
// install wrote; a missing or empty file falls back to the machine name so a
// host does not refuse every request after a bad install.
func TestHostNameFrom(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "host")

	if err := os.WriteFile(f, []byte("  ru-spb-1 \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := hostNameFrom(f); got != "ru-spb-1" {
		t.Fatalf("%q", got)
	}

	if err := os.WriteFile(f, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := hostNameFrom(f); got == "" || got == "ru-spb-1" {
		t.Fatalf("an empty file gave %q", got)
	}

	if got := hostNameFrom(filepath.Join(dir, "nope")); got == "" {
		t.Fatal("a missing file gave an empty host name")
	}
}

// TestPublicKeys: the agent trusts every .pub in its directory and nothing else.
// A broken file is skipped, not fatal, and an empty set is an error — the agent
// must not run with no key at all.
func TestPublicKeys(t *testing.T) {
	dir := t.TempDir()

	if _, err := publicKeys(dir); err == nil {
		t.Fatal("an empty key directory was accepted")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustPKIX(t, &key.PublicKey)})
	if err := os.WriteFile(filepath.Join(dir, "laptop.pub"), der, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "broken.pub"), []byte("not a key"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), der, 0o644); err != nil {
		t.Fatal(err)
	}

	keys, err := publicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	// compare the encodings, not the coordinates: the raw fields are deprecated
	want := mustPKIX(t, &key.PublicKey)
	if len(keys) != 1 || string(mustPKIX(t, keys[0])) != string(want) {
		t.Fatalf("keys %d", len(keys))
	}
}

// TestWriteAnswersOneLineOfJSON: the protocol is one JSON object per call, and
// a pretty-printed answer would break the client that parses the first line.
func TestWriteAnswersOneLineOfJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	os.Stdout = w

	write(agent.Response{V: agent.Version, OK: true, Marker: "abc1234"})

	os.Stdout = old

	_ = w.Close()

	var buf bytes.Buffer

	_, _ = io.Copy(&buf, r)

	line := buf.String()
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("answer %q is not one line", line)
	}

	var res agent.Response
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		t.Fatalf("answer is not JSON: %v", err)
	}

	if res.Marker != "abc1234" {
		t.Fatalf("%+v", res)
	}
}

// mustPKIX marshals a public key or fails the test.
func mustPKIX(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return der
}
