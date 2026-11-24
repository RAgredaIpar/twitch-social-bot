//go:build windows

package cli

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode     = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode     = kernel32.NewProc("SetConsoleMode")
	procReadConsoleInput   = kernel32.NewProc("ReadConsoleInputW")
	procGetNumConsoleInput = kernel32.NewProc("GetNumberOfConsoleInputEvents")
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

// LiveKey represents non-blocking keyboard commands during live bot execution.
type LiveKey int

const (
	LiveKeyNone LiveKey = iota
	LiveKeyToggleLogs
	LiveKeyTogglePersona
	LiveKeyUp
	LiveKeyDown
	LiveKeyEnter
	LiveKeyEsc
	LiveKeyBackToMenu
	LiveKeyQuit
)

// StartLiveKeyReader polls the Windows Console non-blockingly for live dashboard shortcuts
// (L logs, P persona selector, Up/Down/Enter/Esc navigation, Q menu, Ctrl+C quit).
func StartLiveKeyReader(ctx context.Context) <-chan LiveKey {
	ch := make(chan LiveKey, 16)
	if !IsInteractiveTTY() {
		close(ch)
		return ch
	}
	go func() {
		defer close(ch)
		h := syscall.Handle(os.Stdin.Fd())
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for {
					var count uint32
					r, _, _ := procGetNumConsoleInput.Call(uintptr(h), uintptr(unsafe.Pointer(&count)))
					if r == 0 || count == 0 {
						break
					}
					var rec inputRecord
					var readCount uint32
					r, _, _ = procReadConsoleInput.Call(
						uintptr(h),
						uintptr(unsafe.Pointer(&rec)),
						1,
						uintptr(unsafe.Pointer(&readCount)),
					)
					if r == 0 || readCount == 0 {
						break
					}
					if rec.EventType != 0x0001 {
						continue
					}
					ke := (*keyEventRecord)(unsafe.Pointer(&rec.Event[0]))
					if ke.KeyDown == 0 {
						continue
					}
					ru := rune(ke.UnicodeChar)
					var lk LiveKey
					switch {
					case ru == 3:
						lk = LiveKeyQuit
					case ke.VirtualKeyCode == 0x26 || ru == 'k' || ru == 'K':
						lk = LiveKeyUp
					case ke.VirtualKeyCode == 0x28 || ru == 'j' || ru == 'J':
						lk = LiveKeyDown
					case ke.VirtualKeyCode == 0x0D:
						lk = LiveKeyEnter
					case ke.VirtualKeyCode == 0x1B:
						lk = LiveKeyEsc
					case ru == 'q' || ru == 'Q':
						lk = LiveKeyBackToMenu
					case ru == 'l' || ru == 'L':
						lk = LiveKeyToggleLogs
					case ru == 'p' || ru == 'P':
						lk = LiveKeyTogglePersona
					}
					if lk != LiveKeyNone {
						select {
						case ch <- lk:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()
	return ch
}
