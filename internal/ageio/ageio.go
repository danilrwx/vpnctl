// Package ageio encrypts and decrypts the whole secrets file with age.
// The file is written for every recipient listed in age.yaml: one per device
// plus an offline backup identity kept outside the repository.
package ageio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	"gopkg.in/yaml.v3"
)

// RecipientsFile is the list of who can read secrets.age.yaml.
const RecipientsFile = "age.yaml"

// Recipient is one device or the offline backup.
type Recipient struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
	// Added is the day the recipient was created, for the rotation hint.
	Added string `yaml:"added,omitempty"`
}

// Recipients is the whole age.yaml file.
type Recipients struct {
	Recipients []Recipient `yaml:"recipients"`
}

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

// HasIdentity reports an existing age key file.
func HasIdentity(keyPath string) bool {
	_, err := os.Stat(keyPath)
	return err == nil
}

// NewIdentity creates a fresh X25519 identity.
func NewIdentity() (*age.X25519Identity, error) {
	return age.GenerateX25519Identity()
}

// RecipientOf is the public half of an identity.
func RecipientOf(id *age.X25519Identity) string { return id.Recipient().String() }

// WriteIdentity stores an identity in the age keys.txt format, readable only by the owner.
func WriteIdentity(path string, id *age.X25519Identity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	body := fmt.Sprintf("# public key: %s\n%s\n", RecipientOf(id), id.String())

	return os.WriteFile(path, []byte(body), 0o600)
}

// OwnRecipient is the recipient of the identity stored at keyPath.
func OwnRecipient(keyPath string) (string, error) {
	id, err := Identity(keyPath)
	if err != nil {
		return "", err
	}

	return RecipientOf(id), nil
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

	defer func() { _ = f.Close() }()

	r, err := age.Decrypt(f, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", path, err)
	}

	return io.ReadAll(r)
}

// EncryptTo encrypts plain for the recipients and stores it at path.
func EncryptTo(path string, recipients []age.Recipient, plain []byte) error {
	if len(recipients) == 0 {
		return errors.New("no recipients: refusing to write secrets nobody can read")
	}

	var buf bytes.Buffer

	w, err := age.Encrypt(&buf, recipients...)
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

// Write encrypts plain for the identity's own recipient and stores it at path.
func Write(path, keyPath string, plain []byte) error {
	id, err := Identity(keyPath)
	if err != nil {
		return err
	}

	return EncryptTo(path, []age.Recipient{id.Recipient()}, plain)
}

// LoadRecipients reads age.yaml; a missing file is not an error and returns nil.
func LoadRecipients(dir string) (*Recipients, error) {
	b, err := os.ReadFile(filepath.Join(dir, RecipientsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", RecipientsFile, err)
	}

	var r Recipients
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RecipientsFile, err)
	}

	if err := r.Validate(); err != nil {
		return nil, err
	}

	return &r, nil
}

// MarshalRecipients is age.yaml as the tool writes it: two spaces, sorted by
// name, so the file is stable in git and a hand edit shows as one rule broken.
func MarshalRecipients(r *Recipients) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	if len(r.Recipients) == 0 {
		return nil, errors.New("no recipients: refusing to write an empty age.yaml")
	}

	sortRecipients(r.Recipients)

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(r); err != nil {
		return nil, fmt.Errorf("marshal %s: %w", RecipientsFile, err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshal %s: %w", RecipientsFile, err)
	}

	return buf.Bytes(), nil
}

// Save writes age.yaml.
func (r *Recipients) Save(dir string) error {
	b, err := MarshalRecipients(r)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, RecipientsFile), b, 0o644)
}

func sortRecipients(list []Recipient) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Name < list[j-1].Name; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// Validate checks names and keys.
func (r *Recipients) Validate() error {
	seen := map[string]bool{}

	for _, rec := range r.Recipients {
		if !identOK(rec.Name) {
			return fmt.Errorf("recipient %q: name must match [a-z0-9-_]", rec.Name)
		}

		if seen[rec.Name] {
			return fmt.Errorf("recipient %q is listed twice", rec.Name)
		}

		seen[rec.Name] = true
		if _, err := age.ParseX25519Recipient(rec.Key); err != nil {
			return fmt.Errorf("recipient %q: %w", rec.Name, err)
		}
	}

	return nil
}

// Keys returns the parsed recipients.
func (r *Recipients) Keys() ([]age.Recipient, error) {
	out := make([]age.Recipient, 0, len(r.Recipients))
	for _, rec := range r.Recipients {
		k, err := age.ParseX25519Recipient(rec.Key)
		if err != nil {
			return nil, fmt.Errorf("recipient %q: %w", rec.Name, err)
		}

		out = append(out, k)
	}

	return out, nil
}

// Add appends a recipient; a repeated name is an error, a repeated key is not
// (the same person may rotate the device name).
func (r *Recipients) Add(name, key string) error {
	if !identOK(name) {
		return fmt.Errorf("recipient %q: name must match [a-z0-9-_]", name)
	}

	if _, err := age.ParseX25519Recipient(key); err != nil {
		return fmt.Errorf("recipient %q: %w", name, err)
	}

	for _, rec := range r.Recipients {
		if rec.Name == name {
			if rec.Key == key {
				return nil // already there
			}

			return fmt.Errorf("recipient %q already exists with another key", name)
		}
	}

	r.Recipients = append(r.Recipients, Recipient{Name: name, Key: key, Added: today()})

	return nil
}

// Remove drops a recipient by name; the last one cannot be removed.
func (r *Recipients) Remove(name string) (bool, error) {
	for i, rec := range r.Recipients {
		if rec.Name != name {
			continue
		}

		if len(r.Recipients) == 1 {
			return false, errors.New("the last recipient cannot be removed: the secrets would become unreadable")
		}

		r.Recipients = append(r.Recipients[:i], r.Recipients[i+1:]...)

		return true, nil
	}

	return false, nil
}

// Replace adds a recipient or swaps its key, keeping the name.
func (r *Recipients) Replace(name, key string) error {
	if _, err := age.ParseX25519Recipient(key); err != nil {
		return fmt.Errorf("recipient %q: %w", name, err)
	}

	for i := range r.Recipients {
		if r.Recipients[i].Name == name {
			r.Recipients[i].Key = key
			r.Recipients[i].Added = today()

			return nil
		}
	}

	return r.Add(name, key)
}

// Has reports whether a key is already listed.
func (r *Recipients) Has(key string) bool {
	for _, rec := range r.Recipients {
		if rec.Key == key {
			return true
		}
	}

	return false
}

// Names lists recipients in sorted order.
func (r *Recipients) Names() []string {
	out := make([]string, 0, len(r.Recipients))
	for _, rec := range r.Recipients {
		out = append(out, rec.Name)
	}

	sortStrings(out)

	return out
}

// SealIdentity encrypts an identity with a passphrase and returns an armored blob:
// the offline backup the user stores outside the devices.
func SealIdentity(id *age.X25519Identity, passphrase string) (string, error) {
	if len(passphrase) < 8 {
		return "", errors.New("passphrase is shorter than 8 characters")
	}

	rec, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	aw := armor.NewWriter(&buf)

	w, err := age.Encrypt(aw, rec)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	if _, err := w.Write([]byte(id.String())); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	if err := aw.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// OpenIdentity decrypts a blob made by SealIdentity.
func OpenIdentity(blob, passphrase string) (*age.X25519Identity, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}

	r, err := age.Decrypt(armor.NewReader(strings.NewReader(blob)), id)
	if err != nil {
		return nil, fmt.Errorf("wrong passphrase or damaged backup: %w", err)
	}

	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return age.ParseX25519Identity(strings.TrimSpace(string(body)))
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func identOK(s string) bool {
	if s == "" {
		return false
	}

	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}

	return true
}

func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
