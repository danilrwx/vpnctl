package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/state"
)

// Init creates a state directory: skeleton, age.yaml, first commit.
func Init(dir, name, backupPass string, nocommit bool) error {
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
	for _, k := range keys {
		mark := " "
		if k.This {
			mark = "*"
		}
		fmt.Printf("%s %-10s %s\n", mark, k.Name, k.Key)
	}
	return nil
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
