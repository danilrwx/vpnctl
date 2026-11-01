package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/config"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// Init creates a state directory: skeleton, age.yaml, first commit, and
// optionally pushes it. A remote with history needs --fresh: the old tip goes
// to a backup branch before main is overwritten.
func Init(dir, name, backupPass, remote string, fresh, nocommit bool) error {
	if _, err := os.Stat(dir); err == nil {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() != ".git" {
				return fmt.Errorf("%s is not empty", dir)
			}
		}
	}
	res, err := ops.Init(dir, ops.InitOptions{Name: name, BackupPassphrase: backupPass})
	if err != nil {
		return err
	}
	repo, err := gitsync.Init(dir)
	if err != nil {
		return err
	}
	if !nocommit {
		if err := repo.Commit(res.Message); err != nil {
			return err
		}
	}
	var pushed string
	if remote != "" {
		p, err := pushNewState(repo, remote, fresh)
		if err != nil {
			return err
		}
		pushed = p
	}
	if jsonOut {
		_ = emit(struct {
			Dir       string `json:"dir"`
			Recipient string `json:"recipient"`
			Backup    string `json:"backup,omitempty"`
		}{dir, res.Recipient, res.BackupBlob}, func() {})
		return nil
	}
	if pushed != "" {
		fmt.Printf("  pushed     %s\n", pushed)
	}
	fmt.Printf("✓ state in %s\n", dir)
	fmt.Printf("  recipient  %s\n", res.Recipient)
	if res.BackupBlob != "" {
		fmt.Println()
		fmt.Println("  backup identity — показать один раз, сохранить вне устройств:")
		fmt.Println()
		for _, line := range strings.Split(strings.TrimSpace(res.BackupBlob), "\n") {
			fmt.Println("  " + line)
		}
		fmt.Println()
	}
	return nil
}

// KeyInit writes age.yaml from the local identity.
func KeyInit(dir, name string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.KeyInit(st, name)
	if err != nil {
		return err
	}
	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

// KeyAdd adds a recipient and re-encrypts the secrets for everyone.
func KeyAdd(dir, name, key string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.KeyAdd(st, name, key)
	if err != nil {
		return err
	}
	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s · secrets re-encrypted for %s\n", msg, plural(len(mustKeys(st)), "recipient"))
	return nil
}

// KeyRemove drops a recipient and re-encrypts the secrets for the rest.
func KeyRemove(dir, name string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.KeyRemove(st, name)
	if err != nil {
		return err
	}
	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}
	fmt.Printf("✓ %s · secrets re-encrypted for %s\n", msg, plural(len(mustKeys(st)), "recipient"))
	return nil
}

// KeyList prints the recipients of secrets.age.yaml.
func KeyList(dir string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	keys, err := ops.Keys(st)
	if err != nil {
		return err
	}
	return emit(keys, func() {
		for _, k := range keys {
			mark := " "
			if k.This {
				mark = "*"
			}
			added := ""
			if k.Added != "" {
				added = " · added " + k.Added
				if days, err := addedDays(k.Added); err == nil && k.Name == "backup" && days > 180 {
					added += fmt.Sprintf(" · %d days old, rotate with `vpnctl key backup`", days)
				}
			}
			fmt.Printf("%s %-10s %s%s\n", mark, k.Name, k.Key, added)
		}
	})
}

func addedDays(day string) (int, error) {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return 0, err
	}
	return int(time.Since(t).Hours() / 24), nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func mustKeys(st *state.State) []ops.KeyInfo {
	k, _ := ops.Keys(st)
	return k
}

func commit(dir, msg string, nocommit bool) error {
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

// pushNewState publishes a freshly initialised state. A remote that already has
// refs is only overwritten on request, and its old tip is kept as a branch.
func pushNewState(repo *gitsync.Repo, url string, fresh bool) (string, error) {
	auth, err := gitsync.SSHAuth()
	if err != nil && !strings.HasPrefix(url, "/") {
		return "", err
	}
	refs, err := gitsync.ListRemote(url, auth)
	if err != nil {
		return "", err
	}
	if err := repo.AddRemote(url); err != nil {
		return "", err
	}
	if len(refs) == 0 {
		if err := repo.PushRefs(auth, false); err != nil {
			return "", err
		}
		return url, nil
	}
	if !fresh {
		return "", fmt.Errorf("the remote already has %d refs: clone it, or pass --fresh to overwrite (the old tip is kept as a backup branch)", len(refs))
	}
	if err := repo.FetchAuth(auth); err != nil {
		return "", err
	}
	backup := "backup/pre-init-" + time.Now().UTC().Format("20060102-150405")
	src := config.RefSpec("refs/remotes/origin/main:refs/heads/" + backup)
	if err := repo.PushRefs(auth, false, src); err != nil {
		return "", fmt.Errorf("backup branch %s: %w", backup, err)
	}
	if err := repo.PushRefs(auth, true, config.RefSpec("+refs/heads/main:refs/heads/main")); err != nil {
		return "", err
	}
	return url + " (old tip in " + backup + ")", nil
}

// KeyBackup issues a new offline backup identity and prints it once.
func KeyBackup(dir, passphrase string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	blob, msg, err := ops.KeyBackup(st, passphrase)
	if err != nil {
		return err
	}
	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}
	if jsonOut {
		return emit(struct {
			Backup string `json:"backup"`
			Msg    string `json:"message"`
		}{blob, msg}, func() {})
	}
	fmt.Printf("✓ %s\n\n", msg)
	fmt.Println("  backup identity — показать один раз, сохранить вне устройств:")
	fmt.Println()
	for _, line := range strings.Split(strings.TrimSpace(blob), "\n") {
		fmt.Println("  " + line)
	}
	fmt.Println()
	return nil
}
