// vpnctl-agent is the narrow privileged executor: one signed request in, one
// answer out. It is installed as a forced command in authorized_keys, so a
// key can do nothing else on the host.
package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/version"
)

const (
	maxRequest = 8 << 20 // 8 MiB
	keysDir    = "/etc/vpnctl/agents"
	hostFile   = "/etc/vpnctl/host"
	nonceDir   = "/var/lib/vpnctl/nonces"
	nonceTTL   = 15 * time.Minute
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("vpnctl-agent", version.String())
		return
	}
	// a person runs `vpnctl-agent probe` on the entry to see the matrix locally
	if len(os.Args) > 1 && os.Args[1] == "probe" {
		fs := flag.NewFlagSet("probe", flag.ContinueOnError)
		plan := fs.String("plan", "/etc/vpnctl/probe.json", "the rendered plan")

		timeout := fs.Int("timeout", 5, "seconds per handshake")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		body, err := os.ReadFile(*plan)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		matrix, err := agent.RunProbePlan(body, time.Duration(*timeout)*time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		b, _ := json.Marshal(matrix)
		fmt.Println(string(b))

		return
	}
	// a timer runs `vpnctl-agent tick`: disable the accounts whose day has come
	if len(os.Args) > 1 && os.Args[1] == "tick" {
		fs := flag.NewFlagSet("tick", flag.ContinueOnError)
		expiry := fs.String("expiry", "/etc/vpnctl/expiry", "the plan apply wrote")
		config := fs.String("config", "/etc/sing-box/config.json", "engine config to edit")
		engine := fs.String("engine", "sing-box", "sing-box or xray")
		subs := fs.String("subs", "/var/www/sub", "web root of the subscriptions")
		awg := fs.String("awg", "", "awg interface config, empty when there is none")

		dry := fs.Bool("dry-run", false, "report what would be removed and touch nothing")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		err := agent.RunTick(agent.TickOptions{
			ExpiryPath: *expiry, EngineConfig: *config, EngineBin: *engine,
			SubRoot: *subs, AWGConfig: *awg, DryRun: *dry, Now: time.Now(),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		return
	}

	body, err := io.ReadAll(io.LimitReader(os.Stdin, maxRequest))
	if err != nil {
		write(agent.Response{V: agent.Version, Code: "io", Error: err.Error()})
		os.Exit(1)
	}

	var req agent.Request
	if err := json.Unmarshal(body, &req); err != nil {
		write(agent.Response{V: agent.Version, Code: "request", Error: err.Error()})
		os.Exit(1)
	}

	host := hostName()

	keys, err := publicKeys(keysDir)
	if err != nil {
		write(agent.Response{V: agent.Version, Code: "keys", Error: err.Error()})
		os.Exit(1)
	}

	var lastErr error

	verified := false

	for _, k := range keys {
		if err := agent.Verify(&req, host, k, time.Now()); err != nil {
			lastErr = err
			continue
		}

		verified = true

		break
	}

	if !verified {
		code := "signature"

		if lastErr != nil {
			var ae *agent.Error
			if errors.As(lastErr, &ae) {
				code = ae.Code
			}

			write(agent.Response{V: agent.Version, Code: code, Error: lastErr.Error()})
		} else {
			write(agent.Response{V: agent.Version, Code: "keys", Error: "no public keys in " + keysDir})
		}

		os.Exit(1)
	}

	if err := (agent.NonceCache{Dir: nonceDir, TTL: nonceTTL}).Use(req.Nonce); err != nil {
		write(agent.Response{V: agent.Version, Code: "replay", Error: err.Error()})
		os.Exit(1)
	}

	var res agent.Response

	switch req.Op {
	case "plan":
		res = agent.Plan(&req, "/etc/vpnctl/applied-commit", "/etc/vpnctl/bootstrap")
	case "apply":
		res = agent.Apply(&req, "/tmp", func(line string) { fmt.Fprintln(os.Stderr, line) })
	case "probe":
		body, err := os.ReadFile("/etc/vpnctl/probe.json")
		if err != nil {
			res = agent.Response{V: agent.Version, Code: "probe", Error: err.Error()}
			break
		}

		matrix, err := agent.RunProbePlan(body, 0)
		if err != nil {
			res = agent.Response{V: agent.Version, Code: "probe", Error: err.Error()}
			break
		}

		res = agent.Response{V: agent.Version, OK: true, Matrix: matrix}
	default:
		res = agent.Response{V: agent.Version, Code: "op", Error: "unknown op " + req.Op}
	}

	write(res)

	if !res.OK {
		os.Exit(1)
	}
}

func write(res agent.Response) {
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
}

func hostName() string { return hostNameFrom(hostFile) }

// hostNameFrom is what the signature is checked against: the name the state
// gave this host, or the machine name when the file is missing or empty.
func hostNameFrom(path string) string {
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}

	h, _ := os.Hostname()

	return h
}

func publicKeys(dir string) ([]*ecdsa.PublicKey, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var out []*ecdsa.PublicKey

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}

		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}

		k, err := agent.ParsePublicKey(b)
		if err != nil {
			continue
		}

		out = append(out, k)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no usable key in %s", dir)
	}

	return out, nil
}
