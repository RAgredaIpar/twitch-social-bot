package ratelimit_test

import (
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/ratelimit"
)

func TestCooldownLimiter_Allow_GlobalCooldown(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	limiter := ratelimit.NewCooldownLimiter(clock, 3*time.Second, 15*time.Second)

	// First request from user1 should succeed.
	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to be true on first call")
	}

	// Immediately after, user2 calls Allow. Should fail due to global cooldown.
	if limiter.Allow("user2") {
		t.Fatalf("expected Allow(user2) to be blocked by global cooldown at 0s")
	}

	// Advance clock by 2s (< 3s global cooldown). user2 should still be blocked.
	clock.Advance(2 * time.Second)
	if limiter.Allow("user2") {
		t.Fatalf("expected Allow(user2) to be blocked by global cooldown at 2s")
	}

	// Advance clock by 1.1s (total 3.1s > 3s global cooldown).
	// user2 has never had a successful call, so user cooldown does not apply to user2.
	clock.Advance(1100 * time.Millisecond)
	if !limiter.Allow("user2") {
		t.Fatalf("expected Allow(user2) to succeed after global cooldown expired")
	}
}

func TestCooldownLimiter_Allow_UserCooldown(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	limiter := ratelimit.NewCooldownLimiter(clock, 1*time.Second, 10*time.Second)

	// First call for user1 succeeds.
	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to succeed")
	}

	// Advance 2s. Global cooldown (1s) expired, but user cooldown (10s) has NOT expired for user1.
	clock.Advance(2 * time.Second)
	if limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to be blocked by user cooldown at 2s")
	}

	// Different user (user2) should succeed because global cooldown is satisfied.
	if !limiter.Allow("user2") {
		t.Fatalf("expected Allow(user2) to succeed at 2s")
	}

	// Advance 9s more (total 11s from user1's first call).
	clock.Advance(9 * time.Second)

	// user1 can now succeed (11s >= 10s user cooldown and 9s >= 1s global cooldown).
	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to succeed after user cooldown expired")
	}
}

func TestCooldownLimiter_Allow_ZeroCooldowns(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	limiter := ratelimit.NewCooldownLimiter(clock, 0, 0)

	for i := 0; i < 5; i++ {
		if !limiter.Allow("user1") {
			t.Fatalf("iteration %d: expected Allow(user1) with 0 cooldowns to succeed", i)
		}
	}
}

func TestCooldownLimiter_ResetUser(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	limiter := ratelimit.NewCooldownLimiter(clock, 0, 10*time.Second)

	if !limiter.Allow("user1") {
		t.Fatalf("expected initial Allow(user1) to succeed")
	}
	if limiter.Allow("user1") {
		t.Fatalf("expected subsequent Allow(user1) to be blocked by user cooldown")
	}

	limiter.ResetUser("user1")

	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to succeed after ResetUser")
	}
}

func TestCooldownLimiter_PruneExpired(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	limiter := ratelimit.NewCooldownLimiter(clock, 0, 10*time.Second)

	limiter.Allow("user1") // at t0

	clock.Advance(5 * time.Second)
	limiter.Allow("user2") // at t0 + 5s

	// Advance to t0 + 11s. user1 is 11s old (>= 10s), user2 is 6s old (< 10s).
	clock.Advance(6 * time.Second)
	limiter.PruneExpired()

	// user1 was pruned, so Allow("user1") should succeed.
	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow(user1) to succeed after being pruned")
	}

	// user2 was not pruned and has only elapsed 6s since t0+5s (< 10s).
	if limiter.Allow("user2") {
		t.Fatalf("expected Allow(user2) to be blocked by user cooldown")
	}
}

func TestCooldownLimiter_NilClock(t *testing.T) {
	limiter := ratelimit.NewCooldownLimiter(nil, 10*time.Millisecond, 20*time.Millisecond)
	if limiter == nil {
		t.Fatalf("expected non-nil limiter with nil clock")
	}
	if !limiter.Allow("user1") {
		t.Fatalf("expected Allow to work with default real clock")
	}
}

func TestCooldownLimiter_Concurrency(t *testing.T) {
	clock := domain.NewFakeClock(time.Now())
	limiter := ratelimit.NewCooldownLimiter(clock, 1*time.Millisecond, 5*time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			userID := "user"
			if id%2 == 0 {
				userID = "user2"
			}
			limiter.Allow(userID)
			if id%5 == 0 {
				limiter.ResetUser(userID)
			}
			if id%10 == 0 {
				limiter.PruneExpired()
			}
		}(i)
	}
	wg.Wait()
}

func TestCooldownLimiter_BroadcasterBypassesUserCooldown(t *testing.T) {
	clk := domain.NewFakeClock(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC))
	lim := ratelimit.NewCooldownLimiter(clk, 3*time.Second, 8*time.Second)

	if !lim.AllowForUser("streamer_1", true) {
		t.Fatal("expected first broadcaster message allowed")
	}
	// Within 3s global cooldown: still blocked
	clk.Advance(2 * time.Second)
	if lim.AllowForUser("streamer_1", true) {
		t.Fatal("expected broadcaster blocked within 3s global cooldown")
	}
	// After 4s total (> 3s global, < 8s user): broadcaster allowed
	clk.Advance(2 * time.Second)
	if !lim.AllowForUser("streamer_1", true) {
		t.Fatal("expected broadcaster allowed after global cooldown despite 8s user cooldown")
	}

	// Regular viewer still obeys 8s per-viewer cooldown
	clk.Advance(4 * time.Second)
	if !lim.AllowForUser("viewer_1", false) {
		t.Fatal("expected regular viewer first message allowed")
	}
	clk.Advance(4 * time.Second)
	if lim.AllowForUser("viewer_1", false) {
		t.Fatal("expected regular viewer still blocked within 8s user cooldown")
	}
	clk.Advance(5 * time.Second)
	if !lim.AllowForUser("viewer_1", false) {
		t.Fatal("expected regular viewer allowed after 8s user cooldown")
	}
}
