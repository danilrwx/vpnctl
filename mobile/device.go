package mobile

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// RemoveDevice revokes one recipient: the secrets are re-encrypted for the ones
// that remain and the change is committed. A device that is gone stops reading
// anything new, but what it already decrypted stays in git history, so the
// screens ask for the name before calling this.
func (a *App) RemoveDevice(name string) (string, error) {
	recs, err := ageio.LoadRecipients(a.dir)
	if err != nil {
		return "", err
	}

	if recs == nil {
		return "", fmt.Errorf("no %s: nothing to revoke", ageio.RecipientsFile)
	}

	own, err := ageio.OwnRecipient(state.KeyPath())
	if err == nil {
		for _, r := range recs.Recipients {
			if r.Name == name && r.Key == own {
				return "", fmt.Errorf("%s is this device: revoking it would lock the phone out of its own state", name)
			}
		}
	}

	msg, err := ops.KeyRemove(a.st, name)
	if err != nil {
		return "", err
	}

	if a.repo != nil {
		if err := a.repo.Commit(msg); err != nil {
			return "", err
		}
	}

	return msg, nil
}
