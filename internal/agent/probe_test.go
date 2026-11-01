package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// TestProbeNeverLies: a protocol we cannot handshake is not green. awg needs the
// peer, and a TCP connect to a broken reality is exactly the lie the old matrix
// told.
func TestProbeNeverLies(t *testing.T) {
	detail, ok := ProbeCell("203.0.113.1", state.ProbeCell{Protocol: "awg", Port: 51820}, time.Second)
	if ok {
		t.Fatal("awg reported a handshake it never made")
	}
	if !strings.Contains(detail, "unverified") {
		t.Errorf("detail %q does not say unverified", detail)
	}
	detail, ok = ProbeCell("203.0.113.1", state.ProbeCell{Protocol: "trojan", Port: 443}, time.Second)
	if ok || !strings.Contains(detail, "no probe") {
		t.Errorf("an unknown protocol is not reported: %q %v", detail, ok)
	}
}

// TestRunProbePlanEmpty: an entry with no links measures nothing and says so,
// instead of failing on an empty plan.
func TestRunProbePlanEmpty(t *testing.T) {
	rows, err := RunProbePlan([]byte(`{"host":"e","links":[]}`), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("%v", rows)
	}
}

// TestRunProbePlanShapes: the plan's cells become rows with the edge name, in
// the order the state lists them.
func TestRunProbePlanShapes(t *testing.T) {
	body := []byte(`{"host":"e","links":[{"from":"e","to":"x","addr":"127.0.0.1","probes":[
		{"protocol":"awg","port":51820},
		{"protocol":"trojan","port":443}]}]}`)
	rows, err := RunProbePlan(body, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Link != "e->x" || rows[0].Protocol != "awg" {
		t.Fatalf("%v", rows)
	}
	if rows[0].OK || rows[1].OK {
		t.Error("a cell with no handshake came out green")
	}
}
