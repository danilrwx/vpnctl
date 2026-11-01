package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// expiryFile lists the accounts and the day they stop working: the agent's tick
// reads it when nobody is online to apply a new state.
func expiryFile(st *state.State, entry string) string {
	type row struct {
		email string
		date  string
	}
	var rows []row
	for _, acc := range UsersOf(st, entry) {
		if acc.U.Expires == "" {
			continue
		}
		for _, m := range acc.U.Modes {
			for _, p := range acc.U.Protocols {
				if !isEngineUser(p) {
					continue
				}
				rows = append(rows, row{st.Email(acc.Name, m, p), acc.U.Expires})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].email < rows[j].email })
	var b strings.Builder
	b.WriteString("# vpnctl · account expiry, read by vpnctl-agent tick\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s %s\n", r.email, r.date)
	}
	return b.String()
}
