package cli

import (
	"strings"
	"sync"
)

// LogBuffer is a thread-safe ring buffer implementing io.Writer to capture
// standard logger output cleanly while the live animated dashboard is active.
type LogBuffer struct {
	mu      sync.Mutex
	max     int
	partial strings.Builder
	lines   []string
}

// NewLogBuffer creates a LogBuffer retaining up to maxLines recent lines (defaults to 200).
func NewLogBuffer(maxLines int) *LogBuffer {
	if maxLines <= 0 {
		maxLines = 200
	}
	return &LogBuffer{
		max: maxLines,
	}
}

// Write appends log bytes, splitting by newline and keeping at most max recent lines.
func (lb *LogBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, b := range p {
		if b == '\r' {
			continue
		}
		if b == '\n' {
			lb.pushLocked(lb.partial.String())
			lb.partial.Reset()
			continue
		}
		_ = lb.partial.WriteByte(b)
	}
	return len(p), nil
}

func (lb *LogBuffer) pushLocked(line string) {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == "" {
		return
	}
	lb.lines = append(lb.lines, trimmed)
	if len(lb.lines) > lb.max {
		lb.lines = lb.lines[len(lb.lines)-lb.max:]
	}
}

// Lines returns up to limit most recent log lines (or all buffered lines if limit <= 0).
func (lb *LogBuffer) Lines(limit int) []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	total := len(lb.lines)
	if limit <= 0 || limit > total {
		limit = total
	}
	start := total - limit
	out := make([]string, limit)
	copy(out, lb.lines[start:])
	return out
}
