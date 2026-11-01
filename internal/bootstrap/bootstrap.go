// Package bootstrap renders and runs the idempotent host setup script: packages,
// pinned binaries with sha256, rule sets, units, nftables and sysctl.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

const (
	// SingBoxURL, XrayURL, CaddyURL and MTGURL are release templates; {v} is the pin.
	SingBoxURL = "https://github.com/SagerNet/sing-box/releases/download/v{v}/sing-box-{v}-linux-amd64.tar.gz"
	XrayURL    = "https://github.com/XTLS/Xray-core/releases/download/v{v}/Xray-linux-64.zip"
	CaddyURL   = "https://github.com/caddyserver/caddy/releases/download/v{v}/caddy_{v}_linux_amd64.tar.gz"
	MTGURL     = "https://github.com/9seconds/mtg/releases/download/v{v}/mtg-{v}-linux-amd64.tar.gz"
	NtfyURL    = "https://github.com/binwiederhier/ntfy/releases/download/v{v}/ntfy_{v}_linux_amd64.tar.gz"
)

// Download is one file the bootstrap fetches and installs.
type Download struct {
	Name    string
	Version string
	URL     string
	SHA     string
	File    string
	Check   string
	Install string
}

// RuleSet is one downloaded .srs file.
type RuleSet struct {
	Tag string
	URL string
}

// RuleSetTags lists the binary rule sets the rendered config refers to.
func RuleSetTags(configJSON string) []string {
	var cfg struct {
		Route struct {
			RuleSet []struct {
				Format string `json:"format"`
				Tag    string `json:"tag"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range cfg.Route.RuleSet {
		if r.Format != "binary" || r.Tag == "" || seen[r.Tag] {
			continue
		}
		seen[r.Tag] = true
		out = append(out, r.Tag)
	}
	sort.Strings(out)
	return out
}

// RuleSetURL maps a tag to its release: geosite-x and geoip-x live in two repos.
func RuleSetURL(tag, release string) string {
	repo := "sing-geosite"
	if strings.HasPrefix(tag, "geoip-") {
		repo = "sing-geoip"
	}
	if release == "latest" {
		return fmt.Sprintf("https://raw.githubusercontent.com/SagerNet/%s/rule-set/%s.srs", repo, tag)
	}
	return fmt.Sprintf("https://github.com/SagerNet/%s/releases/download/%s/%s.srs", repo, release, tag)
}

// RebootDays is the auto-reboot schedule: odd days for an entry, even for exits.
func RebootDays(role state.Role) string {
	start := 1
	if role != state.Entry {
		start = 2
	}
	var days []string
	for d := start; d <= 31; d += 2 {
		days = append(days, fmt.Sprintf("%02d", d))
	}
	return strings.Join(days, ",")
}

func pin(st *state.State, name string) (state.Pin, error) {
	p, ok := st.Versions[name]
	if !ok {
		return state.Pin{}, fmt.Errorf("versions.yaml has no %q", name)
	}
	return p, nil
}

func download(name string, p state.Pin, url, check, install string) Download {
	u := strings.ReplaceAll(url, "{v}", p.Version)
	file := u
	if i := strings.LastIndex(u, "/"); i >= 0 {
		file = u[i+1:]
	}
	return Download{Name: name, Version: p.Version, URL: u, SHA: p.SHA256, File: file, Check: check, Install: install}
}

// Script is the bootstrap script of a host: a pure function of the state and the render.
func Script(st *state.State, host string, at time.Time) (string, error) {
	h, err := st.Host(host)
	if err != nil {
		return "", err
	}
	files, err := render.RenderHost(st, host, at)
	if err != nil {
		return "", err
	}
	const dl = "/var/lib/vpnctl/dl"
	var downloads []Download
	switch render.EngineBin(h.Engine) {
	case "sing-box":
		p, err := pin(st, "sing-box")
		if err != nil {
			return "", err
		}
		v := p.Version
		downloads = append(downloads, download("sing-box", p, SingBoxURL,
			fmt.Sprintf("sing-box version 2>/dev/null | grep -q 'version %s$'", v),
			fmt.Sprintf("tar -xzf %s/sing-box-%s-linux-amd64.tar.gz -C %s && install -m 0755 %s/sing-box-%s-linux-amd64/sing-box /usr/local/bin/sing-box && mkdir -p /etc/sing-box/rules", dl, v, dl, dl, v)))
	default:
		p, err := pin(st, "xray")
		if err != nil {
			return "", err
		}
		v := p.Version
		downloads = append(downloads, download("xray", p, XrayURL,
			fmt.Sprintf("xray version 2>/dev/null | grep -q '^Xray %s '", v),
			fmt.Sprintf("rm -rf %s/xray && mkdir -p %s/xray && unzip -qo %s/Xray-linux-64.zip -d %s/xray && install -m 0755 %s/xray/xray /usr/local/bin/xray && mkdir -p /etc/xray", dl, dl, dl, dl, dl)))
		// unlike sing-box, xray reads geosite.dat/geoip.dat from the binary directory
		g, err := pin(st, "geodata")
		if err != nil {
			return "", err
		}
		for _, f := range []string{"geosite", "geoip"} {
			url := fmt.Sprintf("https://github.com/Loyalsoldier/v2ray-rules-dat/releases/{v}/download/%s.dat", f)
			downloads = append(downloads, download(f, g, url,
				fmt.Sprintf("test -s /usr/local/bin/%s.dat", f),
				fmt.Sprintf("install -m 0644 %s/%s.dat /usr/local/bin/%s.dat", dl, f, f)))
		}
	}
	if h.Role == state.Entry {
		p, err := pin(st, "caddy")
		if err != nil {
			return "", err
		}
		downloads = append(downloads, download("caddy", p, CaddyURL,
			fmt.Sprintf("caddy version 2>/dev/null | grep -q '^v%s '", p.Version),
			fmt.Sprintf("tar -xzf %s/caddy_%s_linux_amd64.tar.gz -C %s caddy && install -m 0755 %s/caddy /usr/local/bin/caddy", dl, p.Version, dl, dl)))
		m, err := pin(st, "mtg")
		if err != nil {
			return "", err
		}
		downloads = append(downloads, download("mtg", m, MTGURL,
			fmt.Sprintf("mtg --version 2>/dev/null | grep -q '%s'", m.Version),
			fmt.Sprintf("tar -xzf %s/mtg-%s-linux-amd64.tar.gz -C %s && install -m 0755 %s/mtg-%s-linux-amd64/mtg /usr/local/bin/mtg", dl, m.Version, dl, dl, m.Version)))
		// the notification server rides on the same host only when alerts say so
		if st.Alerts.Serve == host {
			n, err := pin(st, "ntfy")
			if err != nil {
				return "", err
			}
			downloads = append(downloads, download("ntfy", n, NtfyURL,
				fmt.Sprintf("ntfy --version 2>/dev/null | grep -q 'version %s$'", n.Version),
				fmt.Sprintf("tar -xzf %s/ntfy_%s_linux_amd64.tar.gz -C %s && install -m 0755 %s/ntfy_%s_linux_amd64/ntfy /usr/local/bin/ntfy", dl, n.Version, dl, dl, n.Version)))
		}
	}
	geodata, err := pin(st, "geodata")
	if err != nil {
		return "", err
	}
	var ruleSets []RuleSet
	if render.EngineBin(h.Engine) == "sing-box" {
		cfg := files[render.EngineConfigPath(h.Engine)]
		for _, tag := range RuleSetTags(cfg) {
			ruleSets = append(ruleSets, RuleSet{Tag: tag, URL: RuleSetURL(tag, geodata.Version)})
		}
	}
	var units []map[string]string
	var enable []string
	for path, body := range files {
		if name, ok := strings.CutPrefix(path, "/etc/systemd/system/"); ok {
			units = append(units, map[string]string{"name": name, "body": body})
			enable = append(enable, name)
		}
		if f, ok := strings.CutPrefix(path, "/etc/amnezia/amneziawg/"); ok {
			enable = append(enable, "awg-quick@"+strings.TrimSuffix(f, ".conf"))
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i]["name"] < units[j]["name"] })
	if h.Role == state.Entry {
		enable = append(enable, "caddy")
	}
	sort.Strings(enable)
	// AWG is needed when there is an awg inbound or an awg link involving the host
	awg := false
	if _, ok := st.Inbounds[host]["awg"]; ok {
		awg = true
	}
	for _, l := range st.Links {
		if (l.From == host || l.To == host) && containsStr(l.Protocols, "awg") {
			awg = true
		}
	}
	commit := "-"
	if repo, err := gitsync.Open(st.Dir); err == nil {
		if h, err := repo.Head(); err == nil {
			commit = h
		}
	}
	awgPin, err := pin(st, "awg")
	if err != nil {
		return "", err
	}
	role := "exit"
	if h.Role == state.Entry {
		role = "entry"
	}
	// the template reads lowercase keys: gonja looks up struct fields by name
	dlCtx := make([]map[string]string, 0, len(downloads))
	for _, d := range downloads {
		dlCtx = append(dlCtx, map[string]string{
			"name": d.Name, "version": d.Version, "url": d.URL,
			"sha": d.SHA, "file": d.File, "check": d.Check, "install": d.Install,
		})
	}
	rsCtx := make([]map[string]string, 0, len(ruleSets))
	for _, r := range ruleSets {
		rsCtx = append(rsCtx, map[string]string{"tag": r.Tag, "url": r.URL})
	}
	return render.Template(st, "bootstrap.sh.j2", map[string]any{
		"host":        host,
		"role":        role,
		"domain":      h.Domain,
		"reboot_days": RebootDays(h.Role),
		"downloads":   dlCtx,
		"rule_sets":   rsCtx,
		"geodata":     geodata.Version,
		"awg":         awg,
		"awg_version": awgPin.Version,
		"nftables":    files["/etc/nftables.conf"],
		"sysctl":      files["/etc/sysctl.d/90-vpnctl.conf"],
		"units":       units,
		"enable":      enable,
		"commit":      commit,
	})
}

// Run records the host key when it is missing, then executes the script over SSH.
func Run(st *state.State, host, sshOverride string, out io.Writer, at time.Time) error {
	h, err := st.Host(host)
	if err != nil {
		return err
	}
	changed := false
	if sshOverride != "" && sshOverride != h.SSH {
		h.SSH = sshOverride
		st.Hosts[host] = h
		changed = true
	}
	if h.HostKey == "" {
		key, err := remote.ScanKey(st, host)
		if err != nil {
			return err
		}
		h = st.Hosts[host]
		h.HostKey = key
		st.Hosts[host] = h
		changed = true
		fmt.Fprintf(out, "  ✓ host key accepted, recorded in the state\n")
	}
	if changed {
		if err := state.SaveHosts(st.Dir, st.Hosts); err != nil {
			return err
		}
		repo, err := gitsync.Open(st.Dir)
		if err != nil {
			return err
		}
		if err := repo.Commit(fmt.Sprintf("hosts: %s: host key", host)); err != nil {
			return err
		}
	}
	script, err := Script(st, host, at)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "▸ bootstrap %s · %s\n", host, h.SSH)
	c, err := remote.Dial(st, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Stream("sh -s", []byte(script), out); err != nil {
		return fmt.Errorf("bootstrap %s failed; a re-run will finish the rest: %w", host, err)
	}
	return nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
