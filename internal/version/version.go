// Package version knows which revision a binary was built from. CI and a laptop
// build the same way, so the answer does not depend on anybody remembering to
// pass a flag: Go records the revision of the checkout in the binary.
package version

import (
	"runtime/debug"
	"strings"
)

// String is the short revision, with "-dirty" when the worktree had changes.
// "dev" means the binary was built outside a checkout (go run of a module, a
// release tarball without build info): comparing it means nothing.
func String() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev, dirty, when string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value
		case "vcs.time":
			when = s.Value
		}
	}
	if rev == "" {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	out := rev
	if dirty == "true" {
		out += "-dirty"
	}
	if when != "" {
		if i := strings.IndexByte(when, 'T'); i > 0 {
			when = when[:i]
		}
		out += " (" + when + ")"
	}
	return out
}

// Same reports whether two version strings describe the same build. A dirty
// build differs from anything, because the host may run code nobody committed.
func Same(a, b string) bool {
	return a != "" && a == b
}
