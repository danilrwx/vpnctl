package mobile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
)

func TestWireGuardProfiles(t *testing.T) {
	a := openFixture(t)

	const entry = "ru-msk-1"
	if _, err := a.SaveInbound(entry, "wg", 51821, "", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := a.AddUser("laptop", "split,full", "wg", false, ""); err != nil {
		t.Fatal(err)
	}

	rows, err := a.UserProfiles("laptop")
	if err != nil {
		t.Fatal(err)
	}

	var profiles []UserProfile
	if err := json.Unmarshal([]byte(rows), &profiles); err != nil {
		t.Fatal(err)
	}

	if len(profiles) != 2 {
		t.Fatalf("expected both WG modes, got %d profiles", len(profiles))
	}

	for i, mode := range []string{"split", "full"} {
		p := profiles[i]
		if p.Mode != mode || p.Protocol != "wg" || !strings.Contains(p.Content, "AllowedIPs = 0.0.0.0/0, ::/0") || strings.Contains(p.Content, "Jc =") {
			t.Fatalf("invalid %s plain WireGuard profile", mode)
		}
	}

	user := a.st.Users["laptop"]

	keys := a.st.Secrets.Users["laptop"].WG
	if user.WG["split"] == user.WG["full"] || keys["split"].Public == keys["full"].Public {
		t.Fatal("modes must have separate WireGuard peers")
	}

	files, err := render.RenderHost(a.st, entry, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"split", "full"} {
		if !strings.Contains(files[render.WGConfig], "AllowedIPs = "+user.WG[mode]+"/32") {
			t.Fatalf("the server is missing the %s peer", mode)
		}
	}

	if !strings.Contains(files["/etc/nftables.conf"], `iifname "wg0" meta l4proto`) ||
		!strings.Contains(files["/etc/sing-box/config.json"], "10.19.1.0/24") ||
		!strings.Contains(files["/etc/sing-box/config.json"], "10.19.2.0/24") {
		t.Fatal("WireGuard traffic must enter the same mode routing as proxy traffic")
	}
}
