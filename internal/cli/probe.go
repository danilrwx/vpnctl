package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// ProbeMatrix asks every entry to measure the links that start there. The probe
// runs on the host, not here: the entry is the only machine on the near side of
// those edges, and a laptop cannot even reach an exit that sits behind a jump.
func ProbeMatrix(dir, host string, timeout time.Duration) error {
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	var entries []string
	for name, h := range st.Hosts {
		if h.Role != state.Entry {
			continue
		}
		if host != "" && host != name {
			continue
		}
		entries = append(entries, name)
	}
	if host != "" && len(entries) == 0 {
		return Usage(fmt.Errorf("host %q is not an entry: the matrix is measured from the entry", host))
	}
	sort.Strings(entries)
	key, err := agent.LoadKey(agent.KeyPath())
	if err != nil {
		return err
	}
	var rows []agent.ProbeResult
	down := 0
	for _, entry := range entries {
		c, err := remote.Dial(st, entry)
		if err != nil {
			return err
		}
		req, err := agent.NewRequest(key, "probe", entry, "", nil)
		if err != nil {
			_ = c.Close()
			return err
		}
		res, err := agent.Call(c, req)
		_ = c.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", entry, err)
		}
		for _, r := range res.Matrix {
			rows = append(rows, r)
			if !r.OK {
				down++
			}
		}
	}
	type row struct {
		Link     string `json:"link"`
		Protocol string `json:"protocol"`
		OK       bool   `json:"ok"`
		Detail   string `json:"detail,omitempty"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{Link: r.Link, Protocol: r.Protocol, OK: r.OK, Detail: r.Detail})
	}
	err = emit(struct {
		Rows []row `json:"rows"`
		Down int   `json:"down"`
	}{Rows: out, Down: down}, func() {
		if len(out) == 0 {
			fmt.Println("no links start at an entry: nothing to measure")
			return
		}
		for _, r := range out {
			mark := "✓"
			if !r.OK {
				mark = "✗"
			}
			fmt.Printf("  %s %-18s %-8s %s\n", mark, r.Link, r.Protocol, r.Detail)
		}
	})
	if err != nil {
		return err
	}
	if down > 0 {
		return Guard(fmt.Errorf("%d of %d cells do not handshake", down, len(out)))
	}
	return nil
}
