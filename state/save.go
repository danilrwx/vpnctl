package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/sopsio"
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
	return sopsio.Write(filepath.Join(dir, "secrets.sops.yaml"), b)
}
