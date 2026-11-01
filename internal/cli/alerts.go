package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/term"
)

// AlertsSet records the notification topic and its token. The token never comes
// from a flag: argv is visible in ps and stays in the shell history.
func AlertsSet(dir, ntfy, serve, topic, tokenFile string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	token, err := alertsToken(tokenFile)
	if err != nil {
		return err
	}
	before := st.Secrets.Alerts.Token
	var msg string
	switch {
	case serve != "":
		msg, err = ops.AlertsServe(st, serve, topic, token)
	case ntfy != "":
		if topic != "" {
			return errors.New("--topic belongs to --serve: pass the whole path in --ntfy instead")
		}
		msg, err = ops.AlertsSet(st, ntfy, token)
	default:
		return errors.New("either --ntfy <url> or --serve <host> --topic <name> is required")
	}
	if err != nil {
		return err
	}
	if err := saveAlerts(st, dir, msg, st.Secrets.Alerts.Token != before, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

// AlertsOff clears the topic and the token: the probe stops notifying.
func AlertsOff(dir string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	before := st.Secrets.Alerts.Token
	msg, err := ops.AlertsSet(st, "", "")
	if err != nil {
		return err
	}
	if err := saveAlerts(st, dir, msg, st.Secrets.Alerts.Token != before, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

// AlertsShow prints the topic, whether a token is set and which hosts alert.
func AlertsShow(dir string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	var entries []string
	for name := range st.Hosts {
		if st.Hosts[name].Role == state.Entry {
			entries = append(entries, name)
		}
	}
	sort.Strings(entries)
	type row struct {
		Ntfy     string   `json:"ntfy"`
		Serve    string   `json:"serve,omitempty"`
		Local    string   `json:"local,omitempty"`
		TokenSet bool     `json:"token_set"`
		Entries  []string `json:"entries"`
	}
	r := row{Ntfy: st.Alerts.Ntfy, Serve: st.Alerts.Serve, TokenSet: st.Secrets.Alerts.Token != "", Entries: entries}
	if st.Alerts.Serve != "" {
		r.Local = render.NtfyLocal(st.Alerts.Ntfy)
	}
	if r.Entries == nil {
		r.Entries = []string{}
	}
	return emit(r, func() {
		if r.Ntfy == "" {
			fmt.Println("alerts: off")
			return
		}
		token := "no token"
		if r.TokenSet {
			token = "token set"
		}
		fmt.Printf("alerts: %s (%s) from %s\n", r.Ntfy, token, strings.Join(entries, ", "))
		if r.Serve != "" {
			fmt.Printf("  served on %s, the probe posts to %s\n", r.Serve, r.Local)
		}
	})
}

// AlertsTest sends one message now: the topic and the token are only useful if
// a phone actually shows them. A self-hosted server is checked twice, over the
// public name and over loopback, because the probe uses the second one.
func AlertsTest(dir string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if st.Alerts.Ntfy == "" {
		return errors.New("no topic: run alerts set --ntfy <url> or --serve <host> --topic <name> first")
	}
	type row struct {
		Ntfy   string `json:"ntfy"`
		Local  string `json:"local,omitempty"`
		Status int    `json:"status"`
	}
	r := row{Ntfy: st.Alerts.Ntfy}
	status, err := postAlert(st.Alerts.Ntfy, st.Secrets.Alerts.Token, "vpnctl test: the topic works", "default")
	if err != nil {
		return err
	}
	r.Status = status
	if st.Alerts.Serve != "" {
		// the loopback address only exists on the host itself, so it is printed
		// and not dialled from here: apply's probe already proves that path
		r.Local = render.NtfyLocal(st.Alerts.Ntfy)
	}
	return emit(r, func() {
		fmt.Printf("✓ %s answered %d\n", r.Ntfy, r.Status)
		if r.Local != "" {
			fmt.Printf("  the host's probe posts to %s\n", r.Local)
		}
	})
}

// postAlert sends one notification and returns the HTTP status.
func postAlert(topic, token, text, priority string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, topic, strings.NewReader(text))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Title", "vpnctl")
	req.Header.Set("Priority", priority)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("the topic did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("the topic answered %s — check the topic and the token", resp.Status)
	}
	return resp.StatusCode, nil
}

// saveAlerts writes alerts.yaml and, only when the token really changed, the
// re-sealed secrets: an alerts run must not rewrite users.yaml or churn the
// ciphertext of a token that stayed the same.
func saveAlerts(st *state.State, dir, msg string, tokenChanged, nocommit bool) error {
	if err := checkState(st); err != nil {
		return err
	}
	if err := state.SaveAlerts(dir, st.Alerts); err != nil {
		return err
	}
	if tokenChanged {
		if err := state.SaveSecrets(dir, st.Secrets); err != nil {
			return err
		}
	}
	if nocommit {
		fmt.Println("(not committed)")
		return nil
	}
	repo, err := gitsync.Open(dir)
	if err != nil {
		return err
	}
	return repo.Commit(msg)
}

// alertsToken reads the ntfy token: from a file, from the environment, from a
// pipe, or from a hidden terminal prompt. An empty answer means a public topic.
func alertsToken(file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read the token file: %w", err)
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
		return "", fmt.Errorf("the token file %s is empty", file)
	}
	if v := os.Getenv("VPNCTL_ALERTS_TOKEN"); v != "" {
		return strings.TrimSpace(v), nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
	fmt.Fprint(os.Stderr, "ntfy token, empty for a public topic: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
