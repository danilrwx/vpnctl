package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/ageio"
	"gopkg.in/yaml.v3"
)

// marshal renders YAML with two-space indentation: the state files have been
// written this way since the Rust core, and four spaces make every diff noisy.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// SaveUsers writes users.yaml.
func SaveUsers(dir string, users map[string]User) error {
	b, err := marshal(map[string]any{"users": users})
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "users.yaml"), b, 0o644)
}

// SaveSecrets writes the secrets file for every recipient listed in age.yaml,
// or for the local identity alone when the state has no age.yaml yet.
func SaveSecrets(dir string, sec Secrets) error {
	b, err := marshal(sec)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	path := filepath.Join(dir, "secrets.age.yaml")
	recs, err := ageio.LoadRecipients(dir)
	if err != nil {
		return err
	}
	if recs != nil && len(recs.Recipients) > 0 {
		keys, err := recs.Keys()
		if err != nil {
			return err
		}
		return ageio.EncryptTo(path, keys, b)
	}
	return ageio.Write(path, KeyPath(), b)
}

// SaveLinks writes links.yaml.
func SaveLinks(dir string, links []Link) error {
	b, err := marshal(map[string]any{"links": links})
	if err != nil {
		return fmt.Errorf("marshal links: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "links.yaml"), b, 0o644)
}
