package mobile

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func TestProtocolsForUserOwnEntry(t *testing.T) {
	a := openFixture(t)
	h := a.st.Hosts["ru-msk-1"]
	a.st.Hosts["other"] = h
	a.st.Inbounds["other"] = map[string]state.Inbound{"hy2": {Port: 443}, "awg": {Port: 51820}}
	u := a.st.Users["brat"]
	u.Entry = "other"

	a.st.Users["brat"] = u
	if got := a.ProtocolsForUser("brat"); got != `["awg","hy2"]` {
		t.Fatal(got)
	}

	if got := a.ProtocolsForUser("unknown"); got != "[]" {
		t.Fatal(got)
	}

	if got := a.ProtocolsForUser("olga"); got != "[]" {
		t.Fatal("ambiguous entry: " + got)
	}
}

func TestDisabledHY2Handoff(t *testing.T) {
	a := openFixture(t)
	if _, err := a.AddUser("hyguard", "split,full", "hy2", false, ""); err != nil {
		t.Fatal(err)
	}

	u := a.st.Users["hyguard"]
	off := false
	u.Enabled = &off

	a.st.Users["hyguard"] = u
	if links, err := a.Links("hyguard"); err != nil || links != "[]" {
		t.Fatalf("disabled user: %s %v", links, err)
	}

	u.Enabled = nil
	a.st.Users["hyguard"] = u
	in := a.st.Inbounds["ru-msk-1"]["hy2"]
	in.Enabled = &off

	a.st.Inbounds["ru-msk-1"]["hy2"] = in
	if links, err := a.Links("hyguard"); err != nil || links != "[]" {
		t.Fatalf("disabled inbound: %s %v", links, err)
	}

	if got := a.ProtocolsForUser("hyguard"); strings.Contains(got, "hy2") {
		t.Fatal("disabled inbound offered: " + got)
	}
}

func TestSaveAWGInboundPersistsKeys(t *testing.T) {
	a := openFixture(t)
	host := a.st.Secrets.Hosts["ru-msk-1"]
	host.AWG = nil
	a.st.Secrets.Hosts["ru-msk-1"] = host
	delete(a.st.Inbounds["ru-msk-1"], "awg")

	if _, err := a.SaveInbound("ru-msk-1", "awg", 51821, "", ""); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(a.dir, "")
	if err != nil {
		t.Fatal(err)
	}

	if reopened.st.Secrets.Hosts["ru-msk-1"].AWG == nil || reopened.st.Secrets.Hosts["ru-msk-1"].AWG.Private == "" {
		t.Fatal("generated host keys were not persisted")
	}

	if _, err := reopened.UserProfiles("brat"); err != nil {
		t.Fatal(err)
	}
}

func TestUserMutationSaveFailureRestoresMemory(t *testing.T) {
	a := openFixture(t)

	before, err := json.Marshal(a.st)
	if err != nil {
		t.Fatal(err)
	}

	a.dir = filepath.Join(a.dir, "missing", "directory")
	if _, err := a.AddUser("unsaved", "split", "hy2,awg", false, ""); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}

	after, err := json.Marshal(a.st)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Fatal("failed save retained mutation")
	}
}
