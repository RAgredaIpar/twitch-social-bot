package llm_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/llm"
)

func TestDetectRepeatedTerms_CatchesEdgeFillersAndBodyLoops(t *testing.T) {
	turns := []domain.ConversationTurn{
		{UserMessage: "hola", BotReply: "tu pe qué? háblame claro xd"},
		{UserMessage: "pq te molestas", BotReply: "no me molesto, pido contexto xddd"},
		{UserMessage: "perdon", BotReply: "perdonado pero anotado en la lista"},
	}
	got := llm.DetectRepeatedTerms(turns)
	if !slices.Contains(got, "xd") {
		t.Fatalf("expected 'xd' flagged as repeated edge filler, got %v", got)
	}

	// Single-char trailing slang filler like "p" repeated at end of >= 2 replies must also be caught.
	pLoop := []domain.ConversationTurn{
		{UserMessage: "1", BotReply: "habla claro p"},
		{UserMessage: "2", BotReply: "ni a palo p"},
	}
	if gotP := llm.DetectRepeatedTerms(pLoop); !slices.Contains(gotP, "p") {
		t.Fatalf("expected trailing 'p' flagged on 2-turn loop, got %v", gotP)
	}

	// Single natural occurrence of "xd" or "p" must NOT be flagged.
	single := []domain.ConversationTurn{
		{UserMessage: "hola", BotReply: "qué onda, todo tranqui p"},
		{UserMessage: "fallé", BotReply: "con esa puntería cero sorpresa xd"},
	}
	gotSingle := llm.DetectRepeatedTerms(single)
	if slices.Contains(gotSingle, "xd") || slices.Contains(gotSingle, "p") {
		t.Fatalf("did not expect single 'xd' or 'p' to be flagged, got %v", gotSingle)
	}

	// Body word repeated across >= 3 replies is flagged regardless of what word it is.
	bodyLoop := []domain.ConversationTurn{
		{UserMessage: "1", BotReply: "eso es puro humo hermano"},
		{UserMessage: "2", BotReply: "deja de vender humo aquí"},
		{UserMessage: "3", BotReply: "otra vez con el humo hoy"},
	}
	if gotBody := llm.DetectRepeatedTerms(bodyLoop); !slices.Contains(gotBody, "humo") {
		t.Fatalf("expected 'humo' flagged on 3-turn body loop, got %v", gotBody)
	}
}

func TestBuildUserPrompt_DynamicAntiRepeatAndBroadcasterChannelName(t *testing.T) {
	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "es")
	if err != nil {
		t.Fatal(err)
	}
	prompt := llm.BuildUserPromptWithLocale(domain.PromptInput{
		BroadcasterName: "Rhfjfgzrdg",
		ViewerRole:      "broadcaster",
		ViewerMessage:   "qué opinas de esta partida",
		Profile: domain.ViewerProfile{
			Username:    "rhfjfgzrdg",
			DisplayName: "Rhfjfgzrdg",
			Affinity:    0,
			RecentTurns: []domain.ConversationTurn{
				{UserMessage: "a", BotReply: "buena esa xd"},
				{UserMessage: "b", BotReply: "ni de broma xd"},
			},
		},
		CurrentTier: domain.TierSpec{ID: "desconocido", Name: "Desconocido", Behavior: "Distante"},
	}, loc)

	if strings.Contains(strings.ToLower(prompt), "broadcaster (dueño del canal)") {
		t.Fatalf("prompt must not label broadcaster as 'Broadcaster (Dueño del canal)':\n%s", prompt)
	}
	if !strings.Contains(prompt, "Streamer (Rhfjfgzrdg)") {
		t.Fatalf("expected channel name in broadcaster role, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "- Streamer: Rhfjfgzrdg") {
		t.Fatalf("expected channel name in stream context, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[VARIEDAD LÉXICA]") || !strings.Contains(prompt, `"xd"`) {
		t.Fatalf("expected dynamic lexical variety directive for 'xd', got:\n%s", prompt)
	}
}

func TestDetectRepeatedTermsForTurn_FlagsUnprompted2TurnTopicLoop(t *testing.T) {
	turns := []domain.ConversationTurn{
		{UserMessage: "tirate un chiste", BotReply: "tu yasuo, ese es el chiste"},
		{UserMessage: "mi comida favorita es el ceviche", BotReply: "ceviche anotado, pero no te salva del yasuo"},
	}

	// Viewer talks about mufa (does NOT mention yasuo) -> "yasuo" repeated in last 2 replies must be flagged!
	flagged := llm.DetectRepeatedTermsForTurn(turns, "anulo mufa para la siguiente partida")
	if !slices.Contains(flagged, "yasuo") {
		t.Fatalf("expected unprompted 2-turn 'yasuo' topic to be flagged, got %v", flagged)
	}

	// If viewer explicitly asks about yasuo, do NOT block "yasuo".
	notFlagged := llm.DetectRepeatedTermsForTurn(turns, "por qué odias tanto a mi yasuo")
	if slices.Contains(notFlagged, "yasuo") {
		t.Fatalf("did not expect 'yasuo' flagged when viewer explicitly asks about it, got %v", notFlagged)
	}
}
