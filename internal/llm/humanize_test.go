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
	if !strings.Contains(sysPrompt, "SOUL.md") || !strings.Contains(sysPrompt, "Jamás uses relleno de asistente") || !strings.Contains(sysPrompt, "*asteriscos*") {
		t.Fatalf("expected SOUL.md parent, anti-AI, and anti-asterisk boundaries in system prompt, got:\n%s", sysPrompt)
	}
	if strings.Contains(sysPrompt, "PROHIBIDO sonar a asistente de IA") {
		t.Fatalf("expected style/anti-AI rules not to be duplicated in hard security rules block, got:\n%s", sysPrompt)
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
		"Role: Streamer (Rhhhh_)",
		"1 a 4 palabras máximo",
		"[HILO_RECIENTE_SALA]",
	}
	for _, want := range checks {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("expected %q in user prompt, got:\n%s", want, userPrompt)
		}
	}
}

func TestElizaOSPromptAndVectorFactSelection(t *testing.T) {
	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "es")
	if err != nil {
		t.Fatal(err)
	}

	organicSpec := &domain.PersonaSpec{
		ID:         "cipher",
		BotName:    "Cipher",
		Bio:        []string{"Experto cínico de sistemas"},
		Lore:       []string{"Desconfía de los discos locales"},
		Adjectives: []string{"directo", "seco"},
		Topics:     []string{"ciberseguridad"},
		Style:      []string{"Usa minúsculas"},
		MessageExamples: []domain.MessageExample{
			{User: "hola", Reply: "al grano, qué pasó"},
		},
	}

	sysPrompt := llm.BuildSystemPromptWithLocale(organicSpec, loc)
	for _, want := range []string{"Biografía:", "[LORE Y TRASFONDO]", "[ESTILO DE ESCRITURA DEL PERSONAJE]", "[EJEMPLOS REALES DE DIÁLOGO]", "Cipher: al grano, qué pasó"} {
		if !strings.Contains(sysPrompt, want) {
			t.Fatalf("expected %q in system prompt, got:\n%s", want, sysPrompt)
		}
	}
	if strings.Contains(sysPrompt, "[NIVELES DE AFINIDAD]") || strings.Contains(sysPrompt, "[PUESTOS SOCIALES Y TÍTULOS]") {
		t.Fatalf("expected tiers and slots omitted for organic persona, got:\n%s", sysPrompt)
	}

	facts := []string{"Tiene un gato llamado Michi"}
	for i := 0; i < 20; i++ {
		facts = append(facts, "Le gusta el café negro por las mañanas")
	}
	facts = append(facts, "Llegó tarde hoy al directo")

	userPrompt := llm.BuildUserPromptWithLocale(domain.PromptInput{
		Persona:       organicSpec,
		Profile:       domain.ViewerProfile{UserID: "u1", Username: "u1", DisplayName: "U1", Facts: facts},
		CurrentTier:   domain.TierSpec{ID: "neutral", Name: "Neutral", Behavior: "Natural"},
		ViewerMessage: "mi gatito está durmiendo",
	}, loc)

	if !strings.Contains(userPrompt, "Tiene un gato llamado Michi") {
		t.Fatalf("expected vector-selected cat fact in user prompt, got:\n%s", userPrompt)
	}
	if !strings.Contains(userPrompt, "Llegó tarde hoy al directo") {
		t.Fatalf("expected recent fact in user prompt, got:\n%s", userPrompt)
	}

	// Verify SFW toggle and unfiltered profanity directive from SOUL
	sfwPrompt := llm.BuildSystemPromptWithOptions(organicSpec, loc, false)
	if !strings.Contains(sfwPrompt, "[MODO SEGURO +18]") {
		t.Fatalf("expected [MODO SEGURO +18] when allowNSFW=false, got:\n%s", sfwPrompt)
	}
	if !strings.Contains(sfwPrompt, "groserías") {
		t.Fatalf("expected profanity permission in SOUL.es.md, got:\n%s", sfwPrompt)
	}
	nsfwPrompt := llm.BuildSystemPromptWithOptions(organicSpec, loc, true)
	if strings.Contains(nsfwPrompt, "[MODO SEGURO +18]") {
		t.Fatalf("expected [MODO SEGURO +18] omitted when allowNSFW=true")
	}
}

func TestBuildUserPrompt_PersonaAttributedTurnsAndSwitchGuard(t *testing.T) {
	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "es")
	if err != nil {
		t.Fatal(err)
	}
	mochi := &domain.PersonaSpec{ID: "cohost_amigos", BotName: "Mochi"}
	prompt := llm.BuildUserPromptWithLocale(domain.PromptInput{
		Persona:       mochi,
		ViewerMessage: "ya volví contigo",
		Profile: domain.ViewerProfile{
			Username:    "viewer1",
			DisplayName: "Viewer1",
			RecentTurns: []domain.ConversationTurn{
				{UserMessage: "hola dante", BotReply: "habla con respeto en esta mesa", BotName: "Dante", PersonaID: "clan_jerarquia"},
				{UserMessage: "antiguo", BotReply: "turno legado sin nombre"},
			},
		},
	}, loc)

	if !strings.Contains(prompt, "Dante: habla con respeto en esta mesa") {
		t.Fatalf("expected speaker label 'Dante:' in recent turns, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Bot: turno legado sin nombre") {
		t.Fatalf("expected legacy empty BotName to fall back to 'Bot:', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[CAMBIO DE PERSONAJE]") || !strings.Contains(prompt, "Mochi") {
		t.Fatalf("expected persona switch style guard for Mochi, got:\n%s", prompt)
	}
}
