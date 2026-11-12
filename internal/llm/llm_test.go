package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/llm"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	initialTime := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(initialTime)
	threshold := 3
	cooldown := 10 * time.Second

	cb := llm.NewCircuitBreaker(clock, threshold, cooldown)

	// 1. Initial State: Closed
	if state := cb.State(); state != llm.StateClosed {
		t.Fatalf("expected state %q, got %q", llm.StateClosed, state)
	}
	if !cb.Allow() {
		t.Fatalf("expected Allow() to be true in closed state")
	}

	// 2. Failures below threshold keep it Closed
	cb.RecordFailure()
	cb.RecordFailure()
	if state := cb.State(); state != llm.StateClosed {
		t.Fatalf("expected state %q after 2 failures, got %q", llm.StateClosed, state)
	}
	if !cb.Allow() {
		t.Fatalf("expected Allow() to be true before reaching failure threshold")
	}

	// 3. 3rd failure reaches threshold -> Transitions to Open
	cb.RecordFailure()
	if state := cb.State(); state != llm.StateOpen {
		t.Fatalf("expected state %q after %d failures, got %q", llm.StateOpen, threshold, state)
	}
	if cb.Allow() {
		t.Fatalf("expected Allow() to be false immediately after entering open state")
	}

	// 4. During cooldown, Allow remains false
	clock.Advance(5 * time.Second)
	if cb.Allow() {
		t.Fatalf("expected Allow() to be false during cooldown")
	}
	if state := cb.State(); state != llm.StateOpen {
		t.Fatalf("expected state %q during cooldown, got %q", llm.StateOpen, state)
	}

	// 5. Cooldown elapses: Allow transitions to Half-Open and permits single probe
	clock.Advance(5 * time.Second) // now 10s elapsed >= cooldown
	if !cb.Allow() {
		t.Fatalf("expected Allow() to return true for the probe request after cooldown")
	}
	if state := cb.State(); state != llm.StateHalfOpen {
		t.Fatalf("expected state %q when probing, got %q", llm.StateHalfOpen, state)
	}

	// 6. While probe is in flight, second Allow returns false
	if cb.Allow() {
		t.Fatalf("expected Allow() to return false while half-open probe is in flight")
	}

	// 7. Probe failure transitions back to Open immediately
	cb.RecordFailure()
	if state := cb.State(); state != llm.StateOpen {
		t.Fatalf("expected state %q after probe failure, got %q", llm.StateOpen, state)
	}
	if cb.Allow() {
		t.Fatalf("expected Allow() to return false after probe failure")
	}

	// 8. Wait cooldown again and probe succeeds -> transitions to Closed
	clock.Advance(cooldown)
	if !cb.Allow() {
		t.Fatalf("expected probe Allow() to succeed after second cooldown")
	}
	cb.RecordSuccess()
	if state := cb.State(); state != llm.StateClosed {
		t.Fatalf("expected state %q after successful probe, got %q", llm.StateClosed, state)
	}
	if !cb.Allow() {
		t.Fatalf("expected Allow() to return true after returning to closed state")
	}
}

func TestCircuitBreaker_Defaults(t *testing.T) {
	cb := llm.NewCircuitBreaker(nil, 0, 0)
	if cb.State() != llm.StateClosed {
		t.Fatalf("expected default state %q, got %q", llm.StateClosed, cb.State())
	}

	// Default threshold should be 5
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
		if cb.State() != llm.StateClosed {
			t.Fatalf("expected closed state after %d failures with default threshold 5", i+1)
		}
	}
	cb.RecordFailure() // 5th failure
	if cb.State() != llm.StateOpen {
		t.Fatalf("expected open state after 5 failures with default threshold")
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	cb := llm.NewCircuitBreaker(domain.NewRealClock(), 10, 50*time.Millisecond)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%3 == 0 {
				cb.Allow()
			} else if idx%3 == 1 {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}
			_ = cb.State()
		}(i)
	}

	wg.Wait()
}

func TestBuildSystemPrompt(t *testing.T) {
	spec := &domain.PersonaSpec{
		ID:            "gato_social",
		Name:          "Gato Social",
		BotName:       "MichiBot",
		BasePersona:   "Eres un gato callejero sarcástico pero afectuoso.",
		TriggerPrefix: "!michi",
		Tiers: []domain.TierSpec{
			{ID: "t3", Name: "Mejor Amigo", Min: 100, Max: 200, Behavior: "Super leal"},
			{ID: "t1", Name: "Desconocido", Min: 0, Max: 29, Behavior: "Distante y frío"},
			{ID: "t2", Name: "Conocido", Min: 30, Max: 99, Behavior: "Curioso"},
		},
		Slots: []domain.SlotSpec{
			{ID: "vip_cat", Title: "Gato VIP", MaxHolders: 1},
			{ID: "alpha_cat", Title: "Gato Alfa", MaxHolders: 1},
		},
	}

	prompt1 := llm.BuildSystemPrompt(spec)
	prompt2 := llm.BuildSystemPrompt(spec)

	// Context caching verification: prompt must be deterministic and identical
	if prompt1 != prompt2 {
		t.Fatalf("BuildSystemPrompt must be deterministic for prefix caching")
	}

	// Must contain base persona and bot name
	if !strings.Contains(prompt1, spec.BasePersona) {
		t.Fatalf("prompt missing BasePersona")
	}
	if !strings.Contains(prompt1, spec.BotName) {
		t.Fatalf("prompt missing BotName")
	}

	// Tiers must be sorted by Min (Desconocido [0], Conocido [30], Mejor Amigo [100])
	posT1 := strings.Index(prompt1, "Desconocido")
	posT2 := strings.Index(prompt1, "Conocido")
	posT3 := strings.Index(prompt1, "Mejor Amigo")
	if posT1 == -1 || posT2 == -1 || posT3 == -1 {
		t.Fatalf("prompt missing tier names")
	}
	if !(posT1 < posT2 && posT2 < posT3) {
		t.Fatalf("tiers not sorted by Min in prompt: posT1=%d, posT2=%d, posT3=%d", posT1, posT2, posT3)
	}

	// Slots must be sorted by ID (alpha_cat before vip_cat)
	posAlpha := strings.Index(prompt1, "alpha_cat")
	posVip := strings.Index(prompt1, "vip_cat")
	if posAlpha == -1 || posVip == -1 {
		t.Fatalf("prompt missing slot IDs")
	}
	if !(posAlpha < posVip) {
		t.Fatalf("slots not sorted by ID in prompt: posAlpha=%d, posVip=%d", posAlpha, posVip)
	}

	// Security rules must be present
	if !strings.Contains(prompt1, "[MENSAJE_ENTRADA_NO_CONFIABLE]") {
		t.Fatalf("prompt missing security mention of [MENSAJE_ENTRADA_NO_CONFIABLE]")
	}
	if !strings.Contains(prompt1, "380") {
		t.Fatalf("prompt missing character limit rule 380")
	}

	// JSON schema must be present
	schemaParts := []string{`"reply"`, `"affinity_delta"`, `"new_title"`, `"new_fact"`, `"slot_claim"`}
	for _, part := range schemaParts {
		if !strings.Contains(prompt1, part) {
			t.Fatalf("prompt missing JSON schema component %s", part)
		}
	}
}

func TestBuildUserPrompt(t *testing.T) {
	t.Run("with populated data", func(t *testing.T) {
		input := domain.PromptInput{
			Profile: domain.ViewerProfile{
				UserID:      "u123",
				Username:    "juanperez",
				DisplayName: "JuanPerez",
				Affinity:    45,
				EarnedTitle: "El Aventurero",
				Facts:       []string{"Le gusta pescar", "No toma café"},
				RecentTurns: []domain.ConversationTurn{
					{
						UserMessage: "Hola michi",
						BotReply:    "Miau, ¿qué quieres?",
						Timestamp:   time.Now(),
					},
				},
			},
			CurrentTier: domain.TierSpec{
				ID:       "t2",
				Name:     "Conocido",
				Min:      30,
				Max:      99,
				Behavior: "Trátalo con cierta confianza",
			},
			ActiveSlots: map[string][]domain.SlotHolder{
				"vip": {
					{
						SlotID:      "vip",
						UserID:      "u999",
						DisplayName: "ReyGato",
						Affinity:    150,
					},
				},
			},
			RecentRoom: []domain.RoomMessage{
				{
					DisplayName: "OtroViewer",
					Text:        "Grande el stream",
				},
			},
			ViewerMessage: "¡Mira lo que encontré!",
		}

		userPrompt := llm.BuildUserPrompt(input)

		if !strings.Contains(userPrompt, "[ESTADO_SOCIAL_CANAL]") {
			t.Fatalf("user prompt missing [ESTADO_SOCIAL_CANAL]")
		}
		if !strings.Contains(userPrompt, "vip: @ReyGato (afinidad: 150)") {
			t.Fatalf("user prompt missing formatted slot holder")
		}

		if !strings.Contains(userPrompt, "[HILO_RECIENTE_SALA]") {
			t.Fatalf("user prompt missing [HILO_RECIENTE_SALA]")
		}
		if !strings.Contains(userPrompt, "@OtroViewer: Grande el stream") {
			t.Fatalf("user prompt missing recent room message")
		}

		if !strings.Contains(userPrompt, "[FICHA_ESPECTADOR_ACTUAL]") {
			t.Fatalf("user prompt missing [FICHA_ESPECTADOR_ACTUAL]")
		}
		if !strings.Contains(userPrompt, "JuanPerez") || !strings.Contains(userPrompt, "juanperez") {
			t.Fatalf("user prompt missing viewer display name or username")
		}
		if !strings.Contains(userPrompt, "El Aventurero") {
			t.Fatalf("user prompt missing viewer earned title")
		}
		if !strings.Contains(userPrompt, "Le gusta pescar") {
			t.Fatalf("user prompt missing viewer facts")
		}
		if !strings.Contains(userPrompt, "Hola michi") || !strings.Contains(userPrompt, "Miau, ¿qué quieres?") {
			t.Fatalf("user prompt missing recent conversation turn")
		}

		if !strings.Contains(userPrompt, "[MENSAJE_ENTRADA_NO_CONFIABLE]") {
			t.Fatalf("user prompt missing [MENSAJE_ENTRADA_NO_CONFIABLE]")
		}
		if !strings.Contains(userPrompt, "¡Mira lo que encontré!") {
			t.Fatalf("user prompt missing viewer message")
		}
	})

	t.Run("with empty slots and room", func(t *testing.T) {
		input := domain.PromptInput{
			Profile: domain.ViewerProfile{
				DisplayName: "Novato",
				Username:    "novato",
				Affinity:    0,
			},
			CurrentTier: domain.TierSpec{
				ID:       "t1",
				Name:     "Desconocido",
				Behavior: "Ignóralo un poco",
			},
			ActiveSlots:   nil,
			RecentRoom:    nil,
			ViewerMessage: "Hola",
		}

		prompt := llm.BuildUserPrompt(input)
		if !strings.Contains(prompt, "sin puestos asignados") {
			t.Fatalf("expected 'sin puestos asignados' for empty active slots")
		}
		if !strings.Contains(prompt, "sin mensajes previos") {
			t.Fatalf("expected 'sin mensajes previos' for empty recent room")
		}
	})
}

func TestParseLLMDecision(t *testing.T) {
	t.Run("clean json", func(t *testing.T) {
		raw := `{"reply":"Hola crack","affinity_delta":3,"new_title":"Compa","new_fact":"Fan de Go","slot_claim":"vip"}`
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dec.Reply != "Hola crack" {
			t.Errorf("expected reply %q, got %q", "Hola crack", dec.Reply)
		}
		if dec.AffinityDelta != 3 {
			t.Errorf("expected delta 3, got %d", dec.AffinityDelta)
		}
		if dec.NewTitle != "Compa" || dec.NewFact != "Fan de Go" || dec.SlotClaim != "vip" {
			t.Errorf("unexpected parsed fields: %+v", dec)
		}
	})

	t.Run("markdown fenced json", func(t *testing.T) {
		raw := "```json\n{\n  \"reply\": \"Respuesta en bloque\",\n  \"affinity_delta\": -2\n}\n```"
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dec.Reply != "Respuesta en bloque" || dec.AffinityDelta != -2 {
			t.Fatalf("unexpected decision: %+v", dec)
		}
	})

	t.Run("surrounded by prose", func(t *testing.T) {
		raw := "Aquí tienes la respuesta solicitada:\n```json\n{\"reply\": \"Dentro de prosa\", \"affinity_delta\": 1}\n```\nSaludos cordiales."
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dec.Reply != "Dentro de prosa" || dec.AffinityDelta != 1 {
			t.Fatalf("unexpected decision: %+v", dec)
		}
	})

	t.Run("plain text fallback", func(t *testing.T) {
		raw := "¡Hola! No soy un JSON pero soy una respuesta válida."
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dec.Reply != raw {
			t.Errorf("expected fallback reply to match raw text, got %q", dec.Reply)
		}
		if dec.AffinityDelta != 0 {
			t.Errorf("expected default affinity delta 0, got %d", dec.AffinityDelta)
		}
	})

	t.Run("json with empty reply falls back to trimmed raw", func(t *testing.T) {
		raw := `{"affinity_delta": 4}`
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dec.Reply != raw {
			t.Fatalf("expected fallback reply %q, got %q", raw, dec.Reply)
		}
	})

	t.Run("truncated json extracts reply cleanly", func(t *testing.T) {
		raw := `{"reply":"¿Israel? Eso suena a debate de sobremesa que no pienso tocar en stream, @rhfjfgzrdg. Pregunta algo que no requiera un panel de expertos y seguimos.","affinity_delta":0,"new_title":"","new_fact":"","slot_claim":""`
		dec, err := llm.ParseLLMDecision(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "¿Israel? Eso suena a debate de sobremesa que no pienso tocar en stream, @rhfjfgzrdg. Pregunta algo que no requiera un panel de expertos y seguimos."
		if dec.Reply != expected {
			t.Fatalf("expected extracted reply %q, got %q", expected, dec.Reply)
		}
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := llm.ParseLLMDecision("   \n\t  ")
		if err == nil {
			t.Fatalf("expected error for whitespace string, got nil")
		}
	})
}

func TestClient_Generate_Success(t *testing.T) {
	var capturedAuth string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("expected path ending in /chat/completions, got %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")

		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "{\"reply\":\"Hola gamer\",\"affinity_delta\":2}"
					}
				}
			]
		}`))
	}))
	defer server.Close()

	clock := domain.NewFakeClock(time.Now())
	cfg := llm.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "secret-token-123",
		Model:      "deepseek-flash",
		HTTPClient: server.Client(),
	}
	client := llm.NewClient(cfg, clock)

	input := domain.PromptInput{
		Persona: &domain.PersonaSpec{
			ID:          "bot",
			BotName:     "TestBot",
			BasePersona: "Test persona",
		},
		ViewerMessage: "Hola bot",
	}

	decision, err := client.Generate(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Reply != "Hola gamer" || decision.AffinityDelta != 2 {
		t.Fatalf("unexpected decision: %+v", decision)
	}

	if capturedAuth != "Bearer secret-token-123" {
		t.Errorf("expected auth 'Bearer secret-token-123', got %q", capturedAuth)
	}
	if capturedBody["model"] != "deepseek-flash" {
		t.Errorf("expected model 'deepseek-flash', got %v", capturedBody["model"])
	}
	messages, ok := capturedBody["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages (system, user), got %v", messages)
	}
}

func TestClient_BaseURL_Normalization(t *testing.T) {
	paths := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	clock := domain.NewRealClock()

	// Test 1: URL without trailing slash
	c1 := llm.NewClient(llm.ClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}, clock)
	_, _ = c1.Generate(context.Background(), domain.PromptInput{})

	// Test 2: URL with trailing slash
	c2 := llm.NewClient(llm.ClientConfig{
		BaseURL:    server.URL + "/",
		HTTPClient: server.Client(),
	}, clock)
	_, _ = c2.Generate(context.Background(), domain.PromptInput{})

	// Test 3: URL ending in /v1
	c3 := llm.NewClient(llm.ClientConfig{
		BaseURL:    server.URL + "/v1",
		HTTPClient: server.Client(),
	}, clock)
	_, _ = c3.Generate(context.Background(), domain.PromptInput{})

	// Test 4: URL ending in /v1/
	c4 := llm.NewClient(llm.ClientConfig{
		BaseURL:    server.URL + "/v1/",
		HTTPClient: server.Client(),
	}, clock)
	_, _ = c4.Generate(context.Background(), domain.PromptInput{})

	expected := []string{
		"/chat/completions",
		"/chat/completions",
		"/v1/chat/completions",
		"/v1/chat/completions",
	}

	if len(paths) != len(expected) {
		t.Fatalf("expected %d requests, got %d", len(expected), len(paths))
	}
	for i, exp := range expected {
		if paths[i] != exp {
			t.Errorf("request %d: expected path %q, got %q", i, exp, paths[i])
		}
	}
}

func TestClient_CircuitBreaker_TriggersOnErrors(t *testing.T) {
	statusCode := http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	clock := domain.NewFakeClock(time.Now())
	cfg := llm.ClientConfig{
		BaseURL:          server.URL,
		FailureThreshold: 2,
		BreakerCooldown:  15 * time.Second,
		HTTPClient:       server.Client(),
	}
	client := llm.NewClient(cfg, clock)

	// Call 1: HTTP 500
	_, err := client.Generate(context.Background(), domain.PromptInput{})
	if err == nil {
		t.Fatalf("expected error from HTTP 500")
	}

	// Call 2: HTTP 429
	statusCode = http.StatusTooManyRequests
	_, err = client.Generate(context.Background(), domain.PromptInput{})
	if err == nil {
		t.Fatalf("expected error from HTTP 429")
	}

	// Now threshold (2) reached -> Breaker is open
	_, err = client.Generate(context.Background(), domain.PromptInput{})
	if !errors.Is(err, llm.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Advance clock past cooldown -> Half-open probe
	clock.Advance(15 * time.Second)
	statusCode = http.StatusOK
	// Prepare server response for recovery
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"reply\":\"recovered\"}"}}]}`))
	})

	dec, err := client.Generate(context.Background(), domain.PromptInput{})
	if err != nil {
		t.Fatalf("expected probe to succeed after recovery, got %v", err)
	}
	if dec.Reply != "recovered" {
		t.Fatalf("expected recovered reply, got %q", dec.Reply)
	}

	// Subsequent request succeeds normally
	dec2, err2 := client.Generate(context.Background(), domain.PromptInput{})
	if err2 != nil || dec2.Reply != "recovered" {
		t.Fatalf("expected subsequent request to succeed, got %v", err2)
	}
}
