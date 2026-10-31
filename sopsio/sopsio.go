// Package sopsio writes the secrets file. Encryption is delegated to the sops
// binary here; a mobile build installs its own implementation.
package sopsio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Encrypter turns a plain YAML document into the encrypted file contents.
type Encrypter interface {
	Encrypt(path string, plain []byte) ([]byte, error)
}

var current Encrypter = binary{}

// Install replaces the encrypter (first call wins in practice).
func Install(e Encrypter) { current = e }

// Current returns the active encrypter.
func Current() Encrypter { return current }

type binary struct{}

// Encrypt runs `sops -e` with the creation rules of the file's directory.
func (binary) Encrypt(path string, plain []byte) ([]byte, error) {
	cmd := exec.Command("sops",
		"-e", "--input-type", "yaml", "--output-type", "yaml",
		"--filename-override", path, "/dev/stdin")
	cmd.Dir = filepath.Dir(path)
	cmd.Stdin = strings.NewReader(string(plain))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sops -e: %w: %s", err, stderr.String())
	}
	return out, nil
}

// Write encrypts plain and stores it at path, honouring .sops.yaml in the dir.
func Write(path string, plain []byte) error {
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), ".sops.yaml")); err != nil {
		return os.WriteFile(path, plain, 0o600)
	}
	out, err := current.Encrypt(path, plain)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
