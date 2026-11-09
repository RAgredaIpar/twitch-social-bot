package llm

import (
	"errors"
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half-open"
)

var ErrCircuitOpen = errors.New("llm circuit breaker is open")

type CircuitBreaker struct {
	mu                    sync.Mutex
	clock                 domain.Clock
	failureThreshold      int
	openCooldown          time.Duration
	state                 string
	consecutiveFailures   int
	openedAt              time.Time
	halfOpenProbeInFlight bool
}

func NewCircuitBreaker(clock domain.Clock, failureThreshold int, openCooldown time.Duration) *CircuitBreaker {
	if clock == nil {
		clock = domain.NewRealClock()
	}
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if openCooldown <= 0 {
		openCooldown = 20 * time.Second
	}

	return &CircuitBreaker{
		clock:            clock,
		failureThreshold: failureThreshold,
		openCooldown:     openCooldown,
		state:            StateClosed,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		if cb.clock.Now().Sub(cb.openedAt) >= cb.openCooldown {
			cb.state = StateHalfOpen
			if !cb.halfOpenProbeInFlight {
				cb.halfOpenProbeInFlight = true
				return true
			}
			return false
		}
		return false

	case StateHalfOpen:
		if !cb.halfOpenProbeInFlight {
			cb.halfOpenProbeInFlight = true
			return true
		}
		return false

	default:
		return true
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
	cb.halfOpenProbeInFlight = false
	cb.state = StateClosed
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	cb.halfOpenProbeInFlight = false

	if cb.state == StateHalfOpen || cb.consecutiveFailures >= cb.failureThreshold {
		cb.state = StateOpen
		cb.openedAt = cb.clock.Now()
	}
}

func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
