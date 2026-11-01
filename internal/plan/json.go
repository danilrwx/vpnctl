package plan

// JSON is the machine-readable plan: what an agent or the app reads before applying.
type JSON []JSONHost

// JSONHost is one host of the plan.
type JSONHost struct {
	Name         string     `json:"name"`
	Marker       string     `json:"marker,omitempty"`
	Unpushed     bool       `json:"unpushed,omitempty"`
	Bootstrapped bool       `json:"bootstrapped"`
	Unreachable  string     `json:"unreachable,omitempty"`
	Changes      int        `json:"changes"`
	Files        []JSONFile `json:"files"`
	Notes        []string   `json:"notes,omitempty"`
}

// JSONFile is one path with its mark.
type JSONFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// JSONOf converts a plan to its wire form.
func JSONOf(plans []HostPlan) JSON {
	out := make(JSON, 0, len(plans))
	for _, p := range plans {
		h := JSONHost{
			Name: p.Name, Marker: p.Marker, Unpushed: p.Unpushed,
			Bootstrapped: p.Bootstrapped, Unreachable: p.Unreachable,
			Changes: p.Changes(), Notes: p.Notes, Files: []JSONFile{},
		}
		for _, f := range p.Files {
			h.Files = append(h.Files, JSONFile{Path: f.Path, Status: f.Status.Mark()})
		}

		out = append(out, h)
	}

	return out
}
