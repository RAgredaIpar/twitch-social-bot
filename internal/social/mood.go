package social

import (
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

const (
	moodWindowSize = 6
	moodTTL        = 10 * time.Minute
)

type moodSample struct {
	delta int
	at    time.Time
}

// MoodTracker maintains a rolling session mood based on recent chat interactions.
type MoodTracker struct {
	mu      sync.Mutex
	clock   domain.Clock
	samples []moodSample
}

func NewMoodTracker(clock domain.Clock) *MoodTracker {
	if clock == nil {
		clock = domain.NewRealClock()
	}
	return &MoodTracker{clock: clock}
}

// Record adds a new interaction affinity delta to the rolling mood window.
func (m *MoodTracker) Record(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.clock.Now()
	m.pruneLocked(now)
	m.samples = append(m.samples, moodSample{delta: delta, at: now})
	if len(m.samples) > moodWindowSize {
		m.samples = m.samples[len(m.samples)-moodWindowSize:]
	}
}

// Current returns the bot's active mood: "chill", "hype", "annoyed", or "playful".
func (m *MoodTracker) Current() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneLocked(m.clock.Now())
	if len(m.samples) == 0 {
		return "chill"
	}

	negatives, positives, net := 0, 0, 0
	for _, s := range m.samples {
		net += s.delta
		if s.delta < 0 {
			negatives++
		} else if s.delta > 0 {
			positives++
		}
	}

	switch {
	case negatives >= 2 || net <= -5:
		return "annoyed"
	case positives >= 3 || net >= 8:
		return "hype"
	case len(m.samples) >= 4:
		return "playful"
	default:
		return "chill"
	}
}

func (m *MoodTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-moodTTL)
	kept := m.samples[:0]
	for _, s := range m.samples {
		if !s.at.Before(cutoff) {
			kept = append(kept, s)
		}
	}
	m.samples = kept
}
