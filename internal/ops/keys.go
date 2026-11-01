package ops

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// KeyInfo is one recipient of secrets.age.yaml with the local device marked.
type KeyInfo struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Added string `json:"added,omitempty"`
	This  bool   `json:"this"`
}

// KeyInit writes age.yaml from the local identity: the state gets an explicit
// recipient list instead of relying on whoever holds the age key.
func KeyInit(st *state.State, name string) (string, error) {
	recs, err := ageio.LoadRecipients(st.Dir)
	if err != nil {
		return "", err
	}
	if recs != nil {
		return "", fmt.Errorf("%s already exists", ageio.RecipientsFile)
	}
	own, err := ageio.OwnRecipient(state.KeyPath())
	if err != nil {
		return "", err
	}
	recs = &ageio.Recipients{}
	if err := recs.Add(name, own); err != nil {
		return "", err
	}
	if err := recs.Save(st.Dir); err != nil {
		return "", err
	}
	if err := state.SaveSecrets(st.Dir, st.Secrets); err != nil {
		return "", err
	}
	return fmt.Sprintf("keys: init %s with %s", ageio.RecipientsFile, name), nil
}

// KeyAdd adds a recipient and re-encrypts the secrets for everyone.
func KeyAdd(st *state.State, name, key string) (string, error) {
	recs, err := loadRecipients(st)
	if err != nil {
		return "", err
	}
	if err := recs.Add(name, key); err != nil {
		return "", err
	}
	if err := recs.Save(st.Dir); err != nil {
		return "", err
	}
	if err := state.SaveSecrets(st.Dir, st.Secrets); err != nil {
		return "", err
	}
	return fmt.Sprintf("keys: + %s", name), nil
}

// KeyRemove drops a recipient and re-encrypts the secrets for the rest.
func KeyRemove(st *state.State, name string) (string, error) {
	recs, err := loadRecipients(st)
	if err != nil {
		return "", err
	}
	ok, err := recs.Remove(name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("recipient %q is not listed", name)
	}
	if err := recs.Save(st.Dir); err != nil {
		return "", err
	}
	if err := state.SaveSecrets(st.Dir, st.Secrets); err != nil {
		return "", err
	}
	return fmt.Sprintf("keys: - %s", name), nil
}

// Keys lists the recipients; the local device is marked.
func Keys(st *state.State) ([]KeyInfo, error) {
	recs, err := ageio.LoadRecipients(st.Dir)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		return nil, fmt.Errorf("no %s: run key init", ageio.RecipientsFile)
	}
	own, ownErr := ageio.OwnRecipient(state.KeyPath())
	out := make([]KeyInfo, 0, len(recs.Recipients))
	for _, r := range recs.Recipients {
		out = append(out, KeyInfo{Name: r.Name, Key: r.Key, Added: r.Added, This: ownErr == nil && r.Key == own})
	}
	return out, nil
}

func loadRecipients(st *state.State) (*ageio.Recipients, error) {
	recs, err := ageio.LoadRecipients(st.Dir)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		return nil, fmt.Errorf("no %s: run key init first", ageio.RecipientsFile)
	}
	return recs, nil
}

// InitOptions is what `vpnctl init` needs.
type InitOptions struct {
	// Name is the device name recorded in age.yaml, e.g. "phone".
	Name string
	// BackupPassphrase creates the offline backup identity and seals it with it.
	BackupPassphrase string
}

// InitResult is returned once: the sealed backup identity is never stored.
type InitResult struct {
	Message    string
	Recipient  string
	BackupBlob string
}

// Init writes the skeleton of a state directory: an empty state, the default
// sets and versions, age.yaml with this device, and the first commit message.
func Init(dir string, opt InitOptions) (InitResult, error) {
	if _, err := os.Stat(filepath.Join(dir, "hosts.yaml")); err == nil {
		return InitResult{}, fmt.Errorf("%s already contains a state", dir)
	}
	if opt.Name == "" {
		h, _ := os.Hostname()
		opt.Name = h
	}
	if err := os.MkdirAll(filepath.Join(dir, "sets"), 0o755); err != nil {
		return InitResult{}, err
	}

	keyPath := state.KeyPath()
	own, err := ageio.Identity(keyPath)
	if err != nil {
		if !ageio.HasIdentity(keyPath) {
			own, err = ageio.NewIdentity()
			if err != nil {
				return InitResult{}, err
			}
			if err := ageio.WriteIdentity(keyPath, own); err != nil {
				return InitResult{}, err
			}
		} else {
			return InitResult{}, err
		}
	}
	recs := &ageio.Recipients{}
	if err := recs.Add(opt.Name, ageio.RecipientOf(own)); err != nil {
		return InitResult{}, err
	}
	res := InitResult{Recipient: ageio.RecipientOf(own)}
	if opt.BackupPassphrase != "" {
		backup, err := ageio.NewIdentity()
		if err != nil {
			return InitResult{}, err
		}
		blob, err := ageio.SealIdentity(backup, opt.BackupPassphrase)
		if err != nil {
			return InitResult{}, err
		}
		// prove the blob opens before the user is told it is the only way back
		back, err := ageio.OpenIdentity(blob, opt.BackupPassphrase)
		if err != nil || back.String() != backup.String() {
			return InitResult{}, fmt.Errorf("the backup identity does not round-trip: %w", err)
		}
		if err := recs.Add("backup", ageio.RecipientOf(backup)); err != nil {
			return InitResult{}, err
		}
		res.BackupBlob = blob
	}
	if err := recs.Save(dir); err != nil {
		return InitResult{}, err
	}

	files := map[string]string{
		"hosts.yaml":     "hosts: {}\n",
		"links.yaml":     "links: []\n",
		"users.yaml":     "users: {}\n",
		"protocols.yaml": "inbounds: {}\n",
		".gitignore":     "/out\n/metrics.sqlite\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return InitResult{}, err
		}
	}
	versions := map[string]state.Pin{
		"sing-box": {Version: "1.12.4"},
		"xray":     {Version: "25.9.11"},
		"geodata":  {Version: "latest"},
		"caddy":    {Version: "2.10.0"},
		"mtg":      {Version: "2.1.7"},
		"awg":      {Version: "1.0.20250706"},
	}
	if err := state.SaveVersions(dir, versions); err != nil {
		return InitResult{}, err
	}
	defaults := map[string]string{
		"sets/ru-direct.yaml": "geosite:\n- category-ru\n- yandex\n- vk\ndomains:\n- gosuslugi.ru\n",
		"sets/ads.yaml":       "geosite:\n- category-ads-all\n",
	}
	for name, body := range defaults {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return InitResult{}, err
		}
	}
	if err := state.SaveSecrets(dir, state.Secrets{}); err != nil {
		return InitResult{}, err
	}
	res.Message = "init: empty state"
	return res, nil
}

// KeyBackup issues a fresh offline backup identity: it replaces the `backup`
// recipient, re-encrypts the secrets and returns the sealed blob, which the
// caller must store outside the devices.
func KeyBackup(st *state.State, passphrase string) (string, string, error) {
	if passphrase == "" {
		return "", "", fmt.Errorf("a passphrase is required: it is the only thing protecting the backup")
	}
	recs, err := loadRecipients(st)
	if err != nil {
		return "", "", err
	}
	backup, err := ageio.NewIdentity()
	if err != nil {
		return "", "", err
	}
	blob, err := ageio.SealIdentity(backup, passphrase)
	if err != nil {
		return "", "", err
	}
	back, err := ageio.OpenIdentity(blob, passphrase)
	if err != nil || back.String() != backup.String() {
		return "", "", fmt.Errorf("the backup identity does not round-trip: %w", err)
	}
	if err := recs.Replace("backup", ageio.RecipientOf(backup)); err != nil {
		return "", "", err
	}
	if err := recs.Save(st.Dir); err != nil {
		return "", "", err
	}
	if err := state.SaveSecrets(st.Dir, st.Secrets); err != nil {
		return "", "", err
	}
	return blob, "keys: new backup identity", nil
}
