package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// HostFiles maps a path on the host to its contents.
type HostFiles map[string]string

// Rendered is the render result for every described host.
type Rendered map[string]HostFiles

// Walk lists the files under root as path relative to root, sorted.
func Walk(root string) ([]struct{ Rel, Body string }, error) {
	var (
		out   []struct{ Rel, Body string }
		stack []string
	)

	stack = append(stack, root)
	for len(stack) > 0 {
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		entries, err := os.ReadDir(d)
		if err != nil {
			return nil, err
		}

		for _, e := range entries {
			p := filepath.Join(d, e.Name())
			if e.IsDir() {
				stack = append(stack, p)
				continue
			}

			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}

			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil, err
			}

			out = append(out, struct{ Rel, Body string }{rel, string(b)})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })

	return out, nil
}

// RenderHost renders every file the host must have: engine config, units, web server,
// subscriptions, network config and state overrides.
func RenderHost(st *state.State, name string, at time.Time) (HostFiles, error) {
	h, err := st.Host(name)
	if err != nil {
		return nil, err
	}

	env, err := newTemplateEnv(st)
	if err != nil {
		return nil, err
	}

	inb := st.Inbounds[name]
	hs := st.Secrets.Hosts[name]
	f := HostFiles{}
	emit := func(path, t string, ctx map[string]any) error {
		body, err := env.render(t, ctx)
		if err != nil {
			return err
		}

		f[path] = body

		return nil
	}

	// engine
	bin := EngineBin(h.Engine)
	cfgPath := EngineConfigPath(h.Engine)

	if bin == engineSingBox {
		cfg, err := SingBoxConfig(st, name, at)
		if err != nil {
			return nil, err
		}

		f[cfgPath] = cfg

		sets, err := RuleSets(st, name)
		if err != nil {
			return nil, err
		}

		for _, s := range sets {
			f["/etc/sing-box/rules/"+s.Tag+".json"] = s.Body
		}
	} else {
		cfg, err := XrayConfig(st, name, at)
		if err != nil {
			return nil, err
		}

		f[cfgPath] = cfg
	}
	// sing-box understands -D (resource dir), xray does not: it only needs -c
	execCmd := fmt.Sprintf("/usr/local/bin/%s run -D /etc/%s -c /etc/%s/config.json", bin, bin, bin)
	if bin == engineXray {
		execCmd = fmt.Sprintf("/usr/local/bin/%s run -c /etc/%s/config.json", bin, bin)
	}

	if err := emit("/etc/systemd/system/"+bin+".service", "engine.service.j2",
		map[string]any{"host": name, "engine": bin, "exec": execCmd}); err != nil {
		return nil, err
	}

	var (
		awgIfaces  []string
		masquerade []string
		clientAWG  *state.Inbound
	)

	// web server and subscriptions
	if h.Role == state.Entry {
		if h.Domain == "" {
			return nil, fmt.Errorf("the entry has no domain")
		}

		domain := h.Domain

		caddy := map[string]any{
			"host": name, "domain": domain, "caddy_port": CaddyPort,
			"ws_path": "", "ws_port": 0, "profile_title": domain,
			"ntfy_domain": "", "ntfy_port": NtfyPort,
		}
		if ws, ok := inb["ws"]; ok {
			caddy["ws_path"] = pathOr(ws.Path)
			caddy["ws_port"] = ws.Port
		}
		// the notification server rides on the same cover site: reality splices
		// every non-VPN client to Caddy, and Caddy picks the site by SNI
		if st.Alerts.Serve == name {
			caddy["ntfy_domain"] = st.NtfyName(name)
			f["/etc/ntfy/server.yml"] = ntfyServer(st, name)
			f["/etc/systemd/system/ntfy.service"] = ntfyUnit(name)
		}

		if caddy["ws_port"] != 0 || caddy["ntfy_domain"] != "" {
			if err := emit("/etc/caddy/Caddyfile", "Caddyfile.j2", caddy); err != nil {
				return nil, err
			}
		}

		for _, acc := range UsersOf(st, name, at) {
			us, ok := st.Secrets.Users[acc.Name]
			if !ok {
				return nil, fmt.Errorf("no secrets for user %s", acc.Name)
			}

			for _, m := range acc.U.Modes {
				links, err := Links(st, name, acc.Name, m)
				if err != nil {
					return nil, err
				}

				base := "/var/www/sub/" + us.SubID + "/" + m
				f[base] = Base64List(links)

				js, err := SingboxJSON(st, name, acc.Name, acc.U, m)
				if err != nil {
					return nil, err
				}

				f[base+".json"] = js

				yl, err := ClashYAML(st, name, acc.Name, acc.U, m)
				if err != nil {
					return nil, err
				}

				f[base+".yaml"] = yl
			}

			if acc.U.AWG != nil && us.AWG != nil {
				if awg, ok := inb["awg"]; ok {
					if awg.Subnets == nil {
						return nil, fmt.Errorf("awg without subnets")
					}

					net := awg.Subnets.ForMode(acc.U.AWG.Mode)

					var serverPK string
					if hs.AWG != nil {
						serverPK = hs.AWG.Public
					}

					if err := emit("/etc/vpnctl/profiles/"+acc.Name+"/awg.conf", "awg-client.conf.j2", map[string]any{
						"user": acc.Name, "mode": acc.U.AWG.Mode, "domain": domain,
						"address": acc.U.AWG.IP, "private_key": us.AWG.Private,
						"dns": Gateway(net), "server_public_key": serverPK,
						"preshared": us.AWGPreshared, "port": awg.Port,
					}); err != nil {
						return nil, err
					}
				}
			}
		}

		f["/etc/vpnctl/expiry"] = expiryFile(st, name, at)
		// the tick needs the plan and the engine path: both live on the entry
		f["/etc/systemd/system/vpnctl-tick.service"] = tickUnit(st, name, h.Engine)
		f["/etc/systemd/system/vpnctl-tick.timer"] = tickTimer(name)
		// the topology matrix is measured from here, where the links start
		f["/etc/vpnctl/probe.json"] = probePlanJSON(st, name)
		// the chain probe runs on the entry: it is the only host that sees the
		// whole path, and the only one that should alert
		f["/etc/vpnctl/probe.sh"] = probeScript(st, name, EngineBin(h.Engine))
		f["/etc/systemd/system/vpnctl-probe.service"] = probeUnit(name)
		f["/etc/systemd/system/vpnctl-probe.timer"] = probeTimer(name)
		f["/etc/vpnctl/alerts.env"] = alertsEnv(st, name)
		// history lives on the entry: the device reads it from there
		f["/etc/vpnctl/metrics.sh"] = metricsScript(name)
		f["/etc/systemd/system/vpnctl-metrics.service"] = metricsUnit(name)

		f["/etc/systemd/system/vpnctl-metrics.timer"] = metricsTimer(name)
		if mtg, ok := inb["mtg"]; ok {
			if err := emit("/etc/mtg/config.toml", "mtg.toml.j2", map[string]any{
				"host": name, "secret": hs.MTG, "port": mtg.Port, "socks_port": ProbePort,
			}); err != nil {
				return nil, err
			}

			if err := emit("/etc/systemd/system/mtg.service", "mtg.service.j2", map[string]any{"host": name}); err != nil {
				return nil, err
			}
		}
	}

	// AWG: awg0 is clients (entry) and incoming links; awgN are outgoing links
	var incoming []*state.Link

	for _, l := range InLinks(st, name) {
		if containsStr(l.Protocols, "awg") {
			incoming = append(incoming, l)
		}
	}

	if h.Role == state.Entry {
		if awg, ok := inb["awg"]; ok {
			clientAWG = &awg
		}
	}

	if clientAWG != nil || len(incoming) > 0 {
		awgIn, ok := inb["awg"]
		if !ok {
			return nil, fmt.Errorf("awg0 without an awg inbound")
		}

		if hs.AWG == nil {
			return nil, fmt.Errorf("no awg keys for the host")
		}

		keys := *hs.AWG

		var (
			addresses        []string
			peers            []map[string]any
			postUp, postDown []string
		)

		if clientAWG != nil {
			if clientAWG.Subnets == nil {
				return nil, fmt.Errorf("awg without subnets")
			}

			for _, net := range []string{clientAWG.Subnets.Split, clientAWG.Subnets.Full} {
				addresses = append(addresses, fmt.Sprintf("%s/%d", Gateway(net), prefixLen(net)))
			}

			for _, acc := range UsersOf(st, name, at) {
				us, ok := st.Secrets.Users[acc.Name]
				if !ok || acc.U.AWG == nil || us.AWG == nil {
					continue
				}

				peers = append(peers, map[string]any{
					"name": acc.Name, "public_key": us.AWG.Public, "preshared": us.AWGPreshared,
					"allowed_ips": []string{acc.U.AWG.IP + "/32"}, "endpoint": nil,
				})
			}

			postUp = append(postUp, "ip rule add fwmark 1 lookup 100")
			postUp = append(postUp, "ip route add local 0.0.0.0/0 dev lo table 100")
			postDown = append(postDown, "ip rule del fwmark 1 lookup 100")
			postDown = append(postDown, "ip route del local 0.0.0.0/0 dev lo table 100")
		}

		for _, l := range incoming {
			if l.Tunnel == "" {
				return nil, fmt.Errorf("link without a tunnel")
			}

			ls := st.Secrets.Links[l.Key()].AWG
			if ls == nil {
				return nil, fmt.Errorf("no awg keys for the link")
			}

			addresses = append(addresses, TunnelAddr(l.Tunnel, 2))
			masquerade = append(masquerade, l.Tunnel)
			peers = append(peers, map[string]any{
				"name": "link from " + l.From, "public_key": ls.From.Public, "preshared": ls.Preshared,
				"allowed_ips": []string{strings.TrimSuffix(TunnelAddr(l.Tunnel, 1), "/30") + "/32"}, "endpoint": nil,
			})
		}

		if err := emit("/etc/amnezia/amneziawg/awg0.conf", "awg.conf.j2", map[string]any{
			"host": name, "iface": "awg0", "purpose": "clients and incoming links",
			"addresses": addresses, "private_key": keys.Private, "listen_port": awgIn.Port,
			"table_off": false, "post_up": postUp, "post_down": postDown, "peers": peers,
		}); err != nil {
			return nil, err
		}

		awgIfaces = append(awgIfaces, "awg0")
	}

	for _, ol := range OutLinks(st, name) {
		l := ol.L
		if !containsStr(l.Protocols, "awg") {
			continue
		}

		if l.Tunnel == "" {
			return nil, fmt.Errorf("link without a tunnel")
		}

		ls := st.Secrets.Links[l.Key()].AWG
		if ls == nil {
			return nil, fmt.Errorf("no awg keys for the link")
		}

		to, err := st.Host(l.To)
		if err != nil {
			return nil, err
		}

		port := 51820
		if i, ok := st.Inbounds[l.To]["awg"]; ok && i.Port != 0 {
			port = i.Port
		}

		iface := fmt.Sprintf("awg%d", ol.N)

		table := 100 + ol.N
		if err := emit("/etc/amnezia/amneziawg/"+iface+".conf", "awg.conf.j2", map[string]any{
			"host": name, "iface": iface, "purpose": "link to " + l.To,
			"addresses": []string{TunnelAddr(l.Tunnel, 1)}, "private_key": ls.From.Private,
			"listen_port": nil, "table_off": true,
			"post_up":   []string{fmt.Sprintf("ip route add 0.0.0.0/0 dev %%i table %d", table), fmt.Sprintf("ip rule add oif %%i lookup %d", table)},
			"post_down": []string{fmt.Sprintf("ip rule del oif %%i lookup %d", table)},
			"peers": []map[string]any{{
				"name": l.To, "public_key": ls.To.Public, "preshared": ls.Preshared,
				"allowed_ips": []string{"0.0.0.0/0"}, "endpoint": fmt.Sprintf("%s:%d", to.IP, port),
			}},
		}); err != nil {
			return nil, err
		}

		awgIfaces = append(awgIfaces, iface)
	}

	// network
	var tcp, udp []int

	for p, i := range inb {
		if !i.IsEnabled() {
			continue
		}

		switch p {
		case "reality", "mtg":
			tcp = append(tcp, i.Port)
		case "hy2", "awg":
			udp = append(udp, i.Port)
		}
	}

	if h.Role == state.Entry {
		tcp = append(tcp, 80)
	}

	sort.Ints(tcp)
	tcp = dedupInts(tcp)

	sort.Ints(udp)
	udp = dedupInts(udp)

	var tproxy any
	if clientAWG != nil {
		tproxy = TProxyPort
	}
	// IPv6 forwarding is only needed for L3 clients (awg): the kernel then disables accept_ra itself
	awgUsed := len(awgIfaces) > 0
	if err := emit("/etc/nftables.conf", "nftables.conf.j2", map[string]any{
		"host": name, "tcp_ports": tcp, "udp_ports": udp, "awg_ifaces": awgIfaces,
		"tproxy_port": tproxy, "masquerade": masquerade,
	}); err != nil {
		return nil, err
	}

	if err := emit("/etc/sysctl.d/90-vpnctl.conf", "sysctl.conf.j2", map[string]any{
		"host": name, "awg": awgUsed,
	}); err != nil {
		return nil, err
	}

	// overrides/<host>/<path>: taken as is (drift adopted from the host)
	ov := filepath.Join(st.Dir, "overrides", name)
	if fi, err := os.Stat(ov); err == nil && fi.IsDir() {
		files, err := Walk(ov)
		if err != nil {
			return nil, err
		}

		for _, w := range files {
			f["/"+strings.ReplaceAll(w.Rel, string(filepath.Separator), "/")] = w.Body
		}
	}

	return f, nil
}

// Render renders every described host, or only the named one.
func Render(st *state.State, only string, at time.Time) (Rendered, error) {
	out := Rendered{}

	names := make([]string, 0, len(st.Hosts))
	for name := range st.Hosts {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		if only != "" && only != name {
			continue
		}

		f, err := RenderHost(st, name, at)
		if err != nil {
			return nil, fmt.Errorf("render host %s: %w", name, err)
		}

		out[name] = f
	}

	if only != "" && len(out) == 0 {
		return nil, fmt.Errorf("host %q is not described", only)
	}

	return out, nil
}

// Write renders the tree to a directory: <out>/<host>/<path on the host>.
func Write(r Rendered, out string) error {
	hosts := make([]string, 0, len(r))
	for h := range r {
		hosts = append(hosts, h)
	}

	sort.Strings(hosts)

	for _, h := range hosts {
		paths := make([]string, 0, len(r[h]))
		for p := range r[h] {
			paths = append(paths, p)
		}

		sort.Strings(paths)

		for _, p := range paths {
			dst := filepath.Join(out, h, filepath.FromSlash(strings.TrimPrefix(p, "/")))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}

			if err := os.WriteFile(dst, []byte(r[h][p]), 0o644); err != nil {
				return err
			}
		}
	}

	return nil
}

// Check renders twice and reports a difference: render must be reproducible.
func Check(st *state.State, at time.Time) (Rendered, error) {
	a, err := Render(st, "", at)
	if err != nil {
		return nil, err
	}

	b, err := Render(st, "", at)
	if err != nil {
		return nil, err
	}

	for host, fa := range a {
		for path, body := range fa {
			if b[host][path] != body {
				return nil, fmt.Errorf("render is not reproducible: %s:%s differs between runs", host, path)
			}
		}
	}

	return a, nil
}

func prefixLen(cidr string) int {
	if i := strings.LastIndex(cidr, "/"); i >= 0 {
		var n int

		_, _ = fmt.Sscanf(cidr[i+1:], "%d", &n)

		return n
	}

	return 32
}

func dedupInts(v []int) []int {
	out := v[:0]
	for i, x := range v {
		if i == 0 || x != v[i-1] {
			out = append(out, x)
		}
	}

	return out
}
