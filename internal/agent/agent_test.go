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

// TestMarkWritesTheRecords swaps the write whitelist for a temp root: the paths
// the agent may touch are the only thing that changes, not the code under test.
func TestMarkWritesTheRecords(t *testing.T) {
	root := t.TempDir()

	old := AllowedPrefixes
	AllowedPrefixes = []string{root + "/"}

	t.Cleanup(func() { AllowedPrefixes = old })

	marker := filepath.Join(root, "vpnctl", "applied-commit")
	manifest := filepath.Join(root, "vpnctl", "applied.sha256")

	res := Mark(&Request{Files: []File{
		file(marker, "abc1234\n", 0o644),
		file(manifest, "deadbeef /etc/sing-box/config.json\n", 0o644),
	}})
	if !res.OK {
		t.Fatalf("mark refused: %s: %s", res.Code, res.Error)
	}

	for path, want := range map[string]string{
		marker:   "abc1234\n",
		manifest: "deadbeef /etc/sing-box/config.json\n",
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if string(body) != want {
			t.Errorf("%s: %q, want %q", path, body, want)
		}
	}
}

// TestMarkRefusesAPathOutsideTheWhitelist: the op writes two known files, so a
// request that names anything else is a bug or an attack, and both must stop.
func TestMarkRefusesAPathOutsideTheWhitelist(t *testing.T) {
	root := t.TempDir()

	old := AllowedPrefixes
	AllowedPrefixes = []string{root + "/"}

	t.Cleanup(func() { AllowedPrefixes = old })

	res := Mark(&Request{Files: []File{file("/root/.ssh/authorized_keys", "x\n", 0o644)}})
	if res.OK {
		t.Fatal("a path outside the whitelist was written")
	}

	if res.Code != "path" {
		t.Errorf("code %q, want path", res.Code)
	}
}

// TestMarkRefusesABodyThatDoesNotMatchItsHash keeps the manifest honest: the
// hash in the request is what the host records about itself.
func TestMarkRefusesABodyThatDoesNotMatchItsHash(t *testing.T) {
	root := t.TempDir()

	old := AllowedPrefixes
	AllowedPrefixes = []string{root + "/"}

	t.Cleanup(func() { AllowedPrefixes = old })

	f := file(filepath.Join(root, "applied-commit"), "abc\n", 0o644)
	f.Body = base64.StdEncoding.EncodeToString([]byte("something else\n"))

	res := Mark(&Request{Files: []File{f}})
	if res.OK || res.Code != "tree" {
		t.Fatalf("a mismatched body was written: %+v", res)
	}
}

// TestReadOnlyServesTheManifest: the read op exists so a phone can show drift.
// The manifest is the allow list, so a signed request cannot read anything the
// agent did not install itself.
func TestReadOnlyServesTheManifest(t *testing.T) {
	root := t.TempDir()

	old := AllowedPrefixes
	AllowedPrefixes = []string{root + "/"}

	t.Cleanup(func() { AllowedPrefixes = old })

	config := filepath.Join(root, "config.json")
	if err := os.WriteFile(config, []byte("{\"a\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := filepath.Join(root, "applied.sha256")
	if err := os.WriteFile(manifest, []byte("deadbeef "+config+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Read(&Request{Paths: []string{config}}, manifest)
	if !res.OK {
		t.Fatalf("read refused: %s: %s", res.Code, res.Error)
	}

	raw, err := base64.StdEncoding.DecodeString(res.Bodies[config])
	if err != nil {
		t.Fatal(err)
	}

	if string(raw) != "{\"a\":1}\n" {
		t.Fatalf("body %q", raw)
	}

	secret := filepath.Join(root, "shadow")
	if err := os.WriteFile(secret, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if res := Read(&Request{Paths: []string{secret}}, manifest); res.OK {
		t.Fatal("a file outside the manifest was readable")
	}
}
