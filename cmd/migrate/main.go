// Converts the sops secrets file into a single age-encrypted file.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/ageio"
	"github.com/getsops/sops/v3/decrypt"
)

func main() {
	dir, key := os.Args[1], os.Args[2]
	old := filepath.Join(dir, "secrets.sops.yaml")
	plain, err := decrypt.File(old, "yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sops:", err)
		os.Exit(1)
	}
	dst := filepath.Join(dir, "secrets.age.yaml")
	if err := ageio.Write(dst, key, plain); err != nil {
		fmt.Fprintln(os.Stderr, "age:", err)
		os.Exit(1)
	}
	back, err := ageio.Decrypt(dst, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		os.Exit(1)
	}
	if string(back) != string(plain) {
		fmt.Fprintln(os.Stderr, "round trip mismatch")
		os.Exit(1)
	}
	fmt.Printf("ok: %d bytes, %s\n", len(plain), dst)
}
