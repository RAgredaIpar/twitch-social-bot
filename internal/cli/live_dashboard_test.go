package cli_test

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/cli"
	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/engine"
	"github.com/twitch-social-bot/internal/i18n"
)

type staticPersonaProvider struct {
	spec *domain.PersonaSpec
}

func (p *staticPersonaProvider) Current() *domain.PersonaSpec          { return p.spec }
func (p *staticPersonaProvider) Switch(string) (*domain.PersonaSpec, error) { return p.spec, nil }
func (p *staticPersonaProvider) List() []string                      { return []string{p.spec.ID} }

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestLogBuffer_RingEvictionAndLines(t *testing.T) {
	lb := cli.NewLogBuffer(3)
	for i := 1; i <= 5; i++ {
		_, _ = fmt.Fprintf(lb, "line %d\n", i)
	}
	got := lb.Lines(10)
	if len(got) != 3 {
		t.Fatalf("expected 3 buffered lines, got %d: %v", len(got), got)
	}
	if got[0] != "line 3" || got[2] != "line 5" {
		t.Fatalf("unexpected ring buffer contents: %v", got)
	}
}

func TestLiveDashboard_StatesLogsToggleAndBoxAlignment(t *testing.T) {
	loc, err := i18n.NewManager("", "es")
	if err != nil {
		t.Fatal(err)
	}
	lb := cli.NewLogBuffer(50)
	_, _ = fmt.Fprintln(lb, "2026/11/24 12:00:00 ¡Suscripción a chat confirmada!")

	var ready atomic.Bool
	ready.Store(true)

	mochi := &domain.PersonaSpec{
		ID:         "cohost_amigos",
		Name:       "Co-Host Amigos",
		BotName:    "Mochi",
		ThemeColor: "#34D399",
	}

	dash := cli.NewLiveDashboard(cli.LiveDashboardConfig{
		Version:       "v2.3.0",
		Broadcaster:   "rhfjfgzrdg",
		HasTwitch:     true,
		Persona:       &staticPersonaProvider{spec: mochi},
		Logs:          lb,
		Locale:        loc,
		EventSubReady: &ready,
		StartTime:     time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
	})

	now := time.Date(2026, 11, 24, 12, 0, 15, 0, time.UTC)

	// 1. IDLE state & verify every box line closes cleanly at same column.
	idleFrame := stripANSI(dash.RenderFrame(now))
	if !strings.Contains(idleFrame, "[  IDLE  ]") || !strings.Contains(idleFrame, "EventSub OK") {
		t.Fatalf("expected IDLE badge and EventSub OK, got:\n%s", idleFrame)
	}
	for _, line := range strings.Split(idleFrame, "\n") {
		if strings.HasPrefix(line, "  │") || strings.HasPrefix(line, "  ╭") || strings.HasPrefix(line, "  ╰") || strings.HasPrefix(line, "  ├") {
			w := len([]rune(line))
			if w != 78 {
				t.Fatalf("expected aligned box width 78, got %d on line %q", w, line)
			}
		}
	}

	// 2. THINKING state.
	dash.ApplyEvent(engine.ActivityEvent{
		Kind:        engine.ActivityThinking,
		Username:    "Rhfjfgzrdg",
		UserMessage: "!bot hola mochi",
		Persona:     mochi,
	})
	thinkFrame := stripANSI(dash.RenderFrame(now))
	if !strings.Contains(thinkFrame, "[ THINK  ]") || !strings.Contains(thinkFrame, "@Rhfjfgzrdg: !bot hola mochi") {
		t.Fatalf("expected THINK state and user message, got:\n%s", thinkFrame)
	}

	// 3. SPEAKING state (ON AIR).
	dash.ApplyEvent(engine.ActivityEvent{
		Kind:        engine.ActivityReplied,
		Username:    "Rhfjfgzrdg",
		UserMessage: "!bot hola mochi",
		BotReply:    "qué onda p, listo para cotorrear",
		Persona:     mochi,
	})
	speakFrame := stripANSI(dash.RenderFrame(now))
	if !strings.Contains(speakFrame, "[ ON AIR ]") || !strings.Contains(speakFrame, "qué onda p, listo para cotorrear") {
		t.Fatalf("expected ON AIR ring and reply text, got:\n%s", speakFrame)
	}

	// 4. Toggle to LOGS view.
	dash.ToggleLogs()
	logsFrame := stripANSI(dash.RenderFrame(now))
	if !strings.Contains(logsFrame, "[LOGS] MODO DEPURACION EN VIVO") || !strings.Contains(logsFrame, "Suscripción a chat confirmada") {
		t.Fatalf("expected live logs frame with buffered log entry, got:\n%s", logsFrame)
	}
	for _, line := range strings.Split(logsFrame, "\n") {
		if strings.HasPrefix(line, "  │") || strings.HasPrefix(line, "  ╭") || strings.HasPrefix(line, "  ╰") {
			w := len([]rune(line))
			if w != 78 {
				t.Fatalf("expected aligned logs box width 78, got %d on line %q", w, line)
			}
		}
	}
}
