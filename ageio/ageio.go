// Package ageio encrypts and decrypts the whole secrets file with age.
// One file, one recipient; the identity is a standard age key.
package ageio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// Identity reads the first secret key from an age key file.
func Identity(keyPath string) (*age.X25519Identity, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read age key: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if id, err := age.ParseX25519Identity(line); err == nil {
			return id, nil
		}
	}
	return nil, fmt.Errorf("no secret key in %s", keyPath)
}

// Decrypt returns the plaintext of an age-encrypted file.
func Decrypt(path, keyPath string) ([]byte, error) {
	id, err := Identity(keyPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	r, err := age.Decrypt(f, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", path, err)
	}
	return io.ReadAll(r)
}

// Write encrypts plain for the identity's recipient and stores it at path.
func Write(path, keyPath string, plain []byte) error {
	id, err := Identity(keyPath)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
