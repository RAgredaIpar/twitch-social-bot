//go:build !windows

package updater

import (
	"os"
	"os/exec"
)

func relaunchBinary(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
