//go:build windows

package cli

import (
	"bufio"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode  = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode  = kernel32.NewProc("SetConsoleMode")
	procReadConsoleInput = kernel32.NewProc("ReadConsoleInputW")
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

type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

type keyEventRecord struct {
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
}

// EnableANSI turns on Windows Virtual Terminal Processing for 24-bit ANSI colors.
func EnableANSI() {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := syscall.Handle(f.Fd())
		var mode uint32
		r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
		if r != 0 {
			_, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
		}
	}
}

// IsInteractiveTTY returns true when stdin is connected to an interactive terminal.
func IsInteractiveTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ReadNavKey reads a single Up/Down/Enter/Esc key from the Windows Console when
// reading from an interactive os.Stdin, or falls back to line-based input in tests.
func ReadNavKey(in io.Reader, reader *bufio.Reader) (KeyEvent, string, error) {
	if in != os.Stdin || !IsInteractiveTTY() {
		raw, err := reader.ReadString('\n')
		if err != nil && len(strings.TrimSpace(raw)) == 0 {
			return KeyOther, "", err
		}
		return KeyOther, strings.TrimSpace(raw), nil
	}

	h := syscall.Handle(os.Stdin.Fd())
	var rec inputRecord
	var readCount uint32
	for {
		r, _, err := procReadConsoleInput.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&rec)),
			1,
			uintptr(unsafe.Pointer(&readCount)),
		)
		if r == 0 {
			return KeyOther, "", err
		}
		if rec.EventType != 0x0001 { // KEY_EVENT
			continue
		}
		ke := (*keyEventRecord)(unsafe.Pointer(&rec.Event[0]))
		if ke.KeyDown == 0 {
			continue
		}
		switch ke.VirtualKeyCode {
		case 0x26: // VK_UP
			return KeyUp, "", nil
		case 0x28: // VK_DOWN
			return KeyDown, "", nil
		case 0x0D: // VK_RETURN
			return KeyEnter, "", nil
		case 0x1B: // VK_ESCAPE
			return KeyEsc, "", nil
		}
		if ke.UnicodeChar != 0 {
			ch := rune(ke.UnicodeChar)
			if ch == 'k' || ch == 'K' {
				return KeyUp, "", nil
			}
			if ch == 'j' || ch == 'J' {
				return KeyDown, "", nil
			}
			return KeyOther, string(ch), nil
		}
	}
}
