// Package apply installs the render on the hosts: guards, copy over the connection,
// config check, atomic swap, probe, marker. Hosts go leaves first.
package apply

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/hostscript"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"github.com/danilrwx/vpnctl-go/internal/validate"
)

// Opts are the apply switches.
type Opts struct {
	Host   string
	Branch string
	Force  bool
	// Unpushed applies a commit that is not on origin/main: only when the
	// remote is unavailable, and the marker says so.
	Unpushed bool
	// AgentApply replaces the shell steps with one signed request to the host
	// agent. It is injected by the CLI so this package stays free of the agent.
	AgentApply func(c *remote.Client, host, commit string, files render.HostFiles) (HostResult, error)
	// At is the day expiry is measured against; the zero value means today.
	At time.Time
	// FetchAuth is the credential the guard uses to fetch origin. A device has
	// no key file and no ssh agent, so it passes the signer it already holds.
	FetchAuth transport.AuthMethod
	// Prober asks the hosts what they have. A phone can only reach them through
	// the agent, and the shell probe would see every host as unreachable.
	Prober plan.Prober
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

// Guards refuse to apply anything but a clean, pushed, current state.
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

	if opts.Unpushed {
		return nil
	}
	// Order is always commit → push → apply: the hosts must run what every
	// device sees, and the marker must point at a commit the others can render.
	fetch := repo.Fetch
	if opts.FetchAuth != nil {
		fetch = func() error { return repo.FetchAuth(opts.FetchAuth) }
	}

	if err := fetch(); err != nil {
		return fmt.Errorf("%w (use --unpushed when the remote is unavailable)", err)
	}

	head, err := repo.Head()
	if err != nil {
		return err
	}

	origin, err := repo.OriginMain()
	if err != nil {
		return fmt.Errorf("%w (use --unpushed when the remote is unavailable)", err)
	}

	if head != origin {
		return fmt.Errorf("HEAD %s is not pushed: origin/main is %s — push first, then apply (or --unpushed)", head, origin)
	}

	return nil
}

// checkAncestors refuses to roll a host back: the marker must be in the history
// of this state, otherwise another device has already applied a newer commit.
func checkAncestors(repo *gitsync.Repo, head string, plans []plan.HostPlan, names []string) error {
	for _, name := range names {
		for _, hp := range plans {
			if hp.Name != name || hp.Marker == "" || hp.Unpushed {
				continue
			}

			ok, err := repo.IsAncestor(hp.Marker, head)
			if err != nil {
				return fmt.Errorf("%s: marker %s: %w", name, hp.Marker, err)
			}

			if !ok {
				return fmt.Errorf("%s runs %s, which is not in the history of %s: another device is ahead — pull first", name, hp.Marker, head)
			}
		}
	}

	return nil
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
			Name: strings.TrimPrefix(p, "/"), Mode: hostscript.ModeFor(p), Size: int64(len(body)),
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

// HostResult is what happened on one host.
type HostResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // applied, unchanged, skipped, failed
	Changes int    `json:"changes"`
	Probe   string `json:"probe,omitempty"`
	Marker  string `json:"marker,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Result is the whole series.
type Result struct {
	Branch string       `json:"branch"`
	Head   string       `json:"head"`
	Hosts  []HostResult `json:"hosts"`
	Tag    string       `json:"tag,omitempty"`
}

// GuardError is a refusal by a guard, not a failure: the caller must pull,
// push or ask a human, and section 16 gives it its own exit code.
type GuardError struct{ Err error }

func (e *GuardError) Error() string { return e.Err.Error() }
func (e *GuardError) Unwrap() error { return e.Err }

func guardErr(err error) error {
	if err == nil {
		return nil
	}

	return &GuardError{Err: err}
}

// Apply renders, plans and installs the configs host by host.
func Apply(st *state.State, opts *Opts, out io.Writer) (Result, error) {
	result := Result{}

	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(out, "  ✗", e)
		}

		return result, fmt.Errorf("%d errors in the state", len(errs))
	}

	if err := Guards(st, opts); err != nil {
		return result, guardErr(err)
	}

	repo, err := gitsync.Open(st.Dir)
	if err != nil {
		return result, err
	}

	head, err := repo.Head()
	if err != nil {
		return result, err
	}

	branch, _ := repo.Branch()
	result.Head, result.Branch = head, branch

	at := opts.At
	if at.IsZero() {
		at = time.Now().UTC()
	}

	rendered, err := render.Check(st, at)
	if err != nil {
		return result, err
	}

	plans, err := plan.PlanWith(st, rendered, at, opts.Prober)
	if err != nil {
		return result, err
	}

	var names []string

	for _, p := range plans {
		if opts.Host != "" && p.Name != opts.Host {
			continue
		}

		names = append(names, p.Name)
	}

	if len(names) == 0 {
		return result, fmt.Errorf("nothing to apply: host not found")
	}

	if err := checkAncestors(repo, head, plans, names); err != nil {
		return result, guardErr(err)
	}

	order := Order(st, names)
	fmt.Fprintf(out, "plan for %s @ %s · order: %s\n", branch, head, strings.Join(order, " → "))

	done := 0

	fail := func(name string, changes int, err error) (Result, error) {
		result.Hosts = append(result.Hosts, HostResult{Name: name, Status: "failed", Changes: changes, Error: err.Error()})
		return result, err
	}
	for _, name := range order {
		var hp plan.HostPlan

		for _, p := range plans {
			if p.Name == name {
				hp = p
			}
		}

		h := st.Hosts[name]
		fmt.Fprintf(out, "▸ %s\n", name)

		if hp.Unreachable != "" {
			return fail(name, hp.Changes(), fmt.Errorf("%s: %s — series stopped", name, hp.Unreachable))
		}

		if !hp.Bootstrapped {
			fmt.Fprintln(out, "  – skipped: bootstrap not done")

			result.Hosts = append(result.Hosts, HostResult{Name: name, Status: "skipped"})

			continue
		}

		c, err := remote.Dial(st, name)
		if err != nil {
			return fail(name, hp.Changes(), err)
		}

		files := rendered[name]
		if hp.Changes() == 0 {
			if _, err := c.SH(fmt.Sprintf("mkdir -p /etc/vpnctl && echo %s > %s && %s", markerValue(head, opts), plan.Marker, hostscript.ManifestScript(files))); err != nil {
				_ = c.Close()
				return fail(name, 0, err)
			}

			fmt.Fprintf(out, "  – no changes, marker %s updated\n", head)

			_ = c.Close()
			done++

			result.Hosts = append(result.Hosts, HostResult{Name: name, Status: "unchanged", Marker: head})

			continue
		}

		if opts.AgentApply != nil {
			hr, err := opts.AgentApply(c, name, head, files)
			_ = c.Close()

			if err != nil {
				return fail(name, hp.Changes(), err)
			}

			hr.Name = name
			hr.Changes = hp.Changes()
			fmt.Fprintf(out, "  ✓ agent: %d files applied, probe %s\n", len(files), hr.Probe)
			result.Hosts = append(result.Hosts, hr)
			done++

			continue
		}

		notes, err := preflight(st, c, name, files, opts.Force)
		if err != nil {
			_ = c.Close()
			return fail(name, hp.Changes(), err)
		}

		for _, n := range notes {
			fmt.Fprintf(out, "  ! %s\n", n)
		}

		fmt.Fprintf(out, "  ✓ render · %d files, %d changes\n", len(files), hp.Changes())

		tarball, err := Tarball(files)
		if err != nil {
			_ = c.Close()
			return fail(name, hp.Changes(), err)
		}

		cmdOut, err := c.Run("rm -rf /tmp/.vpnctl && mkdir -p /tmp/.vpnctl && tar -xf - -C /tmp/.vpnctl", tarball)
		if err != nil {
			_ = c.Close()
			return fail(name, hp.Changes(), err)
		}

		if !cmdOut.OK() {
			_ = c.Close()
			return fail(name, hp.Changes(), fmt.Errorf("%s: copy failed: %s", name, strings.TrimSpace(string(cmdOut.Stderr))))
		}

		fmt.Fprintln(out, "  ✓ copied over the connection → /tmp/.vpnctl/")

		for _, f := range hp.Files {
			if f.Status == plan.Unchanged || f.Status == plan.Removed {
				continue
			}

			cmd, ok := hostscript.CheckCmd(h.Engine, f.Path)
			if !ok {
				continue
			}

			cmdOut, err := c.Run(cmd, nil)
			if err != nil {
				_ = c.Close()
				return fail(name, hp.Changes(), err)
			}

			if !cmdOut.OK() {
				_, _ = c.Run("rm -rf /tmp/.vpnctl", nil)
				_ = c.Close()

				return fail(name, hp.Changes(), fmt.Errorf("%s: check of %s failed:\n%s", name, f.Path, strings.TrimSpace(string(cmdOut.Stderr))))
			}

			fmt.Fprintf(out, "  ✓ check %s\n", f.Path)
		}

		backup := "/var/lib/vpnctl/backup/" + head

		cmdOut, err = c.Run(hostscript.InstallScript(hp, backup), nil)
		if err != nil {
			_ = c.Close()
			return fail(name, hp.Changes(), err)
		}

		if !cmdOut.OK() {
			fmt.Fprintln(out, strings.TrimSpace(string(cmdOut.Stderr)))
			fmt.Fprintln(out, "  ✗ swap failed, rolled back to the backup")

			_, _ = c.Run(hostscript.RollbackScript(hp, backup), nil)
			_ = c.Close()

			return fail(name, hp.Changes(), fmt.Errorf("%s: swap failed — series stopped", name))
		}

		fmt.Fprintln(out, "  ✓ atomic swap · services reloaded")

		cmdOut, err = c.Run(hostscript.ProbeScript(h.Role), nil)

		code := ""
		if err == nil {
			code = strings.TrimSpace(string(cmdOut.Stdout))
		}

		if code != "204" {
			if code == "" {
				code = "no answer"
			}

			fmt.Fprintf(out, "  ✗ probe: %s instead of 204 · rollback\n", code)

			if rb, err := c.Run(hostscript.RollbackScript(hp, backup), nil); err == nil && !rb.OK() {
				fmt.Fprintln(out, strings.TrimSpace(string(rb.Stderr)))
			}

			_ = c.Close()

			return fail(name, hp.Changes(), fmt.Errorf("%s: chain probe failed, config restored — series stopped", name))
		}

		via := "the exit"
		if h.Role == state.Entry {
			via = "the chain"
		}

		fmt.Fprintf(out, "  ✓ probe: 204 via %s\n", via)

		if _, err := c.SH(fmt.Sprintf("mkdir -p /etc/vpnctl && echo %s > %s && %s", markerValue(head, opts), plan.Marker, hostscript.ManifestScript(files))); err != nil {
			_ = c.Close()
			return fail(name, hp.Changes(), err)
		}

		fmt.Fprintf(out, "  ✓ marker %s written\n", head)

		_ = c.Close()
		done++

		result.Hosts = append(result.Hosts, HostResult{Name: name, Status: "applied", Changes: hp.Changes(), Probe: code, Marker: head})
	}

	if done > 0 && opts.Host == "" {
		tag := "applied/" + time.Now().UTC().Format("2006-01-02T15-04-05")
		if err := repo.Tag(tag); err != nil {
			return result, err
		}

		result.Tag = tag
		fmt.Fprintf(out, "done %d of %d · tag %s\n", done, len(order), tag)
	} else {
		fmt.Fprintf(out, "done %d of %d\n", done, len(order))
	}

	return result, nil
}

// markerValue is the applied commit, with a note when it is not on the remote.
func markerValue(head string, opts *Opts) string {
	if opts.Unpushed {
		return head + " unpushed"
	}

	return head
}
