package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// openFixtureDir copies testdata/basic into a temp dir: the tests that write
// must never touch the shared fixture.
func openFixtureDir(t *testing.T) string {
	t.Helper()

	src := filepath.Join("..", "..", "testdata", "basic")
	dst := t.TempDir()

	for _, sub := range []string{"", "routing", "sets", "templates"} {
		from := filepath.Join(src, sub)
		to := filepath.Join(dst, sub)

		entries, err := os.ReadDir(from)
		if err != nil {
			continue
		}

		if err := os.MkdirAll(to, 0o755); err != nil {
			t.Fatal(err)
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}

			b, err := os.ReadFile(filepath.Join(from, e.Name()))
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	return dst
}
