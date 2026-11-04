package ops

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func awgFixture(t *testing.T) *state.State {
	t.Helper()

	st := fixture(t)
	if _, err := InboundSet(st, "a", "awg", 51820, "", ""); err != nil {
		t.Fatal(err)
	}

	return st
}

func snapshotAWG(t *testing.T, st *state.State) string {
	t.Helper()

	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestAWGUserLifecycle(t *testing.T) {
	st := awgFixture(t)
	if _, err := UserAdd(st, "first", []string{"split", "full"}, []string{"awg"}, false); err != nil {
		t.Fatal(err)
	}

	if got := st.Users["first"].AWG; got == nil || got.IP != "10.8.1.2" || got.Mode != "split" {
		t.Fatalf("peer %+v", got)
	}

	sec := st.Secrets.Users["first"]
	if sec.AWG == nil || sec.AWG.Private == "" || sec.AWG.Public == "" || sec.AWGPreshared == "" {
		t.Fatalf("secrets %+v", sec)
	}

	u := st.Users["first"]
	u.Enabled = ptr(false)

	st.Users["first"] = u
	if _, err := UserEdit(st, "u", UserPatch{Protocols: []string{"awg"}}); err != nil {
		t.Fatal(err)
	}

	if got := st.Users["u"].AWG.IP; got != "10.8.1.3" {
		t.Fatalf("disabled peer reused: %s", got)
	}

	if _, err := UserEdit(st, "first", UserPatch{Modes: []string{"full", "split"}}); err != nil {
		t.Fatal(err)
	}

	if got := st.Users["first"].AWG.Mode; got != "split" {
		t.Fatalf("mode changed: %s", got)
	}

	if _, err := UserEdit(st, "first", UserPatch{Modes: []string{"full"}}); err != nil {
		t.Fatal(err)
	}

	if got := st.Users["first"].AWG; got.Mode != "full" || got.IP != "10.8.2.2" {
		t.Fatalf("peer %+v", got)
	}

	if !reflect.DeepEqual(sec, st.Secrets.Users["first"]) {
		t.Fatal("mode change rotated secrets")
	}

	if _, err := UserAdd(st, "reuse", []string{"split"}, []string{"awg"}, false); err != nil {
		t.Fatal(err)
	}

	if st.Users["reuse"].AWG.IP != "10.8.1.2" {
		t.Fatal("did not allocate first free address")
	}

	if _, err := UserEdit(st, "first", UserPatch{Protocols: []string{"reality"}}); err != nil {
		t.Fatal(err)
	}

	after := st.Secrets.Users["first"]
	if st.Users["first"].AWG != nil || after.AWG != nil || after.AWGPreshared != "" {
		t.Fatal("AWG survived removal")
	}

	sec.AWG = nil

	sec.AWGPreshared = ""
	if !reflect.DeepEqual(sec, after) {
		t.Fatal("non-AWG secrets changed")
	}

	if _, err := UserRm(st, "reuse"); err != nil {
		t.Fatal(err)
	}

	if _, ok := st.Secrets.Users["reuse"]; ok {
		t.Fatal("removed user's secrets survived")
	}
}

func TestAWGUserRollback(t *testing.T) {
	cases := map[string]func(*state.State){
		"missing inbound": func(st *state.State) { delete(st.Inbounds["a"], "awg") },
		"disabled": func(st *state.State) {
			i := st.Inbounds["a"]["awg"]
			i.Enabled = ptr(false)
			st.Inbounds["a"]["awg"] = i
		},
		"xray":            func(st *state.State) { h := st.Hosts["a"]; h.Engine = "xray"; st.Hosts["a"] = h },
		"ambiguous entry": func(st *state.State) { st.Hosts["c"] = state.Host{Role: state.Entry} },
		"missing subnet":  func(st *state.State) { i := st.Inbounds["a"]["awg"]; i.Subnets = nil; st.Inbounds["a"]["awg"] = i },
	}
	for _, subnet := range []string{"bad", "2001:db8::/64", "10.8.1.1/24", "10.8.1.0/31", "255.255.255.255/32"} {
		cases[subnet] = func(st *state.State) { st.Inbounds["a"]["awg"].Subnets.Split = subnet }
	}

	cases["exhausted"] = func(st *state.State) {
		st.Inbounds["a"]["awg"].Subnets.Split = "10.8.1.0/30"
		st.Users["disabled"] = state.User{Enabled: ptr(false), AWG: &state.Peer{Mode: "split", IP: "10.8.1.2"}}
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			st := awgFixture(t)
			setup(st)

			before := snapshotAWG(t, st)
			if _, err := UserAdd(st, "new", []string{"split"}, []string{"awg"}, false); err == nil {
				t.Fatal("add accepted")
			}

			if before != snapshotAWG(t, st) {
				t.Fatal("add mutated state")
			}

			if _, err := UserEdit(st, "u", UserPatch{Protocols: []string{"awg"}, Note: ptr("changed")}); err == nil {
				t.Fatal("edit accepted")
			}

			if before != snapshotAWG(t, st) {
				t.Fatal("edit mutated state")
			}
		})
	}

	st := awgFixture(t)

	before := snapshotAWG(t, st)
	if _, err := UserAdd(st, "bad", []string{"bogus"}, []string{"awg"}, false); err == nil {
		t.Fatal("invalid mode accepted")
	}

	if _, err := UserEdit(st, "u", UserPatch{Modes: []string{"split", "bogus"}, Protocols: []string{"awg"}}); err == nil {
		t.Fatal("invalid mode edit accepted")
	}

	if before != snapshotAWG(t, st) {
		t.Fatal("invalid modes mutated state")
	}
}

func TestAWGModeChangeRollback(t *testing.T) {
	st := awgFixture(t)
	if _, err := UserEdit(st, "u", UserPatch{Protocols: []string{"awg"}}); err != nil {
		t.Fatal(err)
	}

	st.Inbounds["a"]["awg"].Subnets.Full = "10.8.2.0/31"

	before := snapshotAWG(t, st)
	if _, err := UserEdit(st, "u", UserPatch{Modes: []string{"full"}}); err == nil {
		t.Fatal("exhausted mode accepted")
	}

	if before != snapshotAWG(t, st) {
		t.Fatal("failed mode change mutated state")
	}
}

func TestAWGSelectedEntryAndReservedAddresses(t *testing.T) {
	st := awgFixture(t)

	st.Hosts["c"] = state.Host{Role: state.Entry, Engine: "sing-box"}
	if _, err := InboundSet(st, "c", "awg", 51820, "", ""); err != nil {
		t.Fatal(err)
	}

	st.Inbounds["c"]["awg"].Subnets.Split = "192.0.2.0/30"
	u := st.Users["u"]
	u.Entry = "c"

	st.Users["u"] = u
	if _, err := UserEdit(st, "u", UserPatch{Protocols: []string{"awg"}}); err != nil {
		t.Fatal(err)
	}

	if got := st.Users["u"].AWG.IP; got != "192.0.2.2" {
		t.Fatalf("selected entry address %s", got)
	}

	st.Users["other"] = state.User{Entry: "c", Modes: []string{"split"}, Protocols: []string{"reality"}}

	before := snapshotAWG(t, st)
	if _, err := UserEdit(st, "other", UserPatch{Protocols: []string{"awg"}}); err == nil {
		t.Fatal("allocated network, gateway, or broadcast after sole usable peer")
	}

	if before != snapshotAWG(t, st) {
		t.Fatal("exhaustion mutated state")
	}
}

func TestAWGInboundDefaultsAndReuse(t *testing.T) {
	st := awgFixture(t)

	i := st.Inbounds["a"]["awg"]
	if i.Subnets.Split != "10.8.1.0/24" || i.Subnets.Full != "10.8.2.0/24" {
		t.Fatalf("defaults %+v", i.Subnets)
	}

	hs := st.Secrets.Hosts["a"]
	if hs.AWG == nil || hs.AWG.Private == "" || hs.AWG.Public == "" || hs.Reality == nil {
		t.Fatalf("host secrets %+v", hs)
	}

	i.Subnets.Split = "10.99.0.0/24"
	i.Subnets.Full = ""

	if _, err := InboundSet(st, "a", "awg", 51821, "", ""); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(hs, st.Secrets.Hosts["a"]) {
		t.Fatal("host secrets replaced")
	}

	got := st.Inbounds["a"]["awg"].Subnets
	if got.Split != "10.99.0.0/24" || got.Full != "10.8.2.0/24" {
		t.Fatalf("subnets %+v", got)
	}

	for _, proto := range []string{"awg", "hy2"} {
		h := st.Hosts["a"]
		h.Engine = "xray"
		st.Hosts["a"] = h

		before := snapshotAWG(t, st)
		if _, err := InboundSet(st, "a", proto, 12345, "", ""); err == nil {
			t.Fatalf("xray accepted %s", proto)
		}

		if before != snapshotAWG(t, st) {
			t.Fatal("unsupported inbound mutated state")
		}
	}

	h := st.Hosts["b"]
	h.Engine = "xray"

	st.Hosts["b"] = h
	if _, err := InboundSet(st, "b", "awg", 51820, "", ""); err != nil {
		t.Fatal(err)
	}

	if st.Inbounds["b"]["awg"].Subnets != nil || st.Secrets.Hosts["b"].AWG == nil {
		t.Fatal("exit AWG setup incorrect")
	}
}
