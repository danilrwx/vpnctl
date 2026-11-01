package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// Sample is one line of /var/lib/vpnctl/metrics/<month>.jsonl.
type Sample struct {
	TS        int64   `json:"ts"`
	Load1     float64 `json:"load1"`
	MemUsedMB int     `json:"mem_used_mb"`
	MemTotal  int     `json:"mem_total_mb"`
	DiskPct   int     `json:"disk_pct"`
	UptimeS   int64   `json:"uptime_s"`
	Conns     int     `json:"conns"`
	Reboot    int     `json:"reboot"`
}

// Metrics reads the samples collected on a host, newest last.
func Metrics(dir, host string, last int) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if _, err := st.Host(host); err != nil {
		return err
	}
	c, err := remote.Dial(st, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	out, err := c.SH(fmt.Sprintf("cat /var/lib/vpnctl/metrics/*.jsonl 2>/dev/null | tail -n %d", last))
	if err != nil {
		return err
	}
	var samples []Sample
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var s Sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		samples = append(samples, s)
	}
	return emit(samples, func() {
		if len(samples) == 0 {
			fmt.Println("no samples yet: the metrics timer writes one every 5 minutes")
			return
		}
		fmt.Printf("%-20s %6s %12s %6s %5s %6s %s\n", "time", "load", "memory", "disk", "conns", "up", "reboot")
		for _, s := range samples {
			fmt.Printf("%-20s %6.2f %5d/%d MB %5d%% %5d %5ds %v\n", timeOf(s.TS), s.Load1,
				s.MemUsedMB, s.MemTotal, s.DiskPct, s.Conns, s.UptimeS, s.Reboot == 1)
		}
	})
}

func timeOf(ts int64) string {
	return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04")
}
