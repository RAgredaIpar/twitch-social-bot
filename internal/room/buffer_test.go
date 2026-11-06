package room_test

import (
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/room"
)

func TestBuffer_Defaults(t *testing.T) {
	buf := room.NewBuffer(nil, 0, 0)
	if buf == nil {
		t.Fatalf("expected non-nil buffer")
	}

	// Should allow pushing without panicking and return empty Recent
	msgs := buf.Recent(10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestBuffer_PushAndRecent_Order(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 5, 10*time.Minute)

	buf.Push(domain.RoomMessage{UserID: "u1", DisplayName: "User1", Text: "msg1", Timestamp: t0})
	buf.Push(domain.RoomMessage{UserID: "u2", DisplayName: "User2", Text: "msg2", Timestamp: t0.Add(1 * time.Second)})
	buf.Push(domain.RoomMessage{UserID: "u3", DisplayName: "User3", Text: "msg3", Timestamp: t0.Add(2 * time.Second)})

	recent := buf.Recent(0)
	if len(recent) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(recent))
	}
	if recent[0].Text != "msg1" || recent[1].Text != "msg2" || recent[2].Text != "msg3" {
		t.Fatalf("unexpected chronological order: %+v", recent)
	}
}

func TestBuffer_Push_SetsTimestampIfZero(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 5, 10*time.Minute)

	buf.Push(domain.RoomMessage{UserID: "u1", DisplayName: "User1", Text: "no timestamp"})

	recent := buf.Recent(1)
	if len(recent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(recent))
	}
	if !recent[0].Timestamp.Equal(t0) {
		t.Fatalf("expected timestamp %v, got %v", t0, recent[0].Timestamp)
	}
}

func TestBuffer_Push_EvictsOlderThanMaxAge(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 10, 5*time.Minute)

	buf.Push(domain.RoomMessage{UserID: "u1", Text: "old msg", Timestamp: t0})

	// Advance clock by 6 minutes
	clock.Advance(6 * time.Minute)
	buf.Push(domain.RoomMessage{UserID: "u2", Text: "new msg", Timestamp: clock.Now()})

	recent := buf.Recent(0)
	if len(recent) != 1 {
		t.Fatalf("expected 1 message after eviction, got %d", len(recent))
	}
	if recent[0].Text != "new msg" {
		t.Fatalf("expected 'new msg', got '%s'", recent[0].Text)
	}
}

func TestBuffer_Push_EvictsOverMaxMessages(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 3, 10*time.Minute)

	buf.Push(domain.RoomMessage{UserID: "u1", Text: "msg1", Timestamp: t0})
	buf.Push(domain.RoomMessage{UserID: "u2", Text: "msg2", Timestamp: t0.Add(1 * time.Second)})
	buf.Push(domain.RoomMessage{UserID: "u3", Text: "msg3", Timestamp: t0.Add(2 * time.Second)})
	buf.Push(domain.RoomMessage{UserID: "u4", Text: "msg4", Timestamp: t0.Add(3 * time.Second)})
	buf.Push(domain.RoomMessage{UserID: "u5", Text: "msg5", Timestamp: t0.Add(4 * time.Second)})

	recent := buf.Recent(0)
	if len(recent) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(recent))
	}
	if recent[0].Text != "msg3" || recent[1].Text != "msg4" || recent[2].Text != "msg5" {
		t.Fatalf("expected [msg3, msg4, msg5], got: %+v", recent)
	}
}

func TestBuffer_Recent_FiltersExpiredWithoutPush(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 10, 5*time.Minute)

	buf.Push(domain.RoomMessage{UserID: "u1", Text: "msg1", Timestamp: t0})
	clock.Advance(2 * time.Minute)
	buf.Push(domain.RoomMessage{UserID: "u2", Text: "msg2", Timestamp: clock.Now()})

	// Advance clock so msg1 is 6m old (>5m), but msg2 is 4m old (<5m)
	clock.Advance(4 * time.Minute)

	recent := buf.Recent(0)
	if len(recent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(recent))
	}
	if recent[0].Text != "msg2" {
		t.Fatalf("expected 'msg2', got '%s'", recent[0].Text)
	}
}

func TestBuffer_Recent_Limit(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(t0)

	buf := room.NewBuffer(clock, 10, 10*time.Minute)

	for i := 1; i <= 5; i++ {
		buf.Push(domain.RoomMessage{UserID: "u", Text: string(rune('0' + i)), Timestamp: t0.Add(time.Duration(i) * time.Second)})
	}

	// Limit 2 should return last 2
	last2 := buf.Recent(2)
	if len(last2) != 2 || last2[0].Text != "4" || last2[1].Text != "5" {
		t.Fatalf("expected [4, 5], got %+v", last2)
	}

	// Limit greater than total should return all
	all := buf.Recent(10)
	if len(all) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(all))
	}

	// Limit <= 0 should return all
	allZero := buf.Recent(0)
	if len(allZero) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(allZero))
	}

	allNeg := buf.Recent(-1)
	if len(allNeg) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(allNeg))
	}
}

func TestBuffer_Clear(t *testing.T) {
	buf := room.NewBuffer(nil, 10, 10*time.Minute)
	buf.Push(domain.RoomMessage{UserID: "u1", Text: "hello"})
	buf.Clear()

	recent := buf.Recent(0)
	if len(recent) != 0 {
		t.Fatalf("expected 0 messages after Clear, got %d", len(recent))
	}
}

func TestBuffer_Immutability(t *testing.T) {
	buf := room.NewBuffer(nil, 10, 10*time.Minute)
	buf.Push(domain.RoomMessage{UserID: "u1", Text: "original"})

	msgs := buf.Recent(1)
	msgs[0].Text = "modified"

	msgsAfter := buf.Recent(1)
	if msgsAfter[0].Text != "original" {
		t.Fatalf("expected buffer to be immutable from caller mutations, got '%s'", msgsAfter[0].Text)
	}
}

func TestBuffer_Concurrency(t *testing.T) {
	clock := domain.NewFakeClock(time.Now())
	buf := room.NewBuffer(clock, 20, 5*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf.Push(domain.RoomMessage{UserID: "u", Text: "msg"})
			_ = buf.Recent(5)
			if id%15 == 0 {
				buf.Clear()
			}
		}(i)
	}
	wg.Wait()
}
