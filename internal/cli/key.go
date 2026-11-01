package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"golang.org/x/term"

	"github.com/go-git/go-git/v5/config"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// Init creates a state directory: skeleton, age.yaml, first commit, and
// optionally pushes it. A remote with history needs --fresh: the old tip goes
// to a backup branch before main is overwritten.
func Init(dir, name, backupPass, remote, backupOut string, fresh, nocommit bool) error {
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
		if backupOut != "" {
			if err := os.WriteFile(backupOut, []byte(res.BackupBlob), 0o600); err != nil {
				return err
			}
			sum := sha256.Sum256([]byte(res.BackupBlob))
			fmt.Printf("  backup     %s (sha256 %s)\n  keep it off this machine\n", backupOut, hex.EncodeToString(sum[:]))
		} else {
			fmt.Println()
			fmt.Println("  backup identity — показать один раз, сохранить вне устройств:")
			fmt.Println()
			for _, line := range strings.Split(strings.TrimSpace(res.BackupBlob), "\n") {
				fmt.Println("  " + line)
			}
			fmt.Println()
		}
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

// KeyBackup issues a new offline backup identity: written to --out, or printed
// once when there is no file to write it to.
func KeyBackup(dir, flagPass, out string, nocommit bool) error {
	pass, err := passphrase(flagPass, true)
	if err != nil {
		return err
	}
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	blob, msg, err := ops.KeyBackup(st, pass)
	if err != nil {
		return err
	}
	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(blob), 0o600); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(blob))
		line := fmt.Sprintf("✓ %s\n  saved %s\n  sha256 %s\n  keep it off this machine", msg, out, hex.EncodeToString(sum[:]))
		if jsonOut {
			return emit(struct {
				File string `json:"file"`
				SHA  string `json:"sha256"`
				Msg  string `json:"message"`
			}{out, hex.EncodeToString(sum[:]), msg}, func() {})
		}
		fmt.Println(line)
		return nil
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

// passphrase resolves the backup passphrase: the flag, then the environment,
// then the terminal. It never echoes and it asks twice when confirming.
func passphrase(flagValue string, confirm bool) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("VPNCTL_BACKUP_PASS"); v != "" {
		return v, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no passphrase: pass --pass, set VPNCTL_BACKUP_PASS, or run in a terminal")
	}
	fmt.Fprint(os.Stderr, "passphrase: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if !confirm {
		return string(first), nil
	}
	fmt.Fprint(os.Stderr, "repeat passphrase: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("the passphrases differ")
	}
	return string(first), nil
}

// KeyRestore writes an identity from a sealed backup file into the key path and
// proves it can read the state.
func KeyRestore(dir, file, keyPath string) error {
	if file == "" {
		return errors.New("the backup file is required")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	pass, err := passphrase("", false)
	if err != nil {
		return err
	}
	id, err := ageio.OpenIdentity(string(body), pass)
	if err != nil {
		return err
	}
	if keyPath == "" {
		keyPath = state.KeyPath()
	}
	if _, err := os.Stat(keyPath); err == nil {
		backup := keyPath + ".before-restore"
		if err := os.Rename(keyPath, backup); err != nil {
			return fmt.Errorf("move the existing key aside: %w", err)
		}
		fmt.Printf("  existing key moved to %s\n", backup)
	}
	if err := ageio.WriteIdentity(keyPath, id); err != nil {
		return err
	}
	fmt.Printf("✓ identity restored into %s\n", keyPath)

	// the point of the restore is reading the secrets: prove it when we can
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "secrets.age.yaml")); err == nil {
			if _, err := ageio.Decrypt(filepath.Join(dir, "secrets.age.yaml"), keyPath); err != nil {
				return fmt.Errorf("the restored key cannot read secrets.age.yaml: %w", err)
			}
			fmt.Printf("✓ secrets.age.yaml decrypts with the restored key\n")
		}
	}
	fmt.Printf("\nnext: vpnctl key add <this-device> %s\n      vpnctl key backup --out <new-file>\n", ageio.RecipientOf(id))
	return nil
}
