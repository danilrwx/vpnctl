package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// releaseURL is the artifact a component is installed from.
func releaseURL(name, version string) (string, bool) {
	switch name {
	case "sing-box":
		return fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/sing-box-%s-linux-amd64.tar.gz", version, version), true
	case "xray":
		return fmt.Sprintf("https://github.com/XTLS/Xray-core/releases/download/v%s/Xray-linux-64.zip", version), true
	case "caddy":
		return fmt.Sprintf("https://github.com/caddyserver/caddy/releases/download/v%s/caddy_%s_linux_amd64.tar.gz", version, version), true
	case "mtg":
		return fmt.Sprintf("https://github.com/9seconds/mtg/releases/download/v%s/mtg-%s-linux-amd64.tar.gz", version, version), true
	case "ntfy":
		return fmt.Sprintf("https://github.com/binwiederhier/ntfy/releases/download/v%s/ntfy_%s_linux_amd64.tar.gz", version, version), true
	}
	// geodata is a moving `latest`: there is nothing stable to pin
	return "", false
}

// VersionsPin downloads every pinned artifact and records its sha256.
func VersionsPin(st *state.State, only string) ([]string, error) {
	names := make([]string, 0, len(st.Versions))
	for n := range st.Versions {
		if only == "" || only == n {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no such component in versions.yaml")
	}
	var log []string
	for _, n := range names {
		p := st.Versions[n]
		url, ok := releaseURL(n, p.Version)
		if !ok {
			log = append(log, fmt.Sprintf("%s: not a downloadable release, skipped", n))
			continue
		}
		sum, err := fetchSum(url)
		if err != nil {
			return log, fmt.Errorf("%s %s: %w", n, p.Version, err)
		}
		p.SHA256 = sum
		st.Versions[n] = p
		log = append(log, fmt.Sprintf("%s %s: %s", n, p.Version, sum))
	}
	return log, nil
}

func fetchSum(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "vpnctl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var _ = os.Stat

// repoOf maps a component to the GitHub repository its releases come from.
// A component that is not here (awg, geodata) has no release to compare with.
var repoOf = map[string]string{
	"sing-box": "SagerNet/sing-box",
	"xray":     "XTLS/Xray-core",
	"caddy":    "caddyserver/caddy",
	"mtg":      "9seconds/mtg",
	"ntfy":     "binwiederhier/ntfy",
}

// LatestTag asks GitHub for the newest release tag without the API: a HEAD to
// /releases/latest answers with a redirect, and that costs no token and no rate
// limit. Not every host can reach github.com, so the caller decides what a
// failure means.
func LatestTag(repo string) (string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Head("https://github.com/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("%s: no redirect to a tag (status %s)", repo, resp.Status)
	}
	return strings.TrimPrefix(loc[i+len("/tag/"):], "v"), nil
}

// Update is one component the upstream has moved ahead of the pin.
type Update struct {
	Name   string `json:"name"`
	Pinned string `json:"pinned"`
	Latest string `json:"latest"`
	Kind   string `json:"kind"`
}

// VersionKind says how big a jump is: patch, minor or major. Anything that does
// not parse is a major, because a version we cannot read is not a safe upgrade.
func VersionKind(pinned, latest string) string {
	p, ok1 := parseVersion(pinned)
	l, ok2 := parseVersion(latest)
	if !ok1 || !ok2 {
		return "major"
	}
	switch {
	case l[0] != p[0]:
		return "major"
	case l[1] != p[1]:
		return "minor"
	default:
		return "patch"
	}
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Check compares the pins with the newest release the upstream has. A component
// that cannot be reached is reported, not skipped: silence would look like
// "up to date".
func Check(st *state.State) ([]Update, error) {
	names := make([]string, 0, len(st.Versions))
	for n := range st.Versions {
		if _, ok := repoOf[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []Update
	for _, n := range names {
		latest, err := LatestTag(repoOf[n])
		if err != nil {
			return out, fmt.Errorf("%s: %w", n, err)
		}
		pinned := st.Versions[n].Version
		if latest == pinned {
			continue
		}
		out = append(out, Update{Name: n, Pinned: pinned, Latest: latest, Kind: VersionKind(pinned, latest)})
	}
	return out, nil
}

// Patches is the subset that may be pulled in without a human: the same major
// and minor, so the config language is the one we already render.
func Patches(updates []Update) []Update {
	var out []Update
	for _, u := range updates {
		if u.Kind == "patch" {
			out = append(out, u)
		}
	}
	return out
}
