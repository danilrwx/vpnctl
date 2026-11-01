package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/crypto/ssh"
)

// HostAgentInstall puts the agent on a host and gives it exactly one job: the
// key in authorized_keys is restricted to the forced command and nothing else.
func HostAgentInstall(dir, host, binary, operator string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if _, err := st.Host(host); err != nil {
		return err
	}
	if binary == "" {
		binary = defaultAgentBinary()
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("agent binary %s: %w (build it with go build -o vpnctl-agent ./cmd/vpnctl-agent)", binary, err)
	}
	key, err := agent.LoadKey(agent.KeyPath())
	if err != nil {
		return err
	}
	pubPEM, err := agent.PublicPEM(key)
	if err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	if operator == "" {
		operator, _ = os.Hostname()
	}
	line := fmt.Sprintf(`restrict,no-pty,command="/usr/local/bin/vpnctl-agent" %s vpnctl-agent:%s`,
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), operator)

	c, err := remote.Dial(st, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	steps := []struct {
		name   string
		script string
		stdin  []byte
	}{
		{"upload", "cat > /tmp/vpnctl-agent.new", body},
		{"install", "install -m 0755 /tmp/vpnctl-agent.new /usr/local/bin/vpnctl-agent && rm -f /tmp/vpnctl-agent.new", nil},
		{"host name", fmt.Sprintf("install -d -m 0755 /etc/vpnctl && printf '%%s\\n' %q > /etc/vpnctl/host", host), nil},
		// what the host runs, so plan can say it out loud when it is stale
		{"version", "/usr/local/bin/vpnctl-agent --version > /etc/vpnctl/agent.version", nil},
		{"key dir", "install -d -m 0700 /etc/vpnctl/agents && install -d -m 0700 /var/lib/vpnctl/nonces", nil},
		{"public key", "cat > /etc/vpnctl/agents/" + operator + ".pub", pubPEM},
		{"authorized_keys", "install -d -m 0700 /root/.ssh && touch /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys && grep -qF 'vpnctl-agent:" + operator + "' /root/.ssh/authorized_keys || printf '%s\\n' " + shellQuote(line) + " >> /root/.ssh/authorized_keys", nil},
	}
	for _, s := range steps {
		out, err := c.Run(s.script, s.stdin)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", host, s.name, err)
		}
		if !out.OK() {
			return fmt.Errorf("%s: %s: %s", host, s.name, strings.TrimSpace(string(out.Stderr)))
		}
	}
	// prove the agent answers before the key is used for anything real
	req, err := agent.NewRequest(key, "plan", host, "", nil)
	if err != nil {
		return err
	}
	resp, err := agent.Call(c, req)
	if err != nil {
		return fmt.Errorf("%s: the agent did not answer: %w", host, err)
	}
	return emit(struct {
		Host     string `json:"host"`
		Operator string `json:"operator"`
		Marker   string `json:"marker"`
		Booted   bool   `json:"bootstrapped"`
	}{host, operator, resp.Marker, resp.Bootstrapped}, func() {
		fmt.Printf("✓ %s: agent installed, marker %q, bootstrapped %v\n", host, resp.Marker, resp.Bootstrapped)
		fmt.Printf("  key for %s is restricted to /usr/local/bin/vpnctl-agent\n", operator)
	})
}

// defaultAgentBinary looks for vpnctl-agent next to the running binary.
func defaultAgentBinary() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "vpnctl-agent")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "vpnctl-agent"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var _ = gitsync.Open
