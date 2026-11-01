package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
)

// Canonical returns the bytes every plain-YAML file of the state would have if
// the tool wrote it now. One rule everywhere: two spaces, nothing else. The
// ciphertext in secrets.age.yaml is not YAML anybody edits and is left alone.
func Canonical(st *State) (map[string][]byte, error) {
	out := map[string][]byte{}
	set := func(name string, v any) error {
		b, err := marshal(v)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", name, err)
		}
		out[name] = b
		return nil
	}
	if err := set("users.yaml", map[string]any{"users": st.Users}); err != nil {
		return nil, err
	}
	if err := set("hosts.yaml", map[string]any{"hosts": st.Hosts}); err != nil {
		return nil, err
	}
	if err := set("links.yaml", map[string]any{"links": st.Links}); err != nil {
		return nil, err
	}
	if err := set("protocols.yaml", map[string]any{"inbounds": st.Inbounds}); err != nil {
		return nil, err
	}
	if err := set("versions.yaml", st.Versions); err != nil {
		return nil, err
	}
	for host, r := range st.Routing {
		if err := set(filepath.Join("routing", host+".yaml"), r); err != nil {
			return nil, err
		}
	}
	for name, s := range st.Sets {
		if err := set(filepath.Join("sets", name+".yaml"), s); err != nil {
			return nil, err
		}
	}
	if st.Alerts.Ntfy != "" {
		m := map[string]any{"ntfy": st.Alerts.Ntfy}
		if st.Alerts.Serve != "" {
			m["serve"] = st.Alerts.Serve
		}
		if err := set("alerts.yaml", m); err != nil {
			return nil, err
		}
	}
	recs, err := ageio.LoadRecipients(st.Dir)
	if err != nil {
		return nil, err
	}
	if recs != nil {
		b, err := ageio.MarshalRecipients(recs)
		if err != nil {
			return nil, err
		}
		out[ageio.RecipientsFile] = b
	}
	return out, nil
}

// Differences lists the owned files whose bytes are not the canonical ones: a
// hand edit, an old tool, or a merge that kept four spaces. A file that is
// missing is not a difference — fmt does not invent files the state lacks.
func Differences(st *State) ([]string, error) {
	want, err := Canonical(st)
	if err != nil {
		return nil, err
	}
	var diff []string
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(st.Dir, filepath.FromSlash(name)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !bytes.Equal(got, body) {
			diff = append(diff, name)
		}
	}
	sort.Strings(diff)
	return diff, nil
}

// Format rewrites every owned file in its canonical form and returns the names
// it touched.
func Format(st *State) ([]string, error) {
	want, err := Canonical(st)
	if err != nil {
		return nil, err
	}
	diff, err := Differences(st)
	if err != nil {
		return nil, err
	}
	for _, name := range diff {
		path := filepath.Join(st.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, want[name], 0o644); err != nil {
			return nil, err
		}
	}
	return diff, nil
}
