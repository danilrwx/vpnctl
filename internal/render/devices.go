package render

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/crypto/ssh"
)

// deviceFiles is the device half of every host: the agent's public key registry
// and the managed block of authorized_keys. Both come from devices.yaml, so a
// phone registered in the state reaches every host on the next apply. The
// block is always emitted, even when there are no devices, so the merge on the
// host removes a block that was dropped from the state.
func deviceFiles(st *state.State) (HostFiles, error) {
	f := HostFiles{}

	names := make([]string, 0, len(st.Devices))
	for name := range st.Devices {
		names = append(names, name)
	}

	sort.Strings(names)

	var block strings.Builder
	block.WriteString("# BEGIN vpnctl devices\n")
	block.WriteString("# managed by vpnctl apply; lines outside this block are kept\n")

	for _, name := range names {
		if err := state.ValidDeviceName(name); err != nil {
			return nil, err
		}

		d := st.Devices[name]

		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(d.SSH))
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", name, err)
		}

		crypto, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			return nil, fmt.Errorf("device %s: not a crypto public key", name)
		}

		ec, ok := crypto.CryptoPublicKey().(*ecdsa.PublicKey)
		if !ok || ec.Curve != elliptic.P256() {
			return nil, fmt.Errorf("device %s: the agent needs a P-256 key", name)
		}

		der, err := x509.MarshalPKIXPublicKey(ec)
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", name, err)
		}

		f["/etc/vpnctl/agents/"+name+".pub"] = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
		fmt.Fprintf(&block, "no-pty,command=\"/usr/local/bin/vpnctl-agent\" %s vpnctl-agent:%s\n", line, name)
	}

	block.WriteString("# END vpnctl devices\n")
	f["/etc/vpnctl/authorized_keys"] = block.String()

	return f, nil
}
