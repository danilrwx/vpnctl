package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NonceCache remembers the nonces of the requests it has seen, so a replay is
// refused even inside the clock skew the signature tolerates. It is a directory
// of empty files: cheap, survives a restart, and needs no database.
type NonceCache struct {
	Dir string
	TTL time.Duration
	// Now is the clock; a test sets it, a host leaves it nil.
	Now func() time.Time
}

// Use records the nonce and fails when it was already recorded.
func (c NonceCache) Use(nonce string) error {
	if nonce == "" {
		return fmt.Errorf("empty nonce")
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if entries, err := os.ReadDir(c.Dir); err == nil {
		for _, e := range entries {
			info, err := e.Info()
			if err == nil && now().Sub(info.ModTime()) > c.TTL {
				_ = os.Remove(filepath.Join(c.Dir, e.Name()))
			}
		}
	}
	f, err := os.OpenFile(filepath.Join(c.Dir, nonce), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("nonce %s: %w", nonce, err)
	}
	return f.Close()
}
