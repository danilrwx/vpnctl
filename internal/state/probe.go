package state

import "encoding/json"

// ProbePlan is the rendered /etc/vpnctl/probe.json: what the entry measures and
// where. It carries no secrets — a handshake needs the address, the port and the
// name on the certificate, nothing else.
type ProbePlan struct {
	Host  string      `json:"host"`
	Links []ProbeLink `json:"links"`
}

// ProbeLink is one edge, from the entry towards an exit.
type ProbeLink struct {
	From   string      `json:"from"`
	To     string      `json:"to"`
	Addr   string      `json:"addr"`
	Probes []ProbeCell `json:"probes"`
}

// ProbeCell is one protocol on that edge.
type ProbeCell struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	SNI      string `json:"sni,omitempty"`
	Path     string `json:"path,omitempty"`
}

// ParseProbePlan reads the rendered plan.
func ParseProbePlan(b []byte) (ProbePlan, error) {
	var p ProbePlan

	err := json.Unmarshal(b, &p)

	return p, err
}
