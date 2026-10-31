package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/ageio"
	"gopkg.in/yaml.v3"
)

// SaveUsers writes users.yaml.
func SaveUsers(dir string, users map[string]User) error {
	b, err := yaml.Marshal(map[string]any{"users": users})
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "users.yaml"), b, 0o644)
}

// SaveSecrets writes the secrets file for every recipient listed in age.yaml,
// or for the local identity alone when the state has no age.yaml yet.
func SaveSecrets(dir string, sec Secrets) error {
	b, err := yaml.Marshal(sec)
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
	b, err := yaml.Marshal(map[string]any{"links": links})
	if err != nil {
		return fmt.Errorf("marshal links: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "links.yaml"), b, 0o644)
}
