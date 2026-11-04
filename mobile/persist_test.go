package mobile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSecretWriteFailureRestoresStateAndFiles(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, operation := range []string{"add", "edit", "inbound"} {
			t.Run(operation+"/existing="+map[bool]string{false: "no", true: "yes"}[exists], func(t *testing.T) {
				a := openFixture(t)
				if exists {
					if err := a.save("fixture"); err != nil {
						t.Fatal(err)
					}
				}

				for _, name := range []string{"users.yaml", "protocols.yaml"} {
					if err := os.Chmod(filepath.Join(a.dir, name), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				before, err := json.Marshal(a.st)
				if err != nil {
					t.Fatal(err)
				}

				disk := diskFiles(t, a.dir)
				t.Setenv("VPNCTL_AGE_KEY", filepath.Join(t.TempDir(), "missing-age-key"))

				switch operation {
				case "add":
					_, err = a.AddUser("failure", "split", "hy2,awg", false, "")
				case "edit":
					_, err = a.EditUser("brat", "full", "hy2,awg", false, "")
				case "inbound":
					_, err = a.SaveInbound("ru-msk-1", "hy2", 8443, "", "")
				}

				if err == nil || !strings.Contains(err.Error(), "read age key") {
					t.Fatalf("expected key failure, got %v", err)
				}

				after, err := json.Marshal(a.st)
				if err != nil {
					t.Fatal(err)
				}

				if string(before) != string(after) {
					t.Fatal("memory changed")
				}

				if !reflect.DeepEqual(disk, diskFiles(t, a.dir)) {
					t.Fatal("disk contents/existence changed")
				}

				for _, name := range []string{"users.yaml", "protocols.yaml"} {
					info, err := os.Stat(filepath.Join(a.dir, name))
					if err != nil {
						t.Fatal(err)
					}

					if info.Mode().Perm() != 0o600 {
						t.Fatalf("%s mode changed: %v", name, info.Mode())
					}
				}
			})
		}
	}
}

func TestPersistFilesRestoresAfterLateFailure(t *testing.T) {
	a := &App{dir: t.TempDir()}
	old := filepath.Join(a.dir, "old")
	added := filepath.Join(a.dir, "added")

	if err := os.WriteFile(old, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("commit failed")

	err := a.persistFiles([]string{"old", "added"}, func() error {
		if err := os.WriteFile(old, []byte("changed"), 0o644); err != nil {
			return err
		}

		if err := os.Chmod(old, 0o644); err != nil {
			return err
		}

		if err := os.WriteFile(added, []byte("created"), 0o600); err != nil {
			return err
		}

		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}

	body, err := os.ReadFile(old)
	if err != nil || string(body) != "original" {
		t.Fatalf("restore: %q %v", body, err)
	}

	info, err := os.Stat(old)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}

	if _, err := os.Stat(added); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created file retained: %v", err)
	}
}
