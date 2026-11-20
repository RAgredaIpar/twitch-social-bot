//go:build windows

package updater

import "os/exec"

func relaunchBinary(exe string, args []string) error {
	// Spawn via cmd /c start so the updated binary receives a clean interactive console
	// and the replaced .exe.old process exits immediately, releasing its file lock.
	cmdArgs := append([]string{"/c", "start", "", exe}, args...)
	cmd := exec.Command("cmd.exe", cmdArgs...)
	return cmd.Run()
}
