// Package apply installs the render on the hosts: guards, copy over the connection,
// config check, atomic swap, probe, marker. Hosts go leaves first.
package apply

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/remote"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Opts are the apply switches.
type Opts struct {
	Host   string
	Branch string
	Force  bool
}

// Order is hosts from leaves to the jump: the longer the via chain, the earlier.
func Order(st *state.State, names []string) []string {
	type item struct {
		depth int
		name  string
	}
	items := make([]item, 0, len(names))
	for _, n := range names {
		d := 0
		if chain, err := remote.JumpChain(st, n); err == nil {
			d = len(chain)
		}
		items = append(items, item{d, n})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].depth != items[j].depth {
			return items[i].depth > items[j].depth
		}
		return items[i].name < items[j].name
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

// Guards refuse to apply a dirty worktree or a branch other than the expected one.
func Guards(st *state.State, opts *Opts) error {
	repo, err := gitsync.Open(st.Dir)
	if err != nil {
		return err
	}
	clean, err := repo.Clean()
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("dirty worktree: commit your changes before apply")
	}
	branch, err := repo.Branch()
	if err != nil {
		return err
	}
	want := "main"
	if opts.Branch != "" {
		want = opts.Branch
	}
	if branch != want {
		return fmt.Errorf("branch %s, only %s is applied (or pass --branch %s explicitly)", branch, want, branch)
	}
	return nil
}

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

// ModeFor is the file mode on the host: secrets are 0600.
func ModeFor(path string) int64 {
	if strings.HasPrefix(path, "/etc/amnezia/") ||
		strings.HasPrefix(path, "/etc/vpnctl/profiles/") ||
		strings.HasPrefix(path, "/etc/sing-box/") ||
		strings.HasPrefix(path, "/etc/xray/") ||
		strings.HasPrefix(path, "/etc/mtg/") {
		return 0o600
	}
	return 0o644
}

// Tarball packs every host file; paths are relative to the root, the result is reproducible.
func Tarball(files render.HostFiles) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	dirs := map[string]bool{}
	for _, p := range paths {
		parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}
	dirNames := make([]string, 0, len(dirs))
	for d := range dirs {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	epoch := time.Unix(0, 0).UTC()
	for _, d := range dirNames {
		if err := tw.WriteHeader(&tar.Header{
			Name: d + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: epoch, Format: tar.FormatUSTAR,
		}); err != nil {
			return nil, err
		}
	}
	for _, p := range paths {
		body := files[p]
		if err := tw.WriteHeader(&tar.Header{
			Name: strings.TrimPrefix(p, "/"), Mode: ModeFor(p), Size: int64(len(body)),
			Typeflag: tar.TypeReg, ModTime: epoch, Format: tar.FormatUSTAR,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// InstallScript backs up, swaps, removes the extra files and reloads the affected services.
func InstallScript(hp plan.HostPlan, backup string) string {
	var s strings.Builder
	s.WriteString("set -e\n")
	fmt.Fprintf(&s, "mkdir -p %s\n", backup)
	var services []string
	add := func(svc string) {
		for _, x := range services {
			if x == svc {
				return
			}
		}
		services = append(services, svc)
	}
	for _, f := range hp.Files {
		path := f.Path
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
		// a changed unit restarts its service, not just daemon-reload
		if u, ok := strings.CutPrefix(path, "/etc/systemd/system/"); ok {
			if u, ok := strings.CutSuffix(u, ".service"); ok {
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

// preflight checks the certificate and the active link protocol before the swap.
func preflight(st *state.State, c *remote.Client, name string, files render.HostFiles, force bool) ([]string, error) {
	var notes []string
	cert, _ := render.CertPaths(st, name)
	for _, body := range files {
		if !strings.Contains(body, cert) {
			continue
		}
		res, err := c.Run(fmt.Sprintf("test -s '%s'", cert), nil)
		if err != nil {
			return nil, err
		}
		if !res.OK() {
			msg := fmt.Sprintf("certificate %s is missing on the host: issue it first or wait for ACME", cert)
			if !force {
				return nil, errors.New(msg)
			}
			notes = append(notes, msg+" · --force")
		}
		break
	}
	for _, ol := range render.OutLinks(st, name) {
		l := ol.L
		if l.Active == "" {
			continue
		}
		to := st.Hosts[l.To]
		port := 443
		if i, ok := st.Inbounds[l.To][l.Active]; ok && i.Port != 0 {
			port = i.Port
		}
		var check string
		switch l.Active {
		case "awg":
			check = fmt.Sprintf(
				"ip link show awg%d >/dev/null 2>&1 || exit 0; h=$(awg show awg%d latest-handshakes 2>/dev/null | awk '{print $2}' | head -1); [ -n \"$h\" ] && [ $(( $(date +%%s) - h )) -lt 180 ]",
				ol.N, ol.N)
		case "hy2":
			// hysteria2 is UDP: a TCP connect to its port never succeeds. Liveness of the link
			// is checked by the chain probe after the configs are swapped.
			check = "true"
		default:
			check = fmt.Sprintf("timeout 5 bash -c 'exec 3<>/dev/tcp/%s/%d' 2>/dev/null", to.IP, port)
		}
		res, err := c.Run(check, nil)
		if err != nil {
			return nil, err
		}
		if !res.OK() {
			msg := fmt.Sprintf("link %s: active protocol %s is not responding", l.Key(), l.Active)
			if !force {
				return nil, fmt.Errorf("%s (use --force to apply anyway)", msg)
			}
			notes = append(notes, msg+" · --force")
		}
	}
	return notes, nil
}

// Apply renders, plans and installs the configs host by host.
func Apply(st *state.State, opts *Opts) error {
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Println("  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	if err := Guards(st, opts); err != nil {
		return err
	}
	repo, err := gitsync.Open(st.Dir)
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	rendered, err := render.Check(st)
	if err != nil {
		return err
	}
	plans, err := plan.Plan(st, rendered)
	if err != nil {
		return err
	}
	var names []string
	for _, p := range plans {
		if opts.Host != "" && p.Name != opts.Host {
			continue
		}
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("nothing to apply: host not found")
	}
	order := Order(st, names)
	branch, _ := repo.Branch()
	fmt.Printf("plan for %s @ %s · order: %s\n", branch, head, strings.Join(order, " → "))

	done := 0
	for _, name := range order {
		var hp plan.HostPlan
		for _, p := range plans {
			if p.Name == name {
				hp = p
			}
		}
		h := st.Hosts[name]
		fmt.Printf("▸ %s\n", name)
		if hp.Unreachable != "" {
			return fmt.Errorf("%s: %s — series stopped", name, hp.Unreachable)
		}
		if !hp.Bootstrapped {
			fmt.Println("  – skipped: bootstrap not done")
			continue
		}
		c, err := remote.Dial(st, name)
		if err != nil {
			return err
		}
		files := rendered[name]
		if hp.Changes() == 0 {
			if _, err := c.SH(fmt.Sprintf("mkdir -p /etc/vpnctl && echo %s > %s", head, plan.Marker)); err != nil {
				c.Close()
				return err
			}
			fmt.Printf("  – no changes, marker %s updated\n", head)
			c.Close()
			done++
			continue
		}
		notes, err := preflight(st, c, name, files, opts.Force)
		if err != nil {
			c.Close()
			return err
		}
		for _, n := range notes {
			fmt.Printf("  ! %s\n", n)
		}
		fmt.Printf("  ✓ render · %d files, %d changes\n", len(files), hp.Changes())
		tarball, err := Tarball(files)
		if err != nil {
			c.Close()
			return err
		}
		res, err := c.Run("rm -rf /tmp/.vpnctl && mkdir -p /tmp/.vpnctl && tar -xf - -C /tmp/.vpnctl", tarball)
		if err != nil {
			c.Close()
			return err
		}
		if !res.OK() {
			c.Close()
			return fmt.Errorf("%s: copy failed: %s", name, strings.TrimSpace(string(res.Stderr)))
		}
		fmt.Println("  ✓ copied over the connection → /tmp/.vpnctl/")
		for _, f := range hp.Files {
			if f.Status == plan.Unchanged || f.Status == plan.Removed {
				continue
			}
			cmd, ok := CheckCmd(h.Engine, f.Path)
			if !ok {
				continue
			}
			res, err := c.Run(cmd, nil)
			if err != nil {
				c.Close()
				return err
			}
			if !res.OK() {
				c.Run("rm -rf /tmp/.vpnctl", nil)
				c.Close()
				return fmt.Errorf("%s: check of %s failed:\n%s", name, f.Path, strings.TrimSpace(string(res.Stderr)))
			}
			fields := strings.Fields(cmd)
			if len(fields) > 2 {
				fields = fields[:2]
			}
			fmt.Printf("  ✓ %s\n", strings.Join(fields, " "))
		}
		backup := "/var/lib/vpnctl/backup/" + head
		res, err = c.Run(InstallScript(hp, backup), nil)
		if err != nil {
			c.Close()
			return err
		}
		if !res.OK() {
			fmt.Println(strings.TrimSpace(string(res.Stderr)))
			fmt.Println("  ✗ swap failed, rolled back to the backup")
			c.Run(RollbackScript(hp, backup), nil)
			c.Close()
			return fmt.Errorf("%s: swap failed — series stopped", name)
		}
		fmt.Println("  ✓ atomic swap · services reloaded")
		res, err = c.Run(ProbeScript(h.Role), nil)
		code := ""
		if err == nil {
			code = strings.TrimSpace(string(res.Stdout))
		}
		if code != "204" {
			if code == "" {
				code = "no answer"
			}
			fmt.Printf("  ✗ probe: %s instead of 204 · rollback\n", code)
			if res, err := c.Run(RollbackScript(hp, backup), nil); err == nil && !res.OK() {
				fmt.Println(strings.TrimSpace(string(res.Stderr)))
			}
			c.Close()
			return fmt.Errorf("%s: chain probe failed, config restored — series stopped", name)
		}
		via := "the exit"
		if h.Role == state.Entry {
			via = "the chain"
		}
		fmt.Printf("  ✓ probe: 204 via %s\n", via)
		if _, err := c.SH(fmt.Sprintf("mkdir -p /etc/vpnctl && echo %s > %s", head, plan.Marker)); err != nil {
			c.Close()
			return err
		}
		fmt.Printf("  ✓ marker %s written\n", head)
		c.Close()
		done++
	}
	if done > 0 && opts.Host == "" {
		tag := "applied/" + time.Now().UTC().Format("2006-01-02T15-04-05")
		if err := repo.Tag(tag); err != nil {
			return err
		}
		fmt.Printf("done %d of %d · tag %s\n", done, len(order), tag)
	} else {
		fmt.Printf("done %d of %d\n", done, len(order))
	}
	return nil
}
