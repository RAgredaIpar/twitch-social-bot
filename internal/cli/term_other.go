//go:build !windows

package cli

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
)

// KeyEvent represents a normalized navigation key press.
type KeyEvent int

const (
	KeyOther KeyEvent = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyEsc
)

// EnableANSI is a no-op on POSIX terminals that natively support ANSI sequences.
func EnableANSI() {}

// IsInteractiveTTY returns true when stdin is connected to an interactive terminal.
func IsInteractiveTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ReadNavKey reads a single Up/Down/Enter/Esc key from a POSIX TTY, or falls
// back to line-based input in non-TTY / test environments.
func ReadNavKey(in io.Reader, reader *bufio.Reader) (KeyEvent, string, error) {
	if in != os.Stdin || !IsInteractiveTTY() {
		raw, err := reader.ReadString('\n')
		if err != nil && len(strings.TrimSpace(raw)) == 0 {
			return KeyOther, "", err
		}
		return KeyOther, strings.TrimSpace(raw), nil
	}

	_ = exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1", "-echo").Run()
	defer func() {
		_ = exec.Command("stty", "-F", "/dev/tty", "sane").Run()
	}()

	buf := make([]byte, 3)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return KeyOther, "", err
	}
	if n == 1 {
		switch buf[0] {
		case '\r', '\n':
			return KeyEnter, "", nil
		case 0x1b:
			return KeyEsc, "", nil
		case 'k', 'K':
			return KeyUp, "", nil
		case 'j', 'J':
			return KeyDown, "", nil
		default:
			return KeyOther, string(buf[0]), nil
		}
	}
	if n == 3 && buf[0] == 0x1b && buf[1] == '[' {
		switch buf[2] {
		case 'A':
			return KeyUp, "", nil
		case 'B':
			return KeyDown, "", nil
		}
	}
	return KeyOther, "", nil
}
