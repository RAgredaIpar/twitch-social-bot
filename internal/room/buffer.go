package room

import (
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

type Buffer struct {
	mu          sync.RWMutex
	clock       domain.Clock
	maxMessages int
	maxAge      time.Duration
	messages    []domain.RoomMessage
}

func NewBuffer(clock domain.Clock, maxMessages int, maxAge time.Duration) *Buffer {
	if clock == nil {
		clock = domain.NewRealClock()
	}
	if maxMessages <= 0 {
		maxMessages = 25
	}
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	return &Buffer{
		clock:       clock,
		maxMessages: maxMessages,
		maxAge:      maxAge,
		messages:    make([]domain.RoomMessage, 0, maxMessages),
	}
}

func (b *Buffer) Push(msg domain.RoomMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.clock.Now()
	if msg.Timestamp.IsZero() {
		msg.Timestamp = now
	}

	cutoff := now.Add(-b.maxAge)

	valid := make([]domain.RoomMessage, 0, len(b.messages)+1)
	for _, m := range b.messages {
		if !m.Timestamp.Before(cutoff) {
			valid = append(valid, m)
		}
	}

	if !msg.Timestamp.Before(cutoff) {
		valid = append(valid, msg)
	}

	if len(valid) > b.maxMessages {
		valid = valid[len(valid)-b.maxMessages:]
	}

	b.messages = valid
}

func (b *Buffer) Recent(limit int) []domain.RoomMessage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	cutoff := b.clock.Now().Add(-b.maxAge)

	valid := make([]domain.RoomMessage, 0, len(b.messages))
	for _, m := range b.messages {
		if !m.Timestamp.Before(cutoff) {
			valid = append(valid, m)
		}
	}

	if limit <= 0 || limit > len(valid) {
		res := make([]domain.RoomMessage, len(valid))
		copy(res, valid)
		return res
	}

	selected := valid[len(valid)-limit:]
	res := make([]domain.RoomMessage, len(selected))
	copy(res, selected)
	return res
}

func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.messages = nil
}
