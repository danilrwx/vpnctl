package mobile

import "fmt"

// A form can go stale while an inbound is turned off. Refuse unusable access
// before persisting it, rather than silently issuing an empty subscription.
func (a *App) userProtocolsEnabled(name string) error {
	u := a.st.Users[name]

	entry, ok := a.st.EntryOf(u)
	if !ok {
		return fmt.Errorf("user %q has no unambiguous entry", name)
	}

	for _, protocol := range u.Protocols {
		inbound, ok := a.st.Inbounds[entry][protocol]
		if !ok || !inbound.IsEnabled() {
			return fmt.Errorf("entry %q: protocol %s is unavailable or disabled", entry, protocol)
		}
	}

	return nil
}
