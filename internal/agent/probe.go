package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// ProbeResult is one cell of the matrix: an edge, a protocol, and whether that
// protocol's own handshake completed.
type ProbeResult struct {
	Link     string `json:"link"`
	Protocol string `json:"protocol"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
}

// ProbeTimeout bounds one handshake: a link that answers slower than this is
// not usable for a phone on the other side either.
const ProbeTimeout = 5 * time.Second

// RunProbePlan measures every cell of the plan. Nothing is written and nothing
// is restarted: the matrix is a read of the edge, from the near side.
func RunProbePlan(body []byte, timeout time.Duration) ([]ProbeResult, error) {
	plan, err := state.ParseProbePlan(body)
	if err != nil {
		return nil, fmt.Errorf("parse the probe plan: %w", err)
	}

	if timeout <= 0 {
		timeout = ProbeTimeout
	}

	var out []ProbeResult

	for _, l := range plan.Links {
		for _, c := range l.Probes {
			r := ProbeResult{Link: l.From + "->" + l.To, Protocol: c.Protocol}
			r.Detail, r.OK = ProbeCell(l.Addr, c, timeout)
			out = append(out, r)
		}
	}

	return out, nil
}

// ProbeCell runs the handshake of one protocol. A TCP connect is deliberately
// not enough: reality with a reachable port and a broken handshake is the exact
// failure the old matrix showed as green.
func ProbeCell(addr string, c state.ProbeCell, timeout time.Duration) (string, bool) {
	target := net.JoinHostPort(addr, strconv.Itoa(c.Port))
	switch c.Protocol {
	case "reality":
		dialer := &net.Dialer{Timeout: timeout}

		conn, err := tls.DialWithDialer(dialer, "tcp", target, &tls.Config{
			ServerName:         c.SNI,
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS13,
			NextProtos:         []string{"h2"},
		})
		if err != nil {
			return err.Error(), false
		}

		defer func() { _ = conn.Close() }()

		state := conn.ConnectionState()

		return "tls " + tls.VersionName(state.Version), true
	case "ws":
		client := &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: c.SNI, InsecureSkipVerify: true}},
		}

		req, err := http.NewRequest(http.MethodGet, "https://"+target+pathOrSlash(c.Path), nil)
		if err != nil {
			return err.Error(), false
		}

		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dmVwbmN0bC1wcm9iZS0=")

		resp, err := client.Do(req)
		if err != nil {
			return err.Error(), false
		}

		defer func() { _ = resp.Body.Close() }()
		// 101 is the upgrade; 400 and 426 are still an answer from a live server
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return "101 switching protocols", true
		}

		return fmt.Sprintf("http %d without an upgrade", resp.StatusCode), resp.StatusCode < 500
	case "hy2":
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		conn, err := quic.DialAddr(ctx, target, &tls.Config{
			ServerName:         c.SNI,
			InsecureSkipVerify: true,
			NextProtos:         []string{"h3"},
		}, nil)
		if err != nil {
			return err.Error(), false
		}

		_ = conn.CloseWithError(0, "")

		return "quic handshake", true
	case "awg":
		return "unverified: a wireguard handshake needs the peer, and inventing one would be a lie", false
	}

	return "unverified: no probe for " + c.Protocol, false
}

func pathOrSlash(p string) string {
	if p == "" {
		return "/"
	}

	return p
}
