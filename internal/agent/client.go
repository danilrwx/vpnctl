package agent

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/internal/remote"
)

// KeyPath is the operator's signing key: the one that authorises an apply.
func KeyPath() string {
	if p := os.Getenv("VPNCTL_APPLY_KEY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "vpnctl", "apply.key")
}

// LoadKey reads the signing key, creating it on first use.
func LoadKey(path string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, gerr := newKey()
		if gerr != nil {
			return nil, gerr
		}
		if werr := SaveKey(path, key); werr != nil {
			return nil, werr
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("%s: not a PEM file", path)
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	any, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	key, ok := any.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ECDSA key", path)
	}
	return key, nil
}

// SaveKey writes the signing key with owner-only permissions.
func SaveKey(path string, key *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	body := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	return os.WriteFile(path, body, 0o600)
}

// PublicPEM is what goes into /etc/vpnctl/agents/<name>.pub on the host.
func PublicPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// NewRequest builds and signs a request for one host.
func NewRequest(key *ecdsa.PrivateKey, op, host, commit string, files []File) (*Request, error) {
	r := &Request{
		V: Version, Op: op, Host: host, Commit: commit,
		Files: files, TS: nowUnix(), Nonce: newNonce(),
	}
	r.TreeHash = TreeHash(files)
	sig, err := Sign(r, key)
	if err != nil {
		return nil, err
	}
	r.Sig = sig
	return r, nil
}

// Call sends the request to the agent over the existing SSH connection. The
// agent is installed as a forced command, so this is the only thing the key can
// do on the host.
func Call(c *remote.Client, r *Request) (Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return Response{}, err
	}
	out, err := c.Run("vpnctl-agent", body)
	if err != nil {
		return Response{}, err
	}
	var res Response
	if jerr := json.Unmarshal(out.Stdout, &res); jerr != nil {
		msg := string(out.Stderr)
		if msg == "" {
			msg = string(out.Stdout)
		}
		return Response{}, fmt.Errorf("%s: agent answered with code %d: %s", c.Name, out.Code, msg)
	}
	if !res.OK {
		return res, &CallError{Code: res.Code, Msg: res.Error}
	}
	return res, nil
}

// CallError is a refusal by the agent with its own code.
type CallError struct {
	Code string
	Msg  string
}

func (e *CallError) Error() string { return fmt.Sprintf("agent: %s: %s", e.Code, e.Msg) }

// FilesFromTree turns rendered host files into request entries.
func FilesFromTree(files map[string]string, mode func(string) int) ([]File, error) {
	out := make([]File, 0, len(files))
	for path, body := range files {
		f, err := FileOf(path, body, mode(path))
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// FileOf stamps one file with its hash and body.
func FileOf(path, body string, mode int) (File, error) {
	sum, err := sumOf(body)
	if err != nil {
		return File{}, err
	}
	return File{Path: path, SHA256: sum, Mode: mode, Body: encodeBody(body)}, nil
}
