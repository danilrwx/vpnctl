package mobile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// Identity is the device's key to the state: the age identity that decrypts
// secrets.age.yaml. It never leaves the app's private storage, and the private
// half is kept wrapped by a Keystore key — Keystore cannot hold X25519 itself,
// so this is the closest the platform allows.
type Identity struct {
	// Recipient is what goes into age.yaml: `vpnctl key add phone age1…`.
	Recipient string
	// Secret is the private half, for the app to store wrapped.
	Secret string
}

// NewIdentity mints a device identity. It is called once, on first launch; the
// caller stores the secret and shows the recipient to the operator.
func NewIdentity() (*Identity, error) {
	id, err := ageio.NewIdentity()
	if err != nil {
		return nil, err
	}

	return &Identity{Recipient: ageio.RecipientOf(id), Secret: id.String()}, nil
}

// RoundTrip proves the pair works without leaving the device: it encrypts a
// sentence to the recipient and decrypts it with the secret. A recipient that
// cannot be decrypted by its own secret would make the state unreadable after
// the operator adds it, and that is worth catching on the phone.
func RoundTrip(recipient, secret string) (string, error) {
	id, err := age.ParseX25519Identity(secret)
	if err != nil {
		return "", fmt.Errorf("secret: %w", err)
	}

	// the identity must be the one the recipient names, or the operator would
	// add a recipient nobody holds
	if id.Recipient().String() != recipient {
		return "", fmt.Errorf("the secret belongs to another recipient")
	}

	var sealed bytes.Buffer

	w, err := age.Encrypt(&sealed, id.Recipient())
	if err != nil {
		return "", err
	}

	if _, err := io.WriteString(w, "vpnctl"); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	r, err := age.Decrypt(bytes.NewReader(sealed.Bytes()), id)
	if err != nil {
		return "", err
	}

	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	if string(out) != "vpnctl" {
		return "", fmt.Errorf("the round trip returned %q", out)
	}

	return "identity works", nil
}

// SealIdentity encrypts this device's identity with a passphrase and returns an
// armored blob. The private half lives only in the Keystore vault, which dies
// with the app: without a sealed copy a wiped phone cannot be recovered, and the
// state would have to be rebuilt around a new recipient.
func SealIdentity(passphrase string) (string, error) {
	path := state.KeyPath()

	id, err := ageio.Identity(path)
	if err != nil {
		return "", fmt.Errorf("this device has no identity to export: %w", err)
	}

	return ageio.SealIdentity(id, passphrase)
}

// UnsealIdentity opens a blob made by SealIdentity and proves it is the key this
// state expects before the caller replaces the device's own. A restored identity
// that is not a recipient would leave the phone unable to decrypt anything, with
// no way back, so the check happens while the old key is still in place.
func UnsealIdentity(blob, passphrase, dir string) (*Identity, error) {
	id, err := ageio.OpenIdentity(strings.TrimSpace(blob), passphrase)
	if err != nil {
		return nil, err
	}

	secrets := filepath.Join(dir, "secrets.age.yaml")

	if _, err := os.Stat(secrets); err == nil {
		if _, err := ageio.DecryptWith(secrets, id); err != nil {
			return nil, fmt.Errorf("this identity does not decrypt %s: %w", filepath.Base(secrets), err)
		}
	}

	return &Identity{Recipient: ageio.RecipientOf(id), Secret: id.String()}, nil
}
