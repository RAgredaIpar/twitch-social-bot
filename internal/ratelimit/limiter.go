package ratelimit

import (
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

type CooldownLimiter struct {
	mu             sync.Mutex
	clock          domain.Clock
	globalCooldown time.Duration
	userCooldown   time.Duration
	lastGlobal     time.Time
	lastUsers      map[string]time.Time
}

func NewCooldownLimiter(clock domain.Clock, globalCooldown, userCooldown time.Duration) *CooldownLimiter {
	if clock == nil {
		clock = domain.NewRealClock()
	}
	return &CooldownLimiter{
		clock:          clock,
		globalCooldown: globalCooldown,
		userCooldown:   userCooldown,
		lastUsers:      make(map[string]time.Time),
	}
}

func (l *CooldownLimiter) Allow(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()

	if l.globalCooldown > 0 && !l.lastGlobal.IsZero() && now.Sub(l.lastGlobal) < l.globalCooldown {
		return false
	}

	if l.userCooldown > 0 {
		if lastUser, ok := l.lastUsers[userID]; ok && now.Sub(lastUser) < l.userCooldown {
			return false
		}
	}

	l.lastGlobal = now
	l.lastUsers[userID] = now
	return true
}

func (l *CooldownLimiter) ResetUser(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.lastUsers, userID)
}

func (l *CooldownLimiter) PruneExpired() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	for uid, t := range l.lastUsers {
		if now.Sub(t) >= l.userCooldown {
			delete(l.lastUsers, uid)
		}
	}
}
