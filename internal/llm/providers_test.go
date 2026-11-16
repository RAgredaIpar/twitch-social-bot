package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/llm"
)

func TestDetectProvider_ByIDAndURL(t *testing.T) {
	if p := llm.DetectProvider("claude", ""); p.ID != "claude" || p.Protocol != "anthropic" {
		t.Fatalf("expected claude anthropic spec, got %+v", p)
	}
	if p := llm.DetectProvider("", "https://generativelanguage.googleapis.com/v1beta/openai"); p.ID != "gemini" {
		t.Fatalf("expected gemini inferred from URL, got %+v", p)
	}
	if p := llm.DetectProvider("", "https://my-private-proxy.local/v1"); p.ID != "custom" {
		t.Fatalf("expected custom inferred from URL, got %+v", p)
	}
}

func TestDiscoverBestModel_LiveAndFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Fatalf("expected anthropic x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-4-haiku-20260101"},{"id":"claude-3-opus"}]}`))
	}))
	defer srv.Close()

	got := llm.DiscoverBestModel(context.Background(), srv.Client(), "claude", srv.URL, "ant-key")
	if got != "claude-4-haiku-20260101" {
		t.Fatalf("expected live discovered model 'claude-4-haiku-20260101', got %q", got)
	}

	// Fallback when server is unreachable
	fb := llm.DiscoverBestModel(context.Background(), srv.Client(), "openai", "http://127.0.0.1:1", "sk-test")
	if fb != "gpt-4o-mini" {
		t.Fatalf("expected fallback 'gpt-4o-mini', got %q", fb)
	}
}

func TestClient_Generate_AnthropicProtocol(t *testing.T) {
	var gotAPIKey, gotVersion, gotPath string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"reply\":\"ni de broma\",\"affinity_delta\":2}"}]}`))
	}))
	defer srv.Close()

	client := llm.NewClient(llm.ClientConfig{
		Provider:   "claude",
		BaseURL:    srv.URL,
		APIKey:     "sk-ant-123",
		Model:      "claude-3-5-haiku-latest",
		HTTPClient: srv.Client(),
	}, nil)

	dec, err := client.Generate(context.Background(), domain.PromptInput{
		Persona: &domain.PersonaSpec{
			ID:          "cohost_amigos",
			BotName:     "Mochi",
			BasePersona: "Co-host",
		},
		ViewerMessage: "hola",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/messages" || gotAPIKey != "sk-ant-123" || gotVersion != "2023-06-01" {
		t.Fatalf("unexpected anthropic request headers/path: path=%q key=%q ver=%q", gotPath, gotAPIKey, gotVersion)
	}
	if dec.Reply != "ni de broma" || dec.AffinityDelta != 2 {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}
