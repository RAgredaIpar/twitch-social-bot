package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/twitch-social-bot/internal/engine"
)

// Server wraps an HTTP server exposing diagnostic endpoints.
type Server struct {
	httpServer *http.Server
	orch       *engine.Orchestrator
	handler    http.Handler
}

// NewServer creates a new diagnostic HTTP server.
func NewServer(addr string, orch *engine.Orchestrator) *Server {
	handler := NewHandler(orch)
	return &Server{
		orch:    orch,
		handler: handler,
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
		},
	}
}

// Handler returns the underlying http.Handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Start begins listening and serving HTTP requests.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server without interrupting active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// NewHandler constructs an http.Handler routing /healthz and /status.
func NewHandler(orch *engine.Orchestrator) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var snap engine.Status
		if orch != nil {
			snap = orch.StatusSnapshot(r.Context())
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(snap)
	})

	return mux
}
