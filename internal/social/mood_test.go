package social_test

import (
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/social"
)

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func TestMoodTracker_TransitionsAndDecay(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)}
	m := social.NewMoodTracker(clk)

	if got := m.Current(); got != "chill" {
		t.Fatalf("expected initial mood 'chill', got %q", got)
	}

	// 2 negative interactions -> annoyed
	m.Record(-2)
	m.Record(-3)
	if got := m.Current(); got != "annoyed" {
		t.Fatalf("expected mood 'annoyed', got %q", got)
	}

	// Advance past 10m TTL -> relaxes back to chill
	clk.now = clk.now.Add(11 * time.Minute)
	if got := m.Current(); got != "chill" {
		t.Fatalf("expected mood to decay to 'chill', got %q", got)
	}

	// 3 positive interactions -> hype
	m.Record(3)
	m.Record(3)
	m.Record(3)
	if got := m.Current(); got != "hype" {
		t.Fatalf("expected mood 'hype', got %q", got)
	}

	// Reset via TTL, then 4 neutral interactions -> playful
	clk.now = clk.now.Add(11 * time.Minute)
	for i := 0; i < 4; i++ {
		m.Record(0)
	}
	if got := m.Current(); got != "playful" {
		t.Fatalf("expected mood 'playful', got %q", got)
	}
}
