package agent

import (
	"path/filepath"
	"testing"
	"time"
)

// TestNonceUsedOnce: the same nonce twice is a replay, and the cache refuses it
// before the signature is even considered again.
func TestNonceUsedOnce(t *testing.T) {
	c := NonceCache{Dir: filepath.Join(t.TempDir(), "nonces"), TTL: time.Minute}
	if err := c.Use("abc"); err != nil {
		t.Fatal(err)
	}

	if err := c.Use("abc"); err == nil {
		t.Fatal("the same nonce was accepted twice")
	}

	if err := c.Use("def"); err != nil {
		t.Fatalf("a fresh nonce was refused: %v", err)
	}

	if err := c.Use(""); err == nil {
		t.Fatal("an empty nonce was accepted")
	}
}

// TestNonceExpires: an old file is dropped, so the directory does not grow for
// ever and a clock-skew replay outside the window still works.
func TestNonceExpires(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonces")

	c := NonceCache{Dir: dir, TTL: time.Minute}
	if err := c.Use("old"); err != nil {
		t.Fatal(err)
	}
	// an hour later the file is stale and must not block a request
	later := NonceCache{Dir: dir, TTL: time.Minute, Now: func() time.Time { return time.Now().Add(time.Hour) }}
	if err := later.Use("old"); err != nil {
		t.Fatalf("a stale nonce was still refused: %v", err)
	}
}
