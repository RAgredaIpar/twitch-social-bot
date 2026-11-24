//go:build windows

package updater

import "os/exec"

func relaunchBinary(exe string, args []string) error {
	// Spawn via cmd /c start using Start() (non-blocking) so the replaced .exe.old
	// process exits immediately and releases its Windows file lock for cleanup.
	cmdArgs := append([]string{"/c", "start", "", exe}, args...)
	cmd := exec.Command("cmd.exe", cmdArgs...)
	return cmd.Start()
}
