package plan

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// fixturePlan renders the shared fixture and returns the state, the tree and a
// prober that answers as if a host had applied it: this is the whole PlanWith
// path without SSH, because the prober owns the connection.
func fixturePlan(t *testing.T, tweak func(p Probe) Probe) (*state.State, render.Rendered, Prober) {
	t.Helper()

	st, err := state.Load(filepath.Join("..", "..", "testdata", "basic"))
	if err != nil {
		t.Fatal(err)
	}

	st.FillStubSecrets()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	rendered, err := render.Check(st, at)
	if err != nil {
		t.Fatal(err)
	}

	prober := func(host string, paths []string) (Probe, error) {
		p := Probe{Hashes: map[string]string{}, Missing: map[string]bool{}, HasMarker: true, Marker: "abc1234", Bootstrapped: true}
		// the manifest is what the host recorded at its last apply: it is the
		// baseline drift is measured against
		p.Manifest = map[string]string{}
		for path, body := range rendered[host] {
			p.Hashes[path] = render.Sha256(body)
			p.Manifest[path] = p.Hashes[path]
		}

		if tweak != nil {
			p = tweak(p)
		}

		return p, nil
	}

	return st, rendered, prober
}

// TestPlanCleanWhenTheHostMatches: nothing to do is a state worth testing, it is
// the answer an operator reads every day.
func TestPlanCleanWhenTheHostMatches(t *testing.T) {
	st, rendered, prober := fixturePlan(t, nil)

	plans, err := PlanWith(st, rendered, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), prober)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range plans {
		var changed []string

		for _, f := range p.Files {
			if f.Status != Unchanged {
				changed = append(changed, f.Path)
			}
		}

		if p.Unreachable != "" || len(changed) != 0 {
			t.Errorf("%s: %d changes %v, unreachable %q", p.Name, len(changed), changed, p.Unreachable)
		}
	}
}

// TestPlanDetectsDriftAndNew: a hand edit on the host is drift, a file the render
// grew is new, and neither is the same as "will change".
func TestPlanDetectsDriftAndNew(t *testing.T) {
	st, rendered, prober := fixturePlan(t, func(p Probe) Probe {
		for path := range p.Hashes {
			p.Hashes[path] = "0000"

			break
		}

		delete(p.Hashes, "/etc/caddy/Caddyfile")
		p.Missing["/etc/caddy/Caddyfile"] = true

		return p
	})

	plans, err := PlanWith(st, rendered, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), prober)
	if err != nil {
		t.Fatal(err)
	}

	var drift, fresh int

	for _, p := range plans {
		for _, f := range p.Files {
			switch f.Status {
			case Drift:
				drift++
			case New:
				fresh++
			}
		}
	}

	if drift == 0 {
		t.Error("an edited file was not reported as drift")
	}

	if fresh == 0 {
		t.Error("a file the host lacks was not reported as new")
	}
}

// TestPlanUnreachable: a host that cannot be asked is a note, not a crash.
func TestPlanUnreachable(t *testing.T) {
	st, rendered, _ := fixturePlan(t, nil)
	failing := func(string, []string) (Probe, error) { return Probe{}, errors.New("no route to host") }

	plans, err := PlanWith(st, rendered, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), failing)
	if err != nil {
		t.Fatal(err)
	}

	if len(plans) == 0 || plans[0].Unreachable == "" {
		t.Fatalf("%+v", plans)
	}
}
