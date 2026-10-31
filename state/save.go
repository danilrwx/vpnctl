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

// SaveSecrets writes the secrets file, encrypted when .sops.yaml is present.
func SaveSecrets(dir string, sec Secrets) error {
	b, err := yaml.Marshal(sec)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	return ageio.Write(filepath.Join(dir, "secrets.age.yaml"), KeyPath(), b)
}

// SaveLinks writes links.yaml.
func SaveLinks(dir string, links []Link) error {
	b, err := yaml.Marshal(map[string]any{"links": links})
	if err != nil {
		return fmt.Errorf("marshal links: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "links.yaml"), b, 0o644)
}
