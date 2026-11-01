package agent

import (
	"strings"
	"testing"
)

// recorder replaces the shell: the test asserts the sequence an agent would run
// on a host, without root, /etc or a network.
type recorder struct {
	ran    []string
	failOn func(cmd string) error
	out    string
}

func (r *recorder) install(t *testing.T) {
	t.Helper()

	oldRun, oldOut := runSh, outputSh

	t.Cleanup(func() { runSh, outputSh = oldRun, oldOut })

	runSh = func(script string) error {
		r.ran = append(r.ran, script)
		if r.failOn != nil {
			return r.failOn(script)
		}

		return nil
	}
	outputSh = func(script string) (string, error) {
		r.ran = append(r.ran, script)
		if r.failOn != nil {
			if err := r.failOn(script); err != nil {
				return "", err
			}
		}

		return r.out, nil
	}
}

func (r *recorder) saw(sub string) bool {
	for _, c := range r.ran {
		if strings.Contains(c, sub) {
			return true
		}
	}

	return false
}

func applyRequest(t *testing.T) *Request {
	t.Helper()

	cfg, err := FileOf("/etc/sing-box/config.json", `{"inbounds":[]}`, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	caddy, err := FileOf("/etc/caddy/Caddyfile", "ohmymail.ru {\n}\n", 0o644)
	if err != nil {
		t.Fatal(err)
	}

	r := &Request{V: Version, Op: "apply", Host: "node", Commit: "abc1234", Files: []File{cfg, caddy}}
	r.TreeHash = TreeHash(r.Files)

	return r
}

// TestApplyMockedHappyPath: the agent checks what it can, swaps, probes, and
// only then reports success. No root, no host: the shell is a recorder.
func TestApplyMockedHappyPath(t *testing.T) {
	rec := &recorder{out: "204"}
	rec.install(t)

	res := Apply(applyRequest(t), t.TempDir(), func(string) {})
	if !res.OK {
		t.Fatalf("apply failed: %+v", res)
	}

	if res.Probe != "204" {
		t.Fatalf("probe %q", res.Probe)
	}

	if !rec.saw("sing-box check") {
		t.Error("the engine config was not checked before the swap")
	}

	if !rec.saw("install -D") {
		t.Error("nothing was installed")
	}

	if !rec.saw("systemctl") {
		t.Error("the engine was never restarted")
	}

	if rec.saw("RollbackScript") {
		t.Error("a successful apply rolled back")
	}

	if len(res.Changes) != 2 || res.Changes[0].Status != "+" {
		t.Fatalf("changes %+v", res.Changes)
	}
}

// TestApplyMockedRollsBack: a config the engine rejects never reaches /etc, and
// a chain that stops answering puts the old files back.
func TestApplyMockedRollsBack(t *testing.T) {
	t.Run("the check fails", func(t *testing.T) {
		rec := &recorder{out: "204", failOn: func(cmd string) error {
			if strings.Contains(cmd, " check ") {
				return errBoom
			}

			return nil
		}}
		rec.install(t)

		res := Apply(applyRequest(t), t.TempDir(), func(string) {})
		if res.OK || res.Code != "check" {
			t.Fatalf("%+v", res)
		}

		if rec.saw("install -D") {
			t.Error("the files were installed after a failed check")
		}
	})
	t.Run("the probe fails", func(t *testing.T) {
		rec := &recorder{out: "000"}
		rec.install(t)

		res := Apply(applyRequest(t), t.TempDir(), func(string) {})
		if res.OK || res.Code != "probe" || !res.RolledBack {
			t.Fatalf("%+v", res)
		}
		// the rollback script is the one that runs with set +e and restores the
		// backup instead of claiming success
		if !rec.saw("set +e") {
			t.Error("no rollback was sent")
		}
	})
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }
