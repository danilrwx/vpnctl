package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"github.com/danilrwx/vpnctl-go/internal/validate"
)

// Checks the contract between saved mobile state and every consumer of a
// profile: host render, API profile and the generated Kotlin surface.
func TestSavedAWGProfileContract(t *testing.T) {
	a := openFixture(t)
	if _, err := a.AddUser("contract", "full", "awg", false, ""); err != nil {
		t.Fatal(err)
	}

	if err := a.save("contract fixture"); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(a.dir, "")
	if err != nil {
		t.Fatal(err)
	}

	u := reopened.st.Users["contract"]
	if u.AWG == nil || u.AWG.IP != "10.8.2.2" || u.AWG.Mode != "full" {
		t.Fatalf("persisted peer: %+v", u.AWG)
	}

	t.Log("OBSERVED: persisted full-mode peer is 10.8.2.2 after reopen")

	if errs := validate.State(reopened.st); len(errs) > 0 {
		t.Fatalf("saved contract state rejected: %v", errs)
	}

	t.Log("OBSERVED: reopened state with generated AWG material passes validate.State")

	files, err := render.RenderHost(reopened.st, "ru-msk-1", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	hostProfile := files["/etc/vpnctl/profiles/contract/awg.conf"]

	apiProfile, err := render.AWGClient(reopened.st, "ru-msk-1", "contract")
	if err != nil {
		t.Fatal(err)
	}

	if hostProfile != apiProfile {
		t.Fatal("host file and mobile profile differ")
	}

	for _, want := range []string{
		"mode full", "Address = 10.8.2.2/32", "DNS = 10.8.2.1",
		"Endpoint = gw.example.ru:51820", "AllowedIPs = 0.0.0.0/0, ::/0",
		"Jc = 4", "H1 = 1", "PersistentKeepalive = 25",
	} {
		if !strings.Contains(apiProfile, want) {
			t.Fatalf("profile lacks %q:\n%s", want, apiProfile)
		}
	}

	if strings.Contains(apiProfile, "10.8.1.2") {
		t.Fatal("full profile leaked a split client address")
	}

	t.Log("OBSERVED: host render and API profile are byte-identical; full mode has full subnet DNS, gateway, endpoint and obfuscation fields")

	rows, err := reopened.UserProfiles("contract")
	if err != nil {
		t.Fatal(err)
	}

	var profiles []struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Mode     string `json:"mode"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rows), &profiles); err != nil {
		t.Fatal(err)
	}

	if len(profiles) != 1 || profiles[0].Protocol != "awg" || profiles[0].Mode != "full" ||
		profiles[0].Filename != "contract-awg-full.conf" || profiles[0].Content != apiProfile {
		t.Fatalf("JSON profile contract: %s", rows)
	}

	t.Log("OBSERVED: mobile JSON has exact Kotlin field names, mode-specific filename and byte-identical content")

	if err := os.MkdirAll(filepath.Join(reopened.dir, "overrides", "ru-msk-1", "etc/vpnctl/profiles", "contract"), 0o700); err != nil {
		t.Fatal(err)
	}

	override := filepath.Join(reopened.dir, "overrides", "ru-msk-1", "etc/vpnctl/profiles", "contract", "awg.conf")
	if err := os.WriteFile(override, []byte("adopted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err = render.RenderHost(reopened.st, "ru-msk-1", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	adopted, err := render.AWGClient(reopened.st, "ru-msk-1", "contract")
	if err != nil {
		t.Fatal(err)
	}

	if files["/etc/vpnctl/profiles/contract/awg.conf"] != adopted || adopted != "adopted\n" {
		t.Fatal("adopted override diverged between host and mobile")
	}

	t.Log("OBSERVED: adopted operator override is identical in host render and mobile download")
}

// Invalid persisted peer material must never leak a key-bearing profile.
func TestSavedAWGProfileContractRejectsCorruption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*state.State)
	}{
		{"wrong mode", func(st *state.State) { u := st.Users["brat"]; u.AWG.Mode = "bogus"; st.Users["brat"] = u }},
		{"mode missing from user", func(st *state.State) { u := st.Users["brat"]; u.Modes = []string{"full"}; st.Users["brat"] = u }},
		{"gateway address", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.8.1.1"; st.Users["brat"] = u }},
		{"network address", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.8.1.0"; st.Users["brat"] = u }},
		{"broadcast address", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.8.1.255"; st.Users["brat"] = u }},
		{"ipv6 in ipv4 subnet", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "2001:db8::2"; st.Users["brat"] = u }},
		{"empty private key", func(st *state.State) {
			sec := st.Secrets.Users["brat"]
			sec.AWG.Private = ""
			st.Secrets.Users["brat"] = sec
		}},
		{"empty psk", func(st *state.State) {
			sec := st.Secrets.Users["brat"]
			sec.AWGPreshared = ""
			st.Secrets.Users["brat"] = sec
		}},
		{"empty server key", func(st *state.State) {
			hs := st.Secrets.Hosts["ru-msk-1"]
			hs.AWG.Public = ""
			st.Secrets.Hosts["ru-msk-1"] = hs
		}},
		{"missing port", func(st *state.State) {
			i := st.Inbounds["ru-msk-1"]["awg"]
			i.Port = 0
			st.Inbounds["ru-msk-1"]["awg"] = i
		}},
		{"port out of range", func(st *state.State) {
			i := st.Inbounds["ru-msk-1"]["awg"]
			i.Port = 70000
			st.Inbounds["ru-msk-1"]["awg"] = i
		}},
		{"malformed subnet", func(st *state.State) { st.Inbounds["ru-msk-1"]["awg"].Subnets.Split = "10.8.1.2/24" }},
		{"ipv6 subnet", func(st *state.State) {
			st.Inbounds["ru-msk-1"]["awg"].Subnets.Split = "2001:db8::/64"
		}},
		{"wrong entry", func(st *state.State) { u := st.Users["brat"]; u.Entry = "fi-hel-1"; st.Users["brat"] = u }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openFixture(t).st
			tc.mutate(st)

			if body, err := render.AWGClient(st, "ru-msk-1", "brat"); err == nil || body != "" {
				t.Fatalf("leaked a profile: %q %v", body, err)
			}
		})
	}
}
