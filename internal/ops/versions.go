package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"

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
