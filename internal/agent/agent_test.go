package agent

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func file(path, body string, mode int) File {
	sum := sha256.Sum256([]byte(body))
	return File{Path: path, SHA256: hex.EncodeToString(sum[:]), Mode: mode, Body: base64.StdEncoding.EncodeToString([]byte(body))}
}

func request(t *testing.T, key *ecdsa.PrivateKey, files ...File) *Request {
	t.Helper()
	r := &Request{
		V: Version, Op: "apply", Host: "node", Commit: "abc1234",
		Files: files, TS: time.Now().Unix(), Nonce: "deadbeef",
	}
	r.TreeHash = TreeHash(files)
	sig, err := Sign(r, key)
	if err != nil {
		t.Fatal(err)
	}
	r.Sig = sig
	return r
}

func TestTreeHashIsStable(t *testing.T) {
	a := []File{file("/etc/b", "b", 0o644), file("/etc/a", "a", 0o600)}
	b := []File{file("/etc/a", "a", 0o600), file("/etc/b", "b", 0o644)}
	if TreeHash(a) != TreeHash(b) {
		t.Fatal("the order of the files changed the hash")
	}
	if TreeHash(a) == TreeHash([]File{file("/etc/a", "a", 0o644), file("/etc/b", "b", 0o644)}) {
		t.Fatal("a mode change did not change the hash")
	}
}

func TestVerifyAccepts(t *testing.T) {
	key := testKey(t)
	r := request(t, key, file("/etc/x.conf", "hello", 0o644))
	if err := Verify(r, "node", &key.PublicKey, time.Now()); err != nil {
		t.Fatalf("a valid request was rejected: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	key := testKey(t)
	other := testKey(t)
	cases := map[string]func(r *Request) error{
		"another host": func(r *Request) error { return Verify(r, "other", &key.PublicKey, time.Now()) },
		"another key":  func(r *Request) error { return Verify(r, "node", &other.PublicKey, time.Now()) }, // signature check
		"stale": func(r *Request) error {
			return Verify(r, "node", &key.PublicKey, time.Now().Add(2*ClockSkew))
		},
		"tampered file": func(r *Request) error {
			r.Files[0].Body = base64.StdEncoding.EncodeToString([]byte("evil"))
			return Verify(r, "node", &key.PublicKey, time.Now())
		},
		"wrong tree hash": func(r *Request) error {
			r.TreeHash = strings.Repeat("0", 64)
			return Verify(r, "node", &key.PublicKey, time.Now())
		},
		"bad version": func(r *Request) error {
			r.V = 99
			return Verify(r, "node", &key.PublicKey, time.Now())
		},
		"no nonce": func(r *Request) error {
			r.Nonce = ""
			return Verify(r, "node", &key.PublicKey, time.Now())
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			r := request(t, key, file("/etc/x.conf", "hello", 0o644))
			if err := run(r); err == nil {
				t.Fatal("the request was accepted")
			}
		})
	}
}

func TestCheckPath(t *testing.T) {
	ok := []string{"/etc/sing-box/config.json", "/etc/nftables.conf", "/var/www/sub/abc/split"}
	for _, p := range ok {
		if err := CheckPath(p, 0o644); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	bad := []string{
		"etc/relative.conf",
		"/etc/../root/.ssh/authorized_keys",
		"/root/.ssh/authorized_keys",
		"/usr/local/bin/sing-box",
		"/etc/passwd\n",
	}
	for _, p := range bad {
		if err := CheckPath(p, 0o644); err == nil {
			t.Errorf("%s was accepted", p)
		}
	}
	if err := CheckPath("/etc/x", 0o4755); err == nil {
		t.Error("a setuid mode was accepted")
	}
	if err := CheckPath("/etc/x", 0o755); err == nil {
		t.Error("mode 0755 was accepted")
	}
}

func TestCheckPathRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := "/tmp/vpnctl-agent-test-link"
	_ = os.Remove(link)
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	defer func() { _ = os.Remove(link) }()
	if err := CheckPath(link+"/x.conf", 0o644); err == nil {
		t.Fatal("a symlink in the path was accepted")
	}
}

func TestPlanResponse(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	bootstrap := filepath.Join(dir, "bootstrap")
	_ = os.WriteFile(marker, []byte("abc1234\n"), 0o644)
	_ = os.WriteFile(bootstrap, []byte("x"), 0o644)
	f := filepath.Join(dir, "f")
	_ = os.WriteFile(f, []byte("hello"), 0o644)
	r := &Request{V: Version, Op: "plan", Paths: []string{f, filepath.Join(dir, "missing")}}
	res := Plan(r, marker, bootstrap)
	if !res.OK || res.Marker != "abc1234" || !res.Bootstrapped {
		t.Fatalf("%+v", res)
	}
	if res.Hashes[f] == "" {
		t.Fatal("the hash is empty")
	}
	if v, ok := res.Hashes[filepath.Join(dir, "missing")]; !ok || v != "" {
		t.Fatalf("a missing path: %+v", res.Hashes)
	}
}

func TestParsePublicKey(t *testing.T) {
	key := testKey(t)
	pemBody, err := PublicPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey(pemBody); err != nil {
		t.Fatalf("the PEM written for the host is unreadable: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParsePublicKey(der)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(&key.PublicKey) {
		t.Fatal("the key changed")
	}
}
