// Package remote runs commands on the hosts over SSH: the jump chain comes from the
// state's `via` graph, host keys are pinned from the state, keys come from the agent
// or ~/.ssh. No ssh binary: the phone has none.
package remote

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/danilrwx/vpnctl-go/state"
)

const (
	sshPort  = "22"
	timeout  = 10 * time.Second
	userHint = "root"
)

// Client is one SSH connection to a host, opened through its jump chain.
type Client struct {
	Name  string
	addr  string
	cfg   *ssh.ClientConfig
	chain []string
	conn  *ssh.Client
}

// JumpChain is the host's via, its via and so on, outermost first: a, then b, then the host.
func JumpChain(st *state.State, host string) ([]string, error) {
	var chain []string
	name := host
	for {
		h, err := st.Host(name)
		if err != nil {
			return nil, err
		}
		if h.Via == "" {
			break
		}
		for _, c := range chain {
			if c == h.Via || h.Via == host {
				return nil, fmt.Errorf("cycle in the via graph at host %s", host)
			}
		}
		chain = append(chain, h.Via)
		name = h.Via
	}
	// built innermost first, the dial order is the other way round
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// Dial opens the connection: client -> jump -> host.
func Dial(st *state.State, host string) (*Client, error) {
	h, err := st.Host(host)
	if err != nil {
		return nil, err
	}
	chain, err := JumpChain(st, host)
	if err != nil {
		return nil, err
	}
	auth, err := authMethods()
	if err != nil {
		return nil, err
	}
	addr, err := address(h)
	if err != nil {
		return nil, err
	}
	c := &Client{Name: host, addr: addr, chain: chain}
	c.cfg = &ssh.ClientConfig{
		User:              user(h),
		Auth:              auth,
		HostKeyCallback:   hostKeyCallback(h.HostKey),
		HostKeyAlgorithms: hostKeyAlgorithms(h.HostKey),
		Timeout:           timeout,
	}
	var conn *ssh.Client
	if len(chain) == 0 {
		conn, err = ssh.Dial("tcp", addr, c.cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", host, err)
		}
	} else {
		first, err := st.Host(chain[0])
		if err != nil {
			return nil, err
		}
		jumpAddr, err := address(first)
		if err != nil {
			return nil, err
		}
		jumpCfg := &ssh.ClientConfig{
			User:              user(first),
			Auth:              auth,
			HostKeyCallback:   hostKeyCallback(first.HostKey),
			HostKeyAlgorithms: hostKeyAlgorithms(first.HostKey),
			Timeout:           timeout,
		}
		conn, err = ssh.Dial("tcp", jumpAddr, jumpCfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", chain[0], err)
		}
		for _, name := range chain[1:] {
			j, err := st.Host(name)
			if err != nil {
				conn.Close()
				return nil, err
			}
			a, err := address(j)
			if err != nil {
				conn.Close()
				return nil, err
			}
			next, err := dialThrough(conn, a, &ssh.ClientConfig{
				User:              user(j),
				Auth:              auth,
				HostKeyCallback:   hostKeyCallback(j.HostKey),
				HostKeyAlgorithms: hostKeyAlgorithms(j.HostKey),
				Timeout:           timeout,
			})
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			conn = next
		}
		next, err := dialThrough(conn, addr, c.cfg)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("%s: %w", host, err)
		}
		conn = next
	}
	c.conn = conn
	return c, nil
}

func dialThrough(jump *ssh.Client, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	nc, err := jump.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

// Close ends the connection, every hop of the chain with it.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Result is a finished remote command.
type Result struct {
	Stdout []byte
	Stderr []byte
	Code   int
}

// OK reports a zero exit status.
func (r Result) OK() bool { return r.Code == 0 }

// Run executes the script, stdin is sent as the command input when not nil.
func (c *Client) Run(script string, stdin []byte) (Result, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("%s: session: %w", c.Name, err)
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errb
	if stdin != nil {
		sess.Stdin = bytes.NewReader(stdin)
	}
	err = sess.Run(script)
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	if err != nil {
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			res.Code = ee.ExitStatus()
			return res, nil
		}
		return res, fmt.Errorf("%s: %w", c.Name, err)
	}
	return res, nil
}

// SH runs the script and treats a non-zero status as an error.
func (c *Client) SH(script string) (string, error) {
	res, err := c.Run(script, nil)
	if err != nil {
		return "", err
	}
	if !res.OK() {
		return "", fmt.Errorf("%s: %s", c.Name, strings.TrimSpace(string(res.Stderr)))
	}
	return string(res.Stdout), nil
}

// Reachable checks that the host answers.
func (c *Client) Reachable() error {
	res, err := c.Run("true", nil)
	if err != nil {
		return err
	}
	if !res.OK() {
		msg := strings.TrimSpace(string(res.Stderr))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = "no answer"
		}
		return errors.New(msg)
	}
	return nil
}

// ScanKey is the host public key in authorized_keys form, without the host name.
func ScanKey(st *state.State, host string) (string, error) {
	h, err := st.Host(host)
	if err != nil {
		return "", err
	}
	chain, err := JumpChain(st, host)
	if err != nil {
		return "", err
	}
	var addr string
	var dialer *ssh.Client
	if len(chain) > 0 {
		first, err := st.Host(chain[0])
		if err != nil {
			return "", err
		}
		jumpAddr, err := address(first)
		if err != nil {
			return "", err
		}
		auth, err := authMethods()
		if err != nil {
			return "", err
		}
		dialer, err = ssh.Dial("tcp", jumpAddr, &ssh.ClientConfig{
			User: user(first), Auth: auth, HostKeyCallback: hostKeyCallback(first.HostKey),
			HostKeyAlgorithms: hostKeyAlgorithms(first.HostKey), Timeout: timeout,
		})
		if err != nil {
			return "", fmt.Errorf("%s: %w", chain[0], err)
		}
		defer dialer.Close()
	}
	addr, err = address(h)
	if err != nil {
		return "", err
	}
	var conn net.Conn
	if dialer != nil {
		conn, err = dialer.Dial("tcp", addr)
	} else {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User:    user(h),
		Timeout: timeout,
		// ssh-keyscan -t ed25519, the same as the Rust core asks for
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return nil
		},
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return "", fmt.Errorf("handshake %s: %w", addr, err)
	}
	ssh.NewClient(cc, chans, reqs).Close()
	if key == nil {
		return "", fmt.Errorf("%s: ssh-keyscan returned nothing", addr)
	}
	return key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal()), nil
}

// hostKeyAlgorithms limits the negotiation to the pinned key type: the server may offer an
// ECDSA key first, and x/crypto/ssh would pick it instead of the recorded ed25519 one.
func hostKeyAlgorithms(pinned string) []string {
	if f := strings.Fields(pinned); len(f) > 0 {
		return []string{f[0]}
	}
	return nil
}

// hostKeyCallback pins the key recorded in the state; without one the key is accepted.
func hostKeyCallback(want string) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		if want == "" {
			return nil
		}
		got := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
		if strings.TrimSpace(got) != strings.TrimSpace(want) {
			return fmt.Errorf("host key mismatch for %s", hostname)
		}
		return nil
	}
}

func address(h state.Host) (string, error) {
	if h.IP == "" {
		return "", fmt.Errorf("host %s has no ip", h.SSH)
	}
	return net.JoinHostPort(h.IP, sshPort), nil
}

// user is `user@host` from the ssh field, root when absent.
func user(h state.Host) string {
	if i := strings.LastIndex(h.SSH, "@"); i > 0 {
		return h.SSH[:i]
	}
	return userHint
}

func authMethods() ([]ssh.AuthMethod, error) {
	var m []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			m = append(m, ssh.PublicKeysCallback(ag.Signers))
		}
	}
	for _, p := range keyPaths() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		k, err := ssh.ParsePrivateKey(b)
		if err != nil {
			continue
		}
		m = append(m, ssh.PublicKeys(k))
	}
	if len(m) == 0 {
		return nil, errors.New("no ssh key: no agent and no key in ~/.ssh")
	}
	return m, nil
}

func keyPaths() []string {
	if p := os.Getenv("VPNCTL_SSH_KEY"); p != "" {
		return []string{p}
	}
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".ssh/id_ed25519"),
		filepath.Join(home, ".ssh/id_ecdsa"),
		filepath.Join(home, ".ssh/id_rsa"),
	}
}
