package llm_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/llm"
)

func TestHumanizePrompts_MinimalMoodRoleAndStreamContext(t *testing.T) {
	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "es")
	if err != nil {
		t.Fatal(err)
	}

	spec := &domain.PersonaSpec{
		ID:          "cohost_amigos",
		BotName:     "Mochi",
		BasePersona: "Co-host sarcástica",
	}

	sysPrompt := llm.BuildSystemPromptWithLocale(spec, loc)
	if !strings.Contains(sysPrompt, "PROHIBIDO sonar a asistente de IA") {
		t.Fatalf("expected anti-AI rule in system prompt, got:\n%s", sysPrompt)
	}

	userPrompt := llm.BuildUserPromptWithLocale(domain.PromptInput{
		Persona: spec,
		Profile: domain.ViewerProfile{
			UserID:      "u1",
			Username:    "rhhhh_",
			DisplayName: "Rhhhh_",
			Affinity:    50,
		},
		CurrentTier:   domain.TierSpec{ID: "pana", Name: "Pana", Behavior: "Amistoso"},
		ViewerMessage: "xd",
		ViewerRole:    "broadcaster",
		CurrentMood:   "annoyed",
		StreamGame:    "Valorant",
		StreamTitle:   "Rankeds de noche",
		RecentRoom: []domain.RoomMessage{
			{UserID: "u2", DisplayName: "Otro", Text: "mira esa jugada"},
		},
	}, loc)

	checks := []string{
		"[CONTEXTO_STREAM]",
		"Game/Category: Valorant",
		"Title: Rankeds de noche",
		"Current Mood: picado y más cortante",
		"Role: Broadcaster (Dueño del canal)",
		"1 a 4 palabras máximo",
		"[HILO_RECIENTE_SALA]",
	}
	for _, want := range checks {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("expected %q in user prompt, got:\n%s", want, userPrompt)
		}
	}
}
