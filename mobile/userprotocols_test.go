package mobile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDisabledHY2MutationDoesNotWrite(t *testing.T) {
	for _, operation := range []string{"add", "edit"} {
		t.Run(operation, func(t *testing.T) {
			a := openFixture(t)
			inbound := a.st.Inbounds["ru-msk-1"]["hy2"]
			off := false
			inbound.Enabled = &off
			a.st.Inbounds["ru-msk-1"]["hy2"] = inbound

			before, err := json.Marshal(a.st)
			if err != nil {
				t.Fatal(err)
			}

			disk := diskFiles(t, a.dir)
			if operation == "add" {
				_, err = a.AddUser("disabled-hy2", "split", "hy2", false, "")
			} else {
				_, err = a.EditUser("brat", "split", "hy2", false, "")
			}

			if err == nil || !strings.Contains(err.Error(), "unavailable or disabled") {
				t.Fatalf("expected disabled protocol error, got %v", err)
			}

			after, err := json.Marshal(a.st)
			if err != nil {
				t.Fatal(err)
			}

			if string(before) != string(after) || !reflect.DeepEqual(disk, diskFiles(t, a.dir)) {
				t.Fatal("rejected HY2 access changed memory or disk")
			}
		})
	}
}
