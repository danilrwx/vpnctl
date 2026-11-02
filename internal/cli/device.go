package cli

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// DeviceAdd records a device's ssh key and age recipient. The app does this
// itself on registration; the command is for a laptop or a device without the
// app, and for repairing a state by hand.
func DeviceAdd(dir, name, sshKey, ageKey string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	msg, err := ops.DeviceSet(st, name, sshKey, ageKey)
	if err != nil {
		return err
	}

	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}

	fmt.Printf("✓ %s\n", msg)

	return nil
}

// DeviceRemove drops a device from the state; the next apply removes its key
// from every host.
func DeviceRemove(dir, name string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	msg, err := ops.DeviceRemove(st, name)
	if err != nil {
		return err
	}

	if err := commit(dir, msg, nocommit); err != nil {
		return err
	}

	fmt.Printf("✓ %s\n", msg)

	return nil
}

// DeviceList prints the devices and the two keys each one carries.
func DeviceList(dir string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	type row struct {
		Name string `json:"name"`
		SSH  string `json:"ssh"`
		Age  string `json:"age,omitempty"`
	}

	rows := make([]row, 0, len(st.Devices))
	for _, name := range ops.DeviceNames(st) {
		d := st.Devices[name]
		rows = append(rows, row{Name: name, SSH: d.SSH, Age: d.Age})
	}

	return emit(rows, func() {
		for _, r := range rows {
			fmt.Printf("%-12s %s\n", r.Name, r.Age)
		}
	})
}

// DeviceSync adds the age recipients recorded on devices to age.yaml. The
// app's registration carries the recipient; this is the laptop side of it, so
// the phone does not need a second copy-paste.
func DeviceSync(dir string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	added, err := ops.DeviceSync(st)
	if err != nil {
		return err
	}

	if len(added) == 0 {
		fmt.Println("✓ devices: age recipients are up to date")

		return nil
	}

	if err := commit(dir, "devices: sync age recipients", nocommit); err != nil {
		return err
	}

	for _, msg := range added {
		fmt.Printf("✓ %s\n", msg)
	}

	return nil
}
