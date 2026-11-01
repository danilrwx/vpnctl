// Package agent is the narrow privileged executor on a host: it takes a signed
// tree, checks it and installs it. No shell, no arbitrary paths, no self-started
// work — only the request it was given.
package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/state"
)

// Version is the protocol version this binary speaks.
const Version = 1

// AllowedPrefixes are the only places the agent writes. It runs as root, so the
// list is the difference between "one narrow thing" and "another sshd".
var AllowedPrefixes = []string{"/etc/", "/var/www/sub/"}

// AllowedModes are the only file modes a request may ask for.
var AllowedModes = []int{0o600, 0o644}

// ClockSkew is how old a signed request may be.
const ClockSkew = 5 * time.Minute

// File is one file of the tree.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   int    `json:"mode"`
	Body   string `json:"body"` // base64
}

// Request is what the client sends.
type Request struct {
	V        int      `json:"v"`
	Op       string   `json:"op"` // plan or apply
	Host     string   `json:"host"`
	Commit   string   `json:"commit"`
	TreeHash string   `json:"tree_hash"`
	TS       int64    `json:"ts"`
	Nonce    string   `json:"nonce"`
	Files    []File   `json:"files,omitempty"`
	Paths    []string `json:"paths,omitempty"` // plan only
	Sig      string   `json:"sig"`
}

// Response is what the host answers.
type Response struct {
	V            int               `json:"v"`
	OK           bool              `json:"ok"`
	Code         string            `json:"code,omitempty"`
	Error        string            `json:"error,omitempty"`
	Marker       string            `json:"marker,omitempty"`
	Bootstrapped bool              `json:"bootstrapped,omitempty"`
	Hashes       map[string]string `json:"hashes,omitempty"`
	Changes      []Change          `json:"changes,omitempty"`
	Probe        string            `json:"probe,omitempty"`
	RolledBack   bool              `json:"rolled_back,omitempty"`
	Log          []string          `json:"log,omitempty"`
}

// Change is one path the apply touched.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// Message is the exact string a request signs: v|op|host|commit|tree_hash|ts|nonce.
func Message(r *Request) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s|%d|%s",
		r.V, r.Op, r.Host, r.Commit, r.TreeHash, r.TS, r.Nonce)
}

// TreeHash is the digest of the file list: path\0sha256\0mode per line, sorted.
func TreeHash(files []File) string {
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("%s\x00%s\x00%d", f.Path, f.SHA256, f.Mode))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// Verify checks everything that can be checked before touching the host.
func Verify(r *Request, host string, pub *ecdsa.PublicKey, now time.Time) error {
	if r.V != Version {
		return &Error{Code: "version", Err: fmt.Errorf("protocol %d, this agent speaks %d", r.V, Version)}
	}
	if r.Host != host {
		return &Error{Code: "host", Err: fmt.Errorf("the request is for %q, this host is %q", r.Host, host)}
	}
	skew := now.Sub(time.Unix(r.TS, 0))
	if skew < -ClockSkew || skew > ClockSkew {
		return &Error{Code: "stale", Err: fmt.Errorf("request is %s old", skew.Round(time.Second))}
	}
	if r.Nonce == "" {
		return &Error{Code: "replay", Err: errors.New("no nonce")}
	}
	if got := TreeHash(r.Files); got != r.TreeHash {
		return &Error{Code: "tree", Err: fmt.Errorf("tree hash %s, files give %s", r.TreeHash, got)}
	}
	if err := verifySig(Message(r), r.Sig, pub); err != nil {
		return &Error{Code: "signature", Err: err}
	}
	for _, f := range r.Files {
		if err := CheckPath(f.Path, f.Mode); err != nil {
			return err
		}
		body, err := base64.StdEncoding.DecodeString(f.Body)
		if err != nil {
			return &Error{Code: "tree", Err: fmt.Errorf("%s: body: %w", f.Path, err)}
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return &Error{Code: "tree", Err: fmt.Errorf("%s: content does not match its hash", f.Path)}
		}
	}
	return nil
}

// CheckPath is the whitelist: absolute, inside the allowed prefixes, no
// traversal, an allowed mode and no symlink in the chain.
func CheckPath(path string, mode int) error {
	if !filepath.IsAbs(path) {
		return &Error{Code: "path", Err: fmt.Errorf("%s: not an absolute path", path)}
	}
	if strings.Contains(path, "..") {
		return &Error{Code: "path", Err: fmt.Errorf("%s: contains ..", path)}
	}
	if strings.ContainsFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return &Error{Code: "path", Err: fmt.Errorf("%q: control character", path)}
	}
	ok := false
	for _, p := range AllowedPrefixes {
		if strings.HasPrefix(path, p) {
			ok = true
			break
		}
	}
	if !ok {
		return &Error{Code: "path", Err: fmt.Errorf("%s: outside %s", path, strings.Join(AllowedPrefixes, ", "))}
	}
	allowed := false
	for _, m := range AllowedModes {
		if mode == m {
			allowed = true
		}
	}
	if !allowed {
		return &Error{Code: "path", Err: fmt.Errorf("%s: mode %o is not allowed", path, mode)}
	}
	// a symlink anywhere in the chain would redirect the write
	for p := path; p != "/" && p != "."; p = filepath.Dir(p) {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return &Error{Code: "path", Err: fmt.Errorf("%s: %s is a symlink", path, p)}
		}
	}
	return nil
}

// Error carries the machine-readable code the client decides on.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ParsePublicKey reads an ECDSA P-256 public key, PEM-wrapped or raw DER.
func ParsePublicKey(data []byte) (*ecdsa.PublicKey, error) {
	der := data
	if block, _ := pem.Decode(data); block != nil {
		der = block.Bytes
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		if cert, cerr := x509.ParseCertificate(der); cerr == nil {
			key = cert.PublicKey
		} else {
			return nil, err
		}
	}
	pub, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA key")
	}
	return pub, nil
}

func verifySig(msg, sigB64 string, pub *ecdsa.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	sum := sha256.Sum256([]byte(msg))
	if !ecdsa.VerifyASN1(pub, sum[:], raw) {
		return errors.New("signature does not match")
	}
	return nil
}

// Sign produces the signature of a request with the operator's key.
func Sign(r *Request, key *ecdsa.PrivateKey) (string, error) {
	sum := sha256.Sum256([]byte(Message(r)))
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Plan hashes the requested paths on the host and reports the marker.
func Plan(r *Request, markerPath, bootstrapPath string) Response {
	res := Response{V: Version, OK: true, Hashes: map[string]string{}}
	if m, err := os.ReadFile(markerPath); err == nil {
		res.Marker = strings.TrimSpace(string(m))
	}
	if _, err := os.Stat(bootstrapPath); err == nil {
		res.Bootstrapped = true
	}
	for _, p := range r.Paths {
		if _, err := os.Lstat(p); err != nil {
			res.Hashes[p] = ""
			continue
		}
		if sum, err := hashFile(p); err == nil {
			res.Hashes[p] = sum
		}
	}
	return res
}

// Apply stages the files, checks them, swaps them in and probes. On a failed
// check or probe it restores the backup and says so.
func Apply(r *Request, root string, out func(string)) Response {
	res := Response{V: Version, OK: true}
	// the engine and the role follow from the tree itself, no extra fields to sign
	engine, role := "sing-box", "exit"
	for _, f := range r.Files {
		if strings.HasPrefix(f.Path, "/etc/xray/") {
			engine = "xray"
		}
		if f.Path == "/etc/caddy/Caddyfile" {
			role = "entry"
		}
	}
	stage := filepath.Join(root, ".vpnctl")
	if err := os.RemoveAll(stage); err != nil {
		return fail(res, "io", err)
	}
	hp := plan.HostPlan{Name: r.Host}
	for _, f := range r.Files {
		body, err := base64.StdEncoding.DecodeString(f.Body)
		if err != nil {
			return fail(res, "tree", err)
		}
		dst := filepath.Join(stage, strings.TrimPrefix(f.Path, "/"))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fail(res, "io", err)
		}
		if err := os.WriteFile(dst, body, os.FileMode(f.Mode)); err != nil {
			return fail(res, "io", err)
		}
		status := plan.Changed
		if cur, err := hashFile(f.Path); err != nil {
			status = plan.New
		} else if cur == f.SHA256 {
			status = plan.Unchanged
		}
		hp.Files = append(hp.Files, plan.FileStatus{Path: f.Path, Status: status})
		res.Changes = append(res.Changes, Change{Path: f.Path, Status: status.Mark()})
	}
	// checks before the swap: a broken config never reaches /etc
	for _, f := range hp.Files {
		if f.Status == plan.Unchanged {
			continue
		}
		cmd, ok := apply.CheckCmd(engine, f.Path)
		if !ok {
			continue
		}
		if err := runSh(cmd); err != nil {
			out("check failed: " + f.Path)
			return fail(res, "check", err)
		}
		out("check ok: " + f.Path)
	}
	backup := filepath.Join(root, "backup", r.Commit)
	if err := runSh(apply.InstallScript(hp, backup)); err != nil {
		out("swap failed, rolling back")
		runSh(apply.RollbackScript(hp, backup))
		res.RolledBack = true
		return fail(res, "io", err)
	}
	out("swapped")
	probe := apply.ProbeScript(roleOf(role))
	code, err := outputSh(probe)
	if err != nil || strings.TrimSpace(code) != "204" {
		out("probe failed: " + strings.TrimSpace(code))
		runSh(apply.RollbackScript(hp, backup))
		res.RolledBack = true
		r2 := fail(res, "probe", fmt.Errorf("probe returned %q", strings.TrimSpace(code)))
		return r2
	}
	res.Probe = "204"
	return res
}

func roleOf(role string) state.Role {
	if role == "entry" {
		return state.Entry
	}
	return state.Exit
}

func fail(res Response, code string, err error) Response {
	res.OK = false
	res.Code = code
	res.Error = err.Error()
	return res
}

func hashFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func runSh(script string) error {
	return exec.Command("sh", "-c", script).Run()
}

func outputSh(script string) (string, error) {
	b, err := exec.Command("sh", "-c", script).Output()
	return string(b), err
}

func newKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func nowUnix() int64 { return time.Now().Unix() }

// newNonce is 16 random bytes in hex.
func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func sumOf(body string) (string, error) {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]), nil
}

func encodeBody(body string) string { return base64.StdEncoding.EncodeToString([]byte(body)) }
