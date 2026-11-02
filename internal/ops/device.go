package ops

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/crypto/ssh"
)

// DeviceSet stores a device's SSH key and, when given, its age recipient. The
// SSH key is the OpenSSH line the app copies; the render turns it into the
// agent registry PEM and the managed authorized_keys block. Validating here
// means a bad key fails before the file is written and before the next apply.
func DeviceSet(st *state.State, name, sshKey, ageKey string) (string, error) {
	if err := state.ValidDeviceName(name); err != nil {
		return "", err
	}

	line := strings.TrimSpace(sshKey)
	if line == "" {
		return "", fmt.Errorf("device %q: an ssh public key is required", name)
	}

	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", fmt.Errorf("device %q: ssh key: %w", name, err)
	}

	crypto, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return "", fmt.Errorf("device %q: the ssh line is not a public key", name)
	}

	ec, ok := crypto.CryptoPublicKey().(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return "", fmt.Errorf("device %q: the agent needs a P-256 key", name)
	}

	if ageKey != "" {
		if _, err := age.ParseX25519Recipient(strings.TrimSpace(ageKey)); err != nil {
			return "", fmt.Errorf("device %q: age recipient: %w", name, err)
		}
	}

	if st.Devices == nil {
		st.Devices = map[string]state.Device{}
	}

	st.Devices[name] = state.Device{SSH: line, Age: strings.TrimSpace(ageKey)}

	if err := state.SaveDevices(st.Dir, st.Devices); err != nil {
		return "", err
	}

	return fmt.Sprintf("devices: + %s", name), nil
}

// DeviceRemove drops a device; the next apply removes its agent key and its
// line from the managed block.
func DeviceRemove(st *state.State, name string) (string, error) {
	if _, ok := st.Devices[name]; !ok {
		return "", fmt.Errorf("device %q is not listed", name)
	}

	delete(st.Devices, name)

	if err := state.SaveDevices(st.Dir, st.Devices); err != nil {
		return "", err
	}

	return fmt.Sprintf("devices: - %s", name), nil
}

// DeviceNames is the sorted device list.
func DeviceNames(st *state.State) []string {
	out := make([]string, 0, len(st.Devices))
	for name := range st.Devices {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// DeviceSync adds the age recipients recorded on devices to age.yaml, so a
// phone registered from the app becomes a recipient without a second paste.
// The SSH key is the app's own; the age secret must exist on the device before
// the state can encrypt for it, and the operator's key does the re-encryption.
// A name that already belongs to a different recipient is left for `key rm`,
// because choosing which device is the same one is the operator's call.
func DeviceSync(st *state.State) ([]string, error) {
	var added []string

	recs, err := loadRecipients(st)
	if err != nil {
		return nil, err
	}

	byKey := map[string]string{}
	for _, r := range recs.Recipients {
		byKey[r.Key] = r.Name
	}

	for _, name := range DeviceNames(st) {
		ageKey := st.Devices[name].Age
		if ageKey == "" {
			continue
		}

		if _, ok := byKey[ageKey]; ok {
			continue
		}

		if existing, ok := recipientName(recs, name); ok && existing != ageKey {
			return nil, fmt.Errorf("device %q: age.yaml already has a different recipient named %q; remove it with `key rm %s` first", name, name, name)
		}

		msg, err := KeyAdd(st, name, ageKey)
		if err != nil {
			return nil, err
		}

		byKey[ageKey] = name

		added = append(added, msg)
	}

	return added, nil
}

func recipientName(recs *ageio.Recipients, name string) (string, bool) {
	for _, r := range recs.Recipients {
		if r.Name == name {
			return r.Key, true
		}
	}

	return "", false
}
