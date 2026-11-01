// Package hostscript writes the shell that runs on a host: the pre-swap checks,
// the atomic swap, the rollback and the probes. It is a pure function of the
// plan, so the scripts can be reviewed as text and compared as golden files.
package hostscript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// ServiceFor is the service to reload after a path changes.
func ServiceFor(path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "/etc/sing-box/"):
		return "sing-box", true
	case strings.HasPrefix(path, "/etc/xray/"):
		return "xray", true
	case path == "/etc/caddy/Caddyfile":
		return "caddy", true
	case strings.HasPrefix(path, "/etc/mtg/"):
		return "mtg", true
	case strings.HasPrefix(path, "/etc/amnezia/amneziawg/"):
		f := strings.TrimPrefix(path, "/etc/amnezia/amneziawg/")
		return "awg-quick@" + strings.TrimSuffix(f, ".conf"), true
	case path == "/etc/nftables.conf":
		return "nftables", true
	case strings.HasPrefix(path, "/etc/sysctl.d/"):
		return "sysctl", true
	case strings.HasPrefix(path, "/etc/systemd/system/"):
		return "systemd", true
	}

	return "", false
}

func reloadCmd(svc string) string {
	switch svc {
	case "nftables":
		return "nft -f /etc/nftables.conf"
	case "sysctl":
		return "sysctl --system >/dev/null"
	case "systemd":
		return "systemctl daemon-reload"
	case "caddy":
		return "systemctl enable -q caddy; systemctl reload caddy || systemctl restart caddy"
	}

	return fmt.Sprintf("systemctl enable -q %s; systemctl restart %s", svc, svc)
}

// CheckCmd is the pre-swap syntax check of a config on the host.
func CheckCmd(engine, path string) (string, bool) {
	tmp := "/tmp/.vpnctl" + path
	switch {
	case path == render.EngineConfigPath("sing-box") && render.EngineBin(engine) == "sing-box":
		// rule sets are relative: ours are already in tmp, downloaded .srs come from the host
		return fmt.Sprintf("mkdir -p /tmp/.vpnctl/etc/sing-box/rules && cp -n /etc/sing-box/rules/*.srs /tmp/.vpnctl/etc/sing-box/rules/ 2>/dev/null; sing-box check -D /tmp/.vpnctl/etc/sing-box -c %s", tmp), true
	case path == render.EngineConfigPath("xray") && render.EngineBin(engine) == "xray":
		return "xray run -test -c " + tmp, true
	case path == "/etc/nftables.conf":
		return "nft -c -f " + tmp, true
	case path == "/etc/caddy/Caddyfile":
		return "caddy validate --config " + tmp + " --adapter caddyfile >/dev/null", true
	}

	return "", false
}

// hasString reports whether list contains v.
func hasString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
}

// ModeFor is the file mode on the host: secrets are 0600.
func ModeFor(path string) int64 {
	if strings.HasPrefix(path, "/etc/amnezia/") ||
		strings.HasPrefix(path, "/etc/vpnctl/") ||
		strings.HasPrefix(path, "/etc/sing-box/") ||
		strings.HasPrefix(path, "/etc/xray/") ||
		strings.HasPrefix(path, "/etc/mtg/") {
		return 0o600
	}

	return 0o644
}

// ModeForInt is ModeFor with an int: the agent request carries JSON modes.
func ModeForInt(path string) int { return int(ModeFor(path)) }

// InstallScript backs up, swaps, removes the extra files and reloads the affected services.
func InstallScript(hp plan.HostPlan, backup string) string {
	var s strings.Builder
	s.WriteString("set -e\n")
	fmt.Fprintf(&s, "mkdir -p %s\n", backup)

	var (
		services []string
		ensure   []string
	)

	add := func(svc string) {
		for _, x := range services {
			if x == svc {
				return
			}
		}

		services = append(services, svc)
	}
	// A unit that is already on the host unchanged still has to run: bootstrap
	// installs and enables units but never starts them, and a service whose only
	// file is its unit would otherwise wait for the next config change.
	need := func(svc string) {
		for _, x := range ensure {
			if x == svc {
				return
			}
		}

		ensure = append(ensure, svc)
	}

	for _, f := range hp.Files {
		path := f.Path
		if u, ok := strings.CutPrefix(path, "/etc/systemd/system/"); ok {
			if name, ok := strings.CutSuffix(u, ".service"); ok {
				need(name)
			} else if strings.HasSuffix(u, ".timer") {
				need(u)
			}
		}

		switch f.Status {
		case plan.Unchanged:
			continue
		case plan.Removed:
			fmt.Fprintf(&s, "cp --parents -a '%s' %s 2>/dev/null || true; rm -f '%s'\n", path, backup, path)

			if svc, ok := ServiceFor(path); ok && strings.HasPrefix(svc, "awg-quick@") {
				fmt.Fprintf(&s, "systemctl disable --now -q %s || true\n", svc)
				continue
			}
		case plan.New:
			fmt.Fprintf(&s, "echo '%s' >> %s/.new; install -D -m %04o '/tmp/.vpnctl%s' '%s'\n",
				path, backup, ModeFor(path), path, path)
		default: // Changed, Drift
			fmt.Fprintf(&s, "cp --parents -a '%s' %s; install -D -m %04o '/tmp/.vpnctl%s' '%s'\n",
				path, backup, ModeFor(path), path, path)
		}

		if svc, ok := ServiceFor(path); ok {
			add(svc)
		}
		// a changed unit restarts its service, not just daemon-reload; a timer
		// is enabled and restarted by its own name
		if u, ok := strings.CutPrefix(path, "/etc/systemd/system/"); ok {
			if name, ok := strings.CutSuffix(u, ".service"); ok {
				add(name)
			} else if strings.HasSuffix(u, ".timer") {
				add(u)
			}
		}
	}
	// systemd and network before services, engine last
	rank := func(s string) int {
		switch {
		case s == "systemd":
			return 0
		case s == "sysctl":
			return 1
		case s == "nftables":
			return 2
		case strings.HasPrefix(s, "awg-quick@"):
			return 3
		case s == "caddy":
			return 4
		case s == "mtg":
			return 6
		}

		return 5
	}

	sort.SliceStable(services, func(i, j int) bool { return rank(services[i]) < rank(services[j]) })

	for _, svc := range services {
		s.WriteString(reloadCmd(svc))
		s.WriteString("\n")
	}

	sort.Strings(ensure)

	for _, svc := range ensure {
		if hasString(services, svc) {
			continue
		}

		fmt.Fprintf(&s, "systemctl enable -q %s; systemctl is-active --quiet %s || systemctl start %s\n", svc, svc, svc)
	}

	s.WriteString("rm -rf /tmp/.vpnctl\necho ok\n")

	return s.String()
}

// RollbackScript restores the backup, drops the new files and reloads the same services.
func RollbackScript(hp plan.HostPlan, backup string) string {
	var s strings.Builder
	s.WriteString("set +e\n")
	fmt.Fprintf(&s, "if [ -f %s/.new ]; then xargs -r rm -f < %s/.new; fi\n", backup, backup)
	fmt.Fprintf(&s, "(cd %s && find . -type f ! -name .new | while read f; do install -D -m 0600 \"$f\" \"/${f#./}\"; done)\n", backup)

	var services []string

	seen := map[string]bool{}

	for _, f := range hp.Files {
		if f.Status == plan.Unchanged {
			continue
		}

		svc, ok := ServiceFor(f.Path)
		if !ok || seen[svc] {
			continue
		}

		seen[svc] = true
		services = append(services, svc)
	}

	sort.Strings(services)

	for _, svc := range services {
		s.WriteString(reloadCmd(svc))
		s.WriteString("\n")
	}

	return s.String()
}

// ProbeScript is the chain probe after the swap: 204 from the endpoint means the path works.
func ProbeScript(role state.Role) string {
	if role == state.Entry {
		return fmt.Sprintf(
			"curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 --socks5-hostname 127.0.0.1:%d %s",
			render.ProbePort, render.ProbeURL)
	}
	// on an exit the engine is not in the probe path, so require it to be alive
	return fmt.Sprintf(
		"systemctl is-active --quiet sing-box 2>/dev/null || systemctl is-active --quiet xray 2>/dev/null || exit 1; curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 %s",
		render.ProbeURL)
}

// ManifestScript records what was just applied: drift later means "someone
// edited this file", not "the renderer changed since then".
func ManifestScript(files render.HostFiles) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	var b strings.Builder
	fmt.Fprintf(&b, "cat > %s <<'MANIFEST'\n", plan.Manifest)

	for _, p := range paths {
		fmt.Fprintf(&b, "%s %s\n", render.Sha256(files[p]), p)
	}

	b.WriteString("MANIFEST")

	return b.String()
}
