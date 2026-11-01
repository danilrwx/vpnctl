package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// UserAdd creates a user with fresh secrets, writes the state and commits it.
func UserAdd(dir, name string, modes, protocols []string, tg bool, entry string, nocommit bool) error {
	if name == "" {
		return fmt.Errorf("the user name is required")
	}
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if _, ok := st.Users[name]; ok {
		return fmt.Errorf("user %q already exists", name)
	}
	msg, err := ops.UserAdd(st, name, modes, protocols, tg)
	if err != nil {
		return err
	}
	if entry != "" {
		u := st.Users[name]
		u.Entry = entry
		st.Users[name] = u
	}
	if err := save(st, dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s: %s (%s)\n", msg, strings.Join(modes, ","), strings.Join(protocols, ","))
	return nil
}

// UserRm removes a user and its secrets.
func UserRm(dir, name string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.UserRm(st, name)
	if err != nil {
		return err
	}
	if err := save(st, dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

// UserRotate gives a user fresh credentials and commits.
func UserRotate(dir, name string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.UserRotate(st, name)
	if err != nil {
		return err
	}
	if err := save(st, dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

// UserList prints every user with its modes, protocols and flags.
func UserList(dir string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	type row struct {
		Name      string   `json:"name"`
		Enabled   bool     `json:"enabled"`
		Modes     []string `json:"modes"`
		Protocols []string `json:"protocols"`
		TG        bool     `json:"tg"`
		AWG       bool     `json:"awg"`
	}
	rows := []row{}
	for _, n := range ops.SortedUsers(st) {
		u := st.Users[n]
		rows = append(rows, row{Name: n, Enabled: u.IsEnabled(), Modes: u.Modes, Protocols: u.Protocols, TG: u.TG, AWG: u.AWG != nil})
	}
	return emit(rows, func() {
		for _, r := range rows {
			status := "on"
			if !r.Enabled {
				status = "off"
			}
			fmt.Printf("%-14s %-4s modes=%-12s protos=%-22s tg=%v awg=%v\n",
				r.Name, status, strings.Join(r.Modes, ","), strings.Join(r.Protocols, ","), r.TG, r.AWG)
		}
	})
}

// UserLink prints the subscription URL, the direct links and the Telegram proxy.
func UserLink(dir, name string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	entry, err := st.Entry()
	if err != nil {
		return err
	}
	u, ok := st.Users[name]
	if !ok {
		return fmt.Errorf("user %q is not described", name)
	}
	mode := "split"
	if len(u.Modes) > 0 {
		mode = u.Modes[0]
	}
	fmt.Printf("domain: %s\n", st.Hosts[entry].Domain)
	sub, err := render.SubURL(st, entry, name, mode)
	if err != nil {
		return err
	}
	fmt.Printf("sub: %s\n", sub)
	links, err := render.Links(st, entry, name, mode)
	if err != nil {
		return err
	}
	for _, l := range links {
		fmt.Println(l)
	}
	if tg, ok := render.TGLink(st, entry); ok {
		fmt.Printf("tg:  %s\n", tg)
	}
	return nil
}

// MessageOpts is what the message command asks for.
type MessageOpts struct {
	Dir     string
	Name    string
	Format  string
	Mode    string
	Lang    string
	IP      bool
	NoLinks bool
	NoTG    bool
}

// UserMessage prints the ready-to-forward message of a user.
func UserMessage(o MessageOpts) error {
	st, err := state.Load(o.Dir)
	if err != nil {
		return err
	}
	markdown := false
	switch o.Format {
	case "", "text":
	case "markdown", "md":
		markdown = true
	default:
		return fmt.Errorf("unknown format %q: expected text or markdown", o.Format)
	}
	lang := o.Lang
	if lang == "" {
		lang = SystemLang()
	}
	msg, err := render.Message(st, o.Name, render.MessageOptions{
		Lang:     lang,
		Markdown: markdown,
		IP:       o.IP,
		NoLinks:  o.NoLinks,
		NoTG:     o.NoTG,
		Mode:     o.Mode,
	})
	if err != nil {
		return err
	}
	fmt.Print(msg)
	return nil
}

// SystemLang is the language of the environment: ru_RU.UTF-8 gives ru.
func SystemLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return render.NormalizeLang(v)
		}
	}
	return "en"
}

// UserConfig writes or prints the client profile of a user: the same bytes the
// entry serves on the subscription endpoint.
func UserConfig(dir, name, mode, format, out string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	entry, err := st.Entry()
	if err != nil {
		return err
	}
	u, ok := st.Users[name]
	if !ok {
		return fmt.Errorf("user %q is not described", name)
	}
	modes := u.Modes
	if mode != "" {
		if !containsStr(u.Modes, mode) {
			return fmt.Errorf("user %q has no mode %q", name, mode)
		}
		modes = []string{mode}
	}
	ext := map[string]string{"singbox": "json", "clash": "yaml", "sub": "txt", "links": "txt"}[format]
	if ext == "" {
		return fmt.Errorf("unknown format %q: expected singbox, clash, sub or links", format)
	}
	multi := len(modes) > 1
	for _, m := range modes {
		body, err := profile(st, entry, name, u, m, format)
		if err != nil {
			return err
		}
		switch {
		case out == "" && multi:
			fmt.Printf("=== %s %s ===\n%s\n", name, m, body)
		case out == "":
			fmt.Print(body)
		case isDir(out):
			p := fmt.Sprintf("%s/%s-%s.%s", strings.TrimSuffix(out, "/"), name, m, ext)
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				return err
			}
			fmt.Println(p)
		default:
			if err := os.WriteFile(out, []byte(body), 0o600); err != nil {
				return err
			}
			fmt.Println(out)
		}
	}
	return nil
}

func profile(st *state.State, entry, name string, u state.User, mode, format string) (string, error) {
	switch format {
	case "singbox":
		return render.SingboxJSON(st, entry, name, u, mode)
	case "clash":
		return render.ClashYAML(st, entry, name, u, mode)
	case "sub":
		links, err := render.Links(st, entry, name, mode)
		if err != nil {
			return "", err
		}
		return render.Base64List(links), nil
	}
	links, err := render.Links(st, entry, name, mode)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, l := range links {
		b.WriteString(l + "\n")
	}
	return b.String(), nil
}

func isDir(p string) bool {
	if strings.HasSuffix(p, "/") {
		return true
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// save validates first and only then writes: a rejected change must not leave
// a dirty worktree behind.
func save(st *state.State, dir, msg string, nocommit bool) error {
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	if err := state.SaveUsers(dir, st.Users); err != nil {
		return err
	}
	if err := state.SaveSecrets(dir, st.Secrets); err != nil {
		return err
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

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
