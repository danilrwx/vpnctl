package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Tick reports the accounts whose day has come. It runs from a systemd timer,
// so it must be boring: read the file apply wrote, compare dates, say what
// changed. Nothing is rewritten yet — disabling an account means editing the
// engine config, which goes through the same check-and-rollback path as apply.
func Tick(expiryPath string, engineConfig string, now time.Time) ([]string, error) {
	body, err := os.ReadFile(expiryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	today := now.UTC().Format("2006-01-02")
	var expired []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] <= today {
			expired = append(expired, fields[0])
		}
	}
	sort.Strings(expired)
	return expired, nil
}

// TickReport is what the CLI prints and what a test can compare.
type TickReport struct {
	Expired []string `json:"expired"`
	Checked int      `json:"checked"`
}

// RunTick writes the report as JSON on stdout, the way the agent answers.
func RunTick(expiryPath string, now time.Time) error {
	expired, err := Tick(expiryPath, "", now)
	if err != nil {
		return err
	}
	rep := TickReport{Expired: expired, Checked: len(expired)}
	if rep.Expired == nil {
		rep.Expired = []string{}
	}
	b, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
