package domain

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func NewRealClock() RealClock {
	return RealClock{}
}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewFakeClock(initial time.Time) *FakeClock {
	return &FakeClock{now: initial.UTC()}
}

func (f *FakeClock) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

func (f *FakeClock) Advance(d time.Duration) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	return f.now
}

func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}
