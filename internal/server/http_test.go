package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/engine"
	"github.com/twitch-social-bot/internal/server"
	"github.com/twitch-social-bot/internal/store"
)

func newTestOrchestrator(t *testing.T) *engine.Orchestrator {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return engine.NewOrchestrator(engine.OrchestratorConfig{
		BotUserID: "bot_123",
		BotLogin:  "lunabot",
		Store:     st,
	})
}

func TestHealthzEndpoint(t *testing.T) {
	orch := newTestOrchestrator(t)
	handler := server.NewHandler(orch)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode json body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	orch := newTestOrchestrator(t)
	handler := server.NewHandler(orch)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var snap engine.Status
	if err := json.NewDecoder(rr.Body).Decode(&snap); err != nil {
		t.Fatalf("failed to decode json body: %v", err)
	}

	if snap.Paused {
		t.Errorf("expected paused to be false")
	}
	if snap.ProcessedMessages != 0 {
		t.Errorf("expected processed_messages 0, got %d", snap.ProcessedMessages)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	orch := newTestOrchestrator(t)
	handler := server.NewHandler(orch)

	for _, path := range []string{"/healthz", "/status"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("path %s: expected status 405 Method Not Allowed, got %d", path, rr.Code)
		}
	}
}

func TestNotFound(t *testing.T) {
	orch := newTestOrchestrator(t)
	handler := server.NewHandler(orch)

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404 Not Found, got %d", rr.Code)
	}
}

func TestServerLifecycle(t *testing.T) {
	orch := newTestOrchestrator(t)
	srv := server.NewServer("127.0.0.1:0", orch)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give the server time to start
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("failed to shutdown server: %v", err)
	}

	err := <-errCh
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("expected ErrServerClosed, got: %v", err)
	}
}
