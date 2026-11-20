package engine_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/engine"
	"github.com/twitch-social-bot/internal/ratelimit"
	"github.com/twitch-social-bot/internal/room"
	"github.com/twitch-social-bot/internal/safety"
	"github.com/twitch-social-bot/internal/social"
	"github.com/twitch-social-bot/internal/store"
)

// mockLLMClient implements domain.LLMClient for testing.
type mockLLMClient struct {
	mu        sync.Mutex
	lastInput domain.PromptInput
	decision  domain.LLMDecision
	err       error
	callCount int
}

func (m *mockLLMClient) Generate(ctx context.Context, input domain.PromptInput) (domain.LLMDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	m.lastInput = input
	if m.err != nil {
		return domain.LLMDecision{}, m.err
	}
	return m.decision, nil
}

// mockChatSender implements domain.ChatSender for testing.
type mockChatSender struct {
	mu           sync.Mutex
	replies      []sentReply
	sendReplyErr error
}

type sentReply struct {
	BroadcasterID string
	SenderID      string
	ReplyParentID string
	Text          string
}

func (m *mockChatSender) SendReply(ctx context.Context, broadcasterID, senderID, replyParentID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendReplyErr != nil {
		return m.sendReplyErr
	}
	m.replies = append(m.replies, sentReply{
		BroadcasterID: broadcasterID,
		SenderID:      senderID,
		ReplyParentID: replyParentID,
		Text:          text,
	})
	return nil
}

func (m *mockChatSender) LastReply() (sentReply, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.replies) == 0 {
		return sentReply{}, false
	}
	return m.replies[len(m.replies)-1], true
}

// mockPersonaProvider implements domain.PersonaProvider for testing.
type mockPersonaProvider struct {
	mu       sync.RWMutex
	current  *domain.PersonaSpec
	personas map[string]*domain.PersonaSpec
}

func newMockPersonaProvider(initial *domain.PersonaSpec) *mockPersonaProvider {
	m := &mockPersonaProvider{
		current:  initial,
		personas: make(map[string]*domain.PersonaSpec),
	}
	if initial != nil {
		m.personas[initial.ID] = initial
	}
	return m
}

func (m *mockPersonaProvider) Current() *domain.PersonaSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *mockPersonaProvider) Switch(idOrFile string) (*domain.PersonaSpec, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.personas[idOrFile]
	if !ok {
		return nil, errors.New("persona not found")
	}
	m.current = p
	return p, nil
}

func (m *mockPersonaProvider) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []string
	for id := range m.personas {
		list = append(list, id)
	}
	return list
}

func samplePersona() *domain.PersonaSpec {
	minAff := 30
	return &domain.PersonaSpec{
		ID:               "cohost_amigos",
		Name:             "Luna",
		BotName:          "LunaBot",
		TriggerPrefix:    "!bot",
		MaxPositiveDelta: 5,
		MaxNegativeDelta: -5,
		BasePersona:      "Una co-host amigable y divertida.",
		BlockedWords:     []string{"groseria", "spam"},
		Tiers: []domain.TierSpec{
			{ID: "hostil", Name: "Hostil", Min: -100, Max: -20, Behavior: "Distante y fría"},
			{ID: "neutral", Name: "Neutral", Min: -19, Max: 20, Behavior: "Amable y educada"},
			{ID: "amigo", Name: "Amigo", Min: 21, Max: 60, Behavior: "Cálida y bromista"},
			{ID: "confidente", Name: "Confidente", Min: 61, Max: 100, Behavior: "Súper leal y cómplice"},
		},
		Slots: []domain.SlotSpec{
			{
				ID:          "bff",
				Title:       "Mejor Amigo",
				MaxHolders:  1,
				MinAffinity: &minAff,
			},
		},
	}
}

func setupTestEnvironment(t *testing.T) (*engine.Orchestrator, *mockLLMClient, *mockChatSender, *mockPersonaProvider, domain.Store, *domain.FakeClock) {
	t.Helper()

	fakeClock := domain.NewFakeClock(time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC))
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	personaSpec := samplePersona()
	personaProv := newMockPersonaProvider(personaSpec)

	llmCli := &mockLLMClient{
		decision: domain.LLMDecision{
			Reply:         "¡Hola! Qué gusto verte en el chat.",
			AffinityDelta: 2,
		},
	}
	sender := &mockChatSender{}
	limiter := ratelimit.NewCooldownLimiter(fakeClock, 3*time.Second, 15*time.Second)
	roomBuf := room.NewBuffer(fakeClock, 25, 10*time.Minute)
	socEngine := social.NewEngine(fakeClock)
	guard := safety.NewGuard(400, 480)

	cfg := engine.OrchestratorConfig{
		BotUserID: "bot_123",
		BotLogin:  "lunabot",
		Clock:     fakeClock,
		Store:     st,
		LLM:       llmCli,
		Sender:    sender,
		Persona:   personaProv,
		Limiter:   limiter,
		Room:      roomBuf,
		Social:    socEngine,
		Guard:     guard,
	}

	orch := engine.NewOrchestrator(cfg)
	return orch, llmCli, sender, personaProv, st, fakeClock
}

func TestHandleMessage_IgnoreSelfAndEmpty(t *testing.T) {
	orch, llmCli, sender, _, _, _ := setupTestEnvironment(t)
	ctx := context.Background()

	// 1. Empty message
	err := orch.HandleMessage(ctx, domain.ChatMessage{
		UserID:   "user_1",
		Username: "viewer1",
		Text:     "   ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2. Self message by bot UserID
	err = orch.HandleMessage(ctx, domain.ChatMessage{
		UserID:   "bot_123",
		Username: "different_name",
		Text:     "!bot hola",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3. Self message by bot Login (case-insensitive)
	err = orch.HandleMessage(ctx, domain.ChatMessage{
		UserID:   "some_id",
		Username: "LUNABOT",
		Text:     "!bot hola",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if llmCli.callCount != 0 {
		t.Errorf("expected 0 LLM calls, got %d", llmCli.callCount)
	}
	if len(sender.replies) != 0 {
		t.Errorf("expected 0 replies sent, got %d", len(sender.replies))
	}
}

func TestHandleMessage_NonTriggered_PushedToRoom(t *testing.T) {
	orch, llmCli, sender, _, _, _ := setupTestEnvironment(t)
	ctx := context.Background()

	err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:       "msg_1",
		BroadcasterID:   "streamer_1",
		BroadcasterName: "streamer",
		UserID:          "viewer_99",
		Username:        "viewer99",
		DisplayName:     "Viewer99",
		Text:            "buenas tardes a todos los presentes",
		Timestamp:       time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if llmCli.callCount != 0 {
		t.Errorf("expected 0 LLM calls for room message, got %d", llmCli.callCount)
	}
	if len(sender.replies) != 0 {
		t.Errorf("expected 0 replies sent, got %d", len(sender.replies))
	}

	snap := orch.StatusSnapshot(ctx)
	if snap.ProcessedMessages != 1 {
		t.Errorf("expected ProcessedMessages == 1, got %d", snap.ProcessedMessages)
	}
	if snap.GeneratedReplies != 0 {
		t.Errorf("expected GeneratedReplies == 0, got %d", snap.GeneratedReplies)
	}
}

func TestHandleMessage_TriggerPrefixVariations(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		text        string
		shouldReply bool
	}{
		{"prefix with space", "!bot hola", true},
		{"prefix alone (empty payload)", "!bot", false},
		{"prefix case-insensitive", "!BOT cómo estás?", true},
		{"at bot name with space", "@LunaBot hola amiga", true},
		{"at bot name alone (empty payload)", "@LunaBot", false},
		{"at bot login case-insensitive", "@lunabot qué tal", true},
		{"not trigger prefix substring", "!bottles are cool", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch, llmCli, sender, _, _, fakeClock := setupTestEnvironment(t)
			fakeClock.Advance(20 * time.Second) // bypass cooldown

			err := orch.HandleMessage(ctx, domain.ChatMessage{
				MessageID:     "msg_test",
				BroadcasterID: "streamer_1",
				UserID:        "user_abc",
				Username:      "user_abc",
				DisplayName:   "UserAbc",
				Text:          tc.text,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.shouldReply {
				if llmCli.callCount != 1 {
					t.Errorf("expected 1 LLM call, got %d", llmCli.callCount)
				}
				if len(sender.replies) != 1 {
					t.Errorf("expected 1 reply sent, got %d", len(sender.replies))
				}
			} else {
				if llmCli.callCount != 0 {
					t.Errorf("expected 0 LLM calls, got %d", llmCli.callCount)
				}
				if len(sender.replies) != 0 {
					t.Errorf("expected 0 replies, got %d", len(sender.replies))
				}
			}
		})
	}
}

func TestHandleMessage_AdminCommands(t *testing.T) {
	ctx := context.Background()

	t.Run("pause and resume", func(t *testing.T) {
		orch, _, sender, _, _, _ := setupTestEnvironment(t)

		// 1. Pause by moderator
		err := orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:   "m1",
			UserID:      "mod_1",
			Username:    "moderator",
			IsModerator: true,
			Text:        "!bot pause",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !orch.IsPaused() {
			t.Errorf("expected orchestrator to be paused")
		}
		reply, ok := sender.LastReply()
		if !ok || !strings.Contains(strings.ToLower(reply.Text), "pausad") {
			t.Errorf("expected pause confirmation reply, got %q", reply.Text)
		}

		// 2. Normal viewer message while paused should be ignored
		err = orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID: "m2",
			UserID:    "viewer_1",
			Username:  "viewer1",
			Text:      "!bot hola Luna",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// replies count should still be 1 (the pause confirmation)
		if len(sender.replies) != 1 {
			t.Errorf("expected 1 total reply, got %d", len(sender.replies))
		}

		// 3. Resume by broadcaster
		err = orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:     "m3",
			UserID:        "streamer_1",
			Username:      "streamer",
			IsBroadcaster: true,
			Text:          "!bot resume",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if orch.IsPaused() {
			t.Errorf("expected orchestrator to be resumed")
		}
		reply, ok = sender.LastReply()
		if !ok || !strings.Contains(strings.ToLower(reply.Text), "reanud") {
			t.Errorf("expected resume confirmation reply, got %q", reply.Text)
		}
	})

	t.Run("non-moderator cannot execute admin commands", func(t *testing.T) {
		orch, llmCli, sender, _, _, _ := setupTestEnvironment(t)

		err := orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:     "m_unauth",
			UserID:        "viewer_2",
			Username:      "viewer2",
			IsModerator:   false,
			IsBroadcaster: false,
			Text:          "!bot pause",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if orch.IsPaused() {
			t.Errorf("unauthorized user should not pause bot")
		}
		if len(sender.replies) != 0 {
			t.Errorf("unauthorized admin command should be ignored silently, got %d replies", len(sender.replies))
		}
		if llmCli.callCount != 0 {
			t.Errorf("LLM should not be called for unauthorized admin command, got %d calls", llmCli.callCount)
		}
	})

	t.Run("status command", func(t *testing.T) {
		orch, _, sender, _, _, _ := setupTestEnvironment(t)

		err := orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:   "m_status",
			UserID:      "mod_1",
			Username:    "moderator",
			IsModerator: true,
			Text:        "!bot status",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		reply, ok := sender.LastReply()
		if !ok {
			t.Fatalf("expected reply for status command")
		}
		if !strings.Contains(reply.Text, "Luna") || !strings.Contains(reply.Text, "activo") {
			t.Errorf("expected status reply with persona and state, got: %s", reply.Text)
		}
	})

	t.Run("persona switch command", func(t *testing.T) {
		orch, _, sender, personaProv, _, _ := setupTestEnvironment(t)

		// Add second persona to mock provider
		personaProv.personas["dark_persona"] = &domain.PersonaSpec{
			ID:            "dark_persona",
			Name:          "Sombra",
			BotName:       "SombraBot",
			TriggerPrefix: "!sombra",
			BasePersona:   "Una sombra misteriosa.",
			Tiers: []domain.TierSpec{
				{ID: "neutral", Name: "Neutral", Min: -100, Max: 100},
			},
		}

		err := orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:   "m_switch",
			UserID:      "mod_1",
			Username:    "moderator",
			IsModerator: true,
			Text:        "!bot persona dark_persona",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		snap := orch.StatusSnapshot(ctx)
		if snap.ActivePersonaID != "dark_persona" {
			t.Errorf("expected active persona dark_persona, got %s", snap.ActivePersonaID)
		}

		reply, ok := sender.LastReply()
		if !ok || !strings.Contains(reply.Text, "Sombra") {
			t.Errorf("expected switch confirmation with Sombra, got: %q", reply.Text)
		}
	})

	t.Run("set affinity and reset user", func(t *testing.T) {
		orch, _, sender, _, st, _ := setupTestEnvironment(t)

		// 1. Set affinity for @carlos to 50
		err := orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:   "m_set",
			UserID:      "mod_1",
			Username:    "moderator",
			IsModerator: true,
			Text:        "!bot set @carlos 50",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		profile, found, err := st.FindProfileByUsername(ctx, "carlos")
		if err != nil || !found {
			t.Fatalf("expected profile for carlos to be created and found, found=%v err=%v", found, err)
		}
		if profile.Affinity != 50 {
			t.Errorf("expected affinity 50, got %d", profile.Affinity)
		}
		if profile.TierID != "amigo" {
			t.Errorf("expected tier amigo, got %s", profile.TierID)
		}

		reply, ok := sender.LastReply()
		if !ok || !strings.Contains(reply.Text, "50") {
			t.Errorf("expected reply confirming affinity 50, got: %q", reply.Text)
		}

		// 2. Reset user @carlos
		err = orch.HandleMessage(ctx, domain.ChatMessage{
			MessageID:   "m_reset",
			UserID:      "mod_1",
			Username:    "moderator",
			IsModerator: true,
			Text:        "!bot reset @carlos",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, found, err = st.FindProfileByUsername(ctx, "carlos")
		if err != nil {
			t.Fatalf("unexpected error finding profile: %v", err)
		}
		if found {
			t.Errorf("expected profile for carlos to be deleted after reset")
		}

		reply, ok = sender.LastReply()
		if !ok || !strings.Contains(strings.ToLower(reply.Text), "restablecid") {
			t.Errorf("expected reset confirmation, got: %q", reply.Text)
		}
	})
}

func TestHandleMessage_ConversationalFlow_EndToEnd(t *testing.T) {
	orch, llmCli, sender, _, st, fakeClock := setupTestEnvironment(t)
	ctx := context.Background()

	llmCli.decision = domain.LLMDecision{
		Reply:         "¡Hola Ana! Me caes súper bien.",
		AffinityDelta: 3,
		NewFact:       "Le gustan los videojuegos retro",
		SlotClaim:     "bff",
	}

	err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:       "msg_ana_1",
		BroadcasterID:   "streamer_1",
		BroadcasterName: "streamer",
		UserID:          "viewer_ana",
		Username:        "ana_gamer",
		DisplayName:     "AnaGamer",
		Text:            "!bot hola Luna, me encantan los juegos retro",
		Timestamp:       fakeClock.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Verify LLM received proper PromptInput
	if llmCli.callCount != 1 {
		t.Fatalf("expected 1 LLM call, got %d", llmCli.callCount)
	}
	if llmCli.lastInput.ViewerMessage != "hola Luna, me encantan los juegos retro" {
		t.Errorf("expected cleaned viewer message, got: %q", llmCli.lastInput.ViewerMessage)
	}
	if llmCli.lastInput.Profile.UserID != "viewer_ana" {
		t.Errorf("expected profile UserID viewer_ana, got: %s", llmCli.lastInput.Profile.UserID)
	}

	// 2. Verify Reply was sent with parent message ID
	lastReply, ok := sender.LastReply()
	if !ok {
		t.Fatalf("expected reply sent to chat")
	}
	if lastReply.ReplyParentID != "msg_ana_1" {
		t.Errorf("expected ReplyParentID msg_ana_1, got: %s", lastReply.ReplyParentID)
	}
	if lastReply.Text != "¡Hola Ana! Me caes súper bien." {
		t.Errorf("unexpected reply text: %s", lastReply.Text)
	}

	// 3. Verify state persistence in store
	profile, found, err := st.GetProfile(ctx, "viewer_ana")
	if err != nil || !found {
		t.Fatalf("profile not found in store: %v", err)
	}
	if profile.Affinity != 3 {
		t.Errorf("expected affinity 3, got %d", profile.Affinity)
	}
	if len(profile.Facts) != 1 || profile.Facts[0] != "Le gustan los videojuegos retro" {
		t.Errorf("expected fact saved, got: %v", profile.Facts)
	}

	// 4. Verify slots state: Ana had affinity 3, but bff requires min 30, so slot should NOT be claimed
	slots, err := st.GetSlots(ctx, "cohost_amigos")
	if err != nil {
		t.Fatalf("error getting slots: %v", err)
	}
	if len(slots["bff"]) != 0 {
		t.Errorf("expected bff slot empty because affinity 3 < 30, got: %v", slots["bff"])
	}

	// 5. Verify Cooldown enforcement
	// Immediately sending another message with same user should hit user cooldown (15s)
	err = orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_ana_2",
		BroadcasterID: "streamer_1",
		UserID:        "viewer_ana",
		Username:      "ana_gamer",
		DisplayName:   "AnaGamer",
		Text:          "!bot otra vez yo",
		Timestamp:     fakeClock.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llmCli.callCount != 1 {
		t.Errorf("expected cooldown to prevent 2nd LLM call, got %d", llmCli.callCount)
	}

	// Advance clock past cooldown (16 seconds)
	fakeClock.Advance(16 * time.Second)

	// Now message should be processed
	err = orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_ana_3",
		BroadcasterID: "streamer_1",
		UserID:        "viewer_ana",
		Username:      "ana_gamer",
		DisplayName:   "AnaGamer",
		Text:          "!bot ahora sí pasó el tiempo",
		Timestamp:     fakeClock.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llmCli.callCount != 2 {
		t.Errorf("expected 2nd message after cooldown to call LLM, got %d calls", llmCli.callCount)
	}
}

func TestStatusSnapshot(t *testing.T) {
	orch, _, _, _, _, _ := setupTestEnvironment(t)
	ctx := context.Background()

	snap := orch.StatusSnapshot(ctx)
	if snap.ActivePersonaID != "cohost_amigos" {
		t.Errorf("expected ActivePersonaID cohost_amigos, got %s", snap.ActivePersonaID)
	}
	if snap.ActivePersonaName != "Luna" {
		t.Errorf("expected ActivePersonaName Luna, got %s", snap.ActivePersonaName)
	}
	if snap.BotName != "LunaBot" {
		t.Errorf("expected BotName LunaBot, got %s", snap.BotName)
	}
	if snap.TriggerPrefix != "!bot" {
		t.Errorf("expected TriggerPrefix !bot, got %s", snap.TriggerPrefix)
	}
	if snap.Paused {
		t.Errorf("expected initially not paused")
	}
}

func TestHandleMessage_SameAccountBroadcasterMode(t *testing.T) {
	fakeClock := domain.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	personaProv := newMockPersonaProvider(samplePersona())
	llmCli := &mockLLMClient{
		decision: domain.LLMDecision{
			Reply:         "@rhfjfgzrdg ¡Hola! Todo al 100 por acá.",
			AffinityDelta: 1,
		},
	}
	sender := &mockChatSender{}
	orch := engine.NewOrchestrator(engine.OrchestratorConfig{
		BotUserID: "677226599",
		BotLogin:  "rhfjfgzrdg",
		Clock:     fakeClock,
		Store:     st,
		LLM:       llmCli,
		Sender:    sender,
		Persona:   personaProv,
		Limiter:   ratelimit.NewCooldownLimiter(fakeClock, 3*time.Second, 15*time.Second),
		Room:      room.NewBuffer(fakeClock, 25, 10*time.Minute),
		Social:    social.NewEngine(fakeClock),
		Guard:     safety.NewGuard(400, 480),
	})

	ctx := context.Background()

	// 1. Streamer typing "!bot hola" from the same account as the bot should trigger a reply.
	if err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_1",
		BroadcasterID: "677226599",
		UserID:        "677226599",
		Username:      "rhfjfgzrdg",
		DisplayName:   "Rhfjfgzrdg",
		IsBroadcaster: true,
		Text:          "!bot hola",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llmCli.callCount != 1 {
		t.Fatalf("expected 1 LLM call in same-account mode, got %d", llmCli.callCount)
	}
	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply sent, got %d", len(sender.replies))
	}

	// 2. Echo of the bot's own reply (even if it starts with @BotLogin) must be ignored to prevent loops.
	fakeClock.Advance(20 * time.Second)
	if err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_echo",
		BroadcasterID: "677226599",
		UserID:        "677226599",
		Username:      "rhfjfgzrdg",
		DisplayName:   "Rhfjfgzrdg",
		IsBroadcaster: true,
		Text:          sender.replies[0].Text,
	}); err != nil {
		t.Fatalf("unexpected error on echo: %v", err)
	}
	if llmCli.callCount != 1 {
		t.Errorf("expected echo of bot reply to be ignored, got %d LLM calls", llmCli.callCount)
	}

	// 3. Untriggered streamer chat must not call LLM.
	if err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_chat",
		BroadcasterID: "677226599",
		UserID:        "677226599",
		Username:      "rhfjfgzrdg",
		DisplayName:   "Rhfjfgzrdg",
		IsBroadcaster: true,
		Text:          "hola chat, empezamos",
	}); err != nil {
		t.Fatalf("unexpected error on untriggered chat: %v", err)
	}
	if llmCli.callCount != 1 {
		t.Errorf("expected untriggered chat not to call LLM, got %d calls", llmCli.callCount)
	}

	// 4. Broadcaster admin command (!bot status) should work from the same account.
	if err := orch.HandleMessage(ctx, domain.ChatMessage{
		MessageID:     "msg_status",
		BroadcasterID: "677226599",
		UserID:        "677226599",
		Username:      "rhfjfgzrdg",
		DisplayName:   "Rhfjfgzrdg",
		IsBroadcaster: true,
		Text:          "!bot status",
	}); err != nil {
		t.Fatalf("unexpected error on status command: %v", err)
	}
	if len(sender.replies) != 2 {
		t.Errorf("expected status reply to be sent (total 2), got %d", len(sender.replies))
	}
	if !strings.Contains(strings.ToLower(sender.replies[1].Text), "luna") {
		t.Errorf("expected status reply to mention persona, got %q", sender.replies[1].Text)
	}
}
