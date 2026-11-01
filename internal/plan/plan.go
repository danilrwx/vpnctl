// Package plan compares the render with the hosts: pending changes, drift, reachability.
package plan

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

const (
	// Marker is the commit the host runs.
	Marker = "/etc/vpnctl/applied-commit"
	// Bootstrap is the marker of a finished bootstrap.
	Bootstrap = "/etc/vpnctl/bootstrap"
	// Manifest holds the hash of every file the last apply wrote: drift is a
	// difference against it, not against a re-render of an old commit.
	Manifest = "/etc/vpnctl/applied.sha256"
)

// Status is the file state against the applied commit and the host.
type Status int

const (
	Unchanged Status = iota
	Changed
	New
	// Drift: the host has neither the applied nor the upcoming content.
	Drift
	Removed
)

// Mark is the one-letter status used by the report.
func (s Status) Mark() string {
	switch s {
	case Changed:
		return "~"
	case New:
		return "+"
	case Drift:
		return "!"
	case Removed:
		return "-"
	}
	return "="
}

// HostPlan is the plan of one host.
type HostPlan struct {
	Name         string
	Unreachable  string
	Bootstrapped bool
	Marker       string
	// Unpushed marks a host applied from a commit that is not on the remote.
	Unpushed bool
	Files    []FileStatus
	Notes    []string
}

// FileStatus is one path with its status.
type FileStatus struct {
	Path   string
	Status Status
}

// Changes is the number of files that are not unchanged.
func (p HostPlan) Changes() int {
	n := 0
	for _, f := range p.Files {
		if f.Status != Unchanged {
			n++
		}
	}
	return n
}

// Classify is the file status from three hashes: previously applied, upcoming, on the host.
func Classify(old, new, host string, hasOld, hasNew, hasHost bool) Status {
	switch {
	case hasNew && hasHost && new == host:
		return Unchanged
	case !hasNew && hasHost:
		return Removed
	case hasNew && !hasHost:
		return New
	case hasOld && hasNew && hasHost && old == host:
		return Changed
	case !hasOld && hasNew && hasHost:
		return Changed // first apply: no history yet
	case hasNew && hasHost:
		return Drift
	case !hasNew && !hasHost:
		return Unchanged
	}
	return Drift
}

// ProbeScript asks the host for its marker, bootstrap flag and the hashes of the paths.
func ProbeScript(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = "'" + p + "'"
	}
	return fmt.Sprintf(
		"cat %s 2>/dev/null || echo -; test -e %s && echo yes || echo no; if [ -s %s ]; then base64 -w0 %s; else echo -; fi; for f in %s; do if [ -e \"$f\" ]; then printf '%%s %%s\\n' \"$(sha256sum \"$f\" | cut -d' ' -f1)\" \"$f\"; else printf 'missing %%s\\n' \"$f\"; fi; done",
		Marker, Bootstrap, Manifest, Manifest, strings.Join(quoted, " "))
}

// Probe is the parsed answer of one host.
type Probe struct {
	Marker       string
	HasMarker    bool
	Unpushed     bool
	Bootstrapped bool
	// Manifest is what the last apply wrote: path to hash.
	Manifest map[string]string
	Hashes   map[string]string
	Missing  map[string]bool
}

// ParseProbe reads the marker line, the bootstrap flag and the hashes.
func ParseProbe(out string) Probe {
	p := Probe{Hashes: map[string]string{}, Missing: map[string]bool{}}
	lines := strings.Split(out, "\n")
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) > 0 && fields[0] != "-" {
			p.Marker, p.HasMarker = fields[0], true
			p.Unpushed = len(fields) > 1 && fields[1] == "unpushed"
		}
	}
	if len(lines) > 1 && lines[1] == "yes" {
		p.Bootstrapped = true
	}
	if len(lines) > 2 && lines[2] != "-" && lines[2] != "" {
		if raw, err := base64.StdEncoding.DecodeString(lines[2]); err == nil {
			p.Manifest = map[string]string{}
			for _, l := range strings.Split(string(raw), "\n") {
				if i := strings.IndexByte(l, ' '); i > 0 {
					p.Manifest[l[i+1:]] = l[:i]
				}
			}
		}
	}
	for _, l := range lines[3:] {
		i := strings.IndexByte(l, ' ')
		if i < 0 {
			continue
		}
		h, path := l[:i], l[i+1:]
		if h == "missing" {
			p.Missing[path] = true
			continue
		}
		p.Hashes[path] = h
	}
	return p
}

// ErrPreMigration is returned when the commit still holds the sops secrets file.
var ErrPreMigration = errors.New("secrets at that commit are still in the sops file: render is impossible without the old core")

// RenderAt is the render of the state at the marker commit, for drift.
func RenderAt(st *state.State, commit, host string) (render.HostFiles, error) {
	repo, err := gitsync.Open(st.Dir)
	if err != nil {
		return nil, err
	}
	files, err := repo.FilesAt(commit)
	if err != nil {
		return nil, err
	}
	if _, ok := files["secrets.age.yaml"]; !ok {
		if _, sops := files["secrets.sops.yaml"]; sops {
			return nil, ErrPreMigration
		}
	}
	tmp, err := os.MkdirTemp("", "vpnctl-at-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for name, body := range files {
		p := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			return nil, err
		}
	}
	old, err := state.Load(tmp)
	if err != nil {
		return nil, err
	}
	return render.RenderHost(old, host)
}

func hashMap(f render.HostFiles) map[string]string {
	out := make(map[string]string, len(f))
	for p, b := range f {
		out[p] = render.Sha256(b)
	}
	return out
}

// Prober answers the probe for one host: the shell script by default, the agent
// when the CLI passes one.
type Prober func(c *remote.Client, host string, paths []string) (Probe, error)

// Plan queries the hosts in parallel with the shell probe.
func Plan(st *state.State, rendered render.Rendered) ([]HostPlan, error) {
	return PlanWith(st, rendered, nil)
}

// PlanWith queries the hosts in parallel and builds the plan, in host name order.
func PlanWith(st *state.State, rendered render.Rendered, prober Prober) ([]HostPlan, error) {
	names := make([]string, 0, len(rendered))
	for name := range rendered {
		names = append(names, name)
	}
	sort.Strings(names)

	type answer struct {
		probe Probe
		err   error
	}
	answers := make(map[string]answer, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a := answer{}
			defer func() {
				mu.Lock()
				answers[name] = a
				mu.Unlock()
			}()
			files := rendered[name]
			c, err := remote.Dial(st, name)
			if err != nil {
				a.err = err
				return
			}
			defer func() { _ = c.Close() }()
			if err := c.Reachable(); err != nil {
				a.err = err
				return
			}
			paths := make([]string, 0, len(files))
			for p := range files {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			var p Probe
			if prober != nil {
				p, err = prober(c, name, paths)
			} else {
				var out string
				out, err = c.SH(ProbeScript(paths))
				if err == nil {
					p = ParseProbe(out)
				}
			}
			if err != nil {
				a.err = err
				return
			}
			// plus old paths when the marker is known: they need hashing too
			if p.HasMarker {
				if old, err := RenderAt(st, p.Marker, name); err == nil {
					var extra []string
					for path := range old {
						if _, ok := files[path]; !ok {
							extra = append(extra, path)
						}
					}
					if len(extra) > 0 {
						sort.Strings(extra)
						var p2 Probe
						if prober != nil {
							p2, err = prober(c, name, extra)
						} else {
							var out2 string
							out2, err = c.SH(ProbeScript(extra))
							if err == nil {
								p2 = ParseProbe(out2)
							}
						}
						if err != nil {
							a.err = err
							return
						}
						for k, v := range p2.Hashes {
							p.Hashes[k] = v
						}
						for k, v := range p2.Missing {
							p.Missing[k] = v
						}
					}
				}
			}
			a.probe = p
		}(name)
	}
	wg.Wait()

	plans := make([]HostPlan, 0, len(names))
	for _, name := range names {
		hp := HostPlan{Name: name}
		a := answers[name]
		next := hashMap(rendered[name])
		if a.err != nil {
			hp.Unreachable = a.err.Error()
			paths := make([]string, 0, len(next))
			for p := range next {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				hp.Files = append(hp.Files, FileStatus{Path: p, Status: Changed})
			}
			plans = append(plans, hp)
			continue
		}
		hp.Bootstrapped = a.probe.Bootstrapped
		hp.Marker = a.probe.Marker
		hp.Unpushed = a.probe.Unpushed
		var old map[string]string
		if len(a.probe.Manifest) > 0 {
			old = a.probe.Manifest
		} else if a.probe.HasMarker {
			if f, err := RenderAt(st, a.probe.Marker, name); err == nil {
				old = hashMap(f)
			} else if errors.Is(err, ErrPreMigration) {
				hp.Notes = append(hp.Notes, fmt.Sprintf(
					"applied commit %s predates the age migration: drift cannot be determined, the next apply fixes the marker",
					a.probe.Marker))
			} else {
				hp.Notes = append(hp.Notes, fmt.Sprintf(
					"applied commit %s cannot be rendered (%v): drift cannot be determined", a.probe.Marker, err))
			}
		}
		paths := make([]string, 0, len(next))
		for p := range next {
			paths = append(paths, p)
		}
		for p := range old {
			if _, ok := next[p]; !ok {
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		for _, path := range paths {
			o, hasO := old[path]
			n, hasN := next[path]
			h, hasH := a.probe.Hashes[path]
			hp.Files = append(hp.Files, FileStatus{Path: path, Status: Classify(o, n, h, hasO, hasN, hasH)})
		}
		if a.probe.Unpushed {
			hp.Notes = append(hp.Notes, "applied from an unpushed commit: push it so the other devices can render this marker")
		}
		if !a.probe.Bootstrapped {
			hp.Notes = append(hp.Notes, "bootstrap not done: the host will be skipped")
		}
		plans = append(plans, hp)
	}
	// reachability through the jump: recomputed once the jump is known
	down := map[string]bool{}
	for _, p := range plans {
		if p.Unreachable != "" {
			down[p.Name] = true
		}
	}
	for i := range plans {
		if plans[i].Unreachable == "" {
			continue
		}
		if via := st.Hosts[plans[i].Name].Via; via != "" && down[via] {
			plans[i].Unreachable = "unreachable via " + via
		}
	}
	return plans, nil
}

// Print writes the report.
func Print(plans []HostPlan) {
	for _, p := range plans {
		if p.Unreachable != "" {
			fmt.Printf("%s · ✗ %s\n", p.Name, p.Unreachable)
			continue
		}
		applied := "not applied yet"
		if p.Marker != "" {
			applied = "applied " + p.Marker
		}
		fmt.Printf("%s · %s · %d changes\n", p.Name, applied, p.Changes())
		for _, f := range p.Files {
			if f.Status == Unchanged {
				continue
			}
			note := ""
			if f.Status == Drift {
				note = "   drift: edited on the host, will be overwritten (--adopt to take it into the repo)"
			}
			fmt.Printf("  %s %s%s\n", f.Status.Mark(), f.Path, note)
		}
		for _, n := range p.Notes {
			fmt.Printf("  ! %s\n", n)
		}
	}
}

// Fetch reads a file from the host.
func Fetch(st *state.State, host, path string) (string, error) {
	c, err := remote.Dial(st, host)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	return c.SH("cat '" + path + "'")
}

// Diff is the unified diff between the render and the file on the host.
func Diff(st *state.State, rendered render.Rendered, host, path string) (string, error) {
	next, ok := rendered[host][path]
	if !ok {
		return "", fmt.Errorf("%s: file %s is not rendered", host, path)
	}
	cur, err := Fetch(st, host, path)
	if err != nil {
		return "", err
	}
	return unifiedDiff(host+":"+path, "render", cur, next), nil
}

// Adopt moves a file from the host into the state's overrides/<host>/<path>: the render picks it up.
func Adopt(st *state.State, host, path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("the path must be absolute")
	}
	cur, err := Fetch(st, host, path)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(st.Dir, "overrides", host, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(cur), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("overrides(%s): %s from the host", host, path), nil
}
