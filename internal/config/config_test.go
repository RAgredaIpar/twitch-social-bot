package config_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/config"
	"github.com/twitch-social-bot/internal/domain"
)

func TestFakeClock(t *testing.T) {
	initial := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(initial)

	if got := clock.Now(); !got.Equal(initial) {
		t.Fatalf("expected initial time %v, got %v", initial, got)
	}

	advanced := clock.Advance(5 * time.Minute)
	expectedAdv := initial.Add(5 * time.Minute)
	if !advanced.Equal(expectedAdv) {
		t.Fatalf("expected advanced time %v, got %v", expectedAdv, advanced)
	}
	if got := clock.Now(); !got.Equal(expectedAdv) {
		t.Fatalf("expected Now() after advance %v, got %v", expectedAdv, got)
	}

	newTime := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.Set(newTime)
	if got := clock.Now(); !got.Equal(newTime) {
		t.Fatalf("expected Now() after Set %v, got %v", newTime, got)
	}

	// Concurrency test with -race
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = clock.Now()
		}()
		go func() {
			defer wg.Done()
			clock.Advance(1 * time.Millisecond)
		}()
	}
	wg.Wait()
}

func TestRealClock(t *testing.T) {
	rc := domain.NewRealClock()
	before := time.Now().UTC()
	got := rc.Now()
	after := time.Now().UTC()

	if got.Location() != time.UTC {
		t.Fatalf("expected RealClock.Now() in UTC, got %v", got.Location())
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("expected RealClock.Now() %v to be between %v and %v", got, before, after)
	}
}

func TestConfig_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("LLM_API_KEY", "sk-default-test")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("expected no error loading default config, got %v", err)
	}

	if cfg.LLMAPIKey != "sk-default-test" {
		t.Errorf("expected LLMAPIKey 'sk-default-test', got '%s'", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://api.deepseek.com" {
		t.Errorf("expected LLMBaseURL 'https://api.deepseek.com', got '%s'", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "deepseek-flash" {
		t.Errorf("expected LLMModel 'deepseek-flash', got '%s'", cfg.LLMModel)
	}
	if cfg.LLMTimeout != 8*time.Second {
		t.Errorf("expected LLMTimeout 8s, got %v", cfg.LLMTimeout)
	}
	if cfg.TwitchClientID != "" {
		t.Errorf("expected empty TwitchClientID, got '%s'", cfg.TwitchClientID)
	}
	if cfg.TwitchBroadcasterLogin != "" {
		t.Errorf("expected empty TwitchBroadcasterLogin, got '%s'", cfg.TwitchBroadcasterLogin)
	}
	if cfg.TwitchBotLogin != "" {
		t.Errorf("expected empty TwitchBotLogin, got '%s'", cfg.TwitchBotLogin)
	}
	if cfg.TwitchAccessToken != "" {
		t.Errorf("expected empty TwitchAccessToken, got '%s'", cfg.TwitchAccessToken)
	}
	if cfg.TwitchRefreshToken != "" {
		t.Errorf("expected empty TwitchRefreshToken, got '%s'", cfg.TwitchRefreshToken)
	}
	if cfg.TokenFile != ".tokens.json" {
		t.Errorf("expected TokenFile '.tokens.json', got '%s'", cfg.TokenFile)
	}
	if cfg.PersonaDir != "personas" {
		t.Errorf("expected PersonaDir 'personas', got '%s'", cfg.PersonaDir)
	}
	if cfg.PersonaFile != "personas/cohost_amigos.yaml" {
		t.Errorf("expected PersonaFile 'personas/cohost_amigos.yaml', got '%s'", cfg.PersonaFile)
	}
	if cfg.StorePath != "data/social.db" {
		t.Errorf("expected StorePath 'data/social.db', got '%s'", cfg.StorePath)
	}
	if cfg.GlobalCooldown != 3*time.Second {
		t.Errorf("expected GlobalCooldown 3s, got %v", cfg.GlobalCooldown)
	}
	if cfg.UserCooldown != 15*time.Second {
		t.Errorf("expected UserCooldown 15s, got %v", cfg.UserCooldown)
	}
	if cfg.RoomBufferSize != 25 {
		t.Errorf("expected RoomBufferSize 25, got %d", cfg.RoomBufferSize)
	}
	if cfg.MaxViewerFacts != 15 {
		t.Errorf("expected MaxViewerFacts 15, got %d", cfg.MaxViewerFacts)
	}
	if cfg.RoomBufferMaxAge != 10*time.Minute {
		t.Errorf("expected RoomBufferMaxAge 10m, got %v", cfg.RoomBufferMaxAge)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("expected HTTPAddr '127.0.0.1:8080', got '%s'", cfg.HTTPAddr)
	}
}

func TestConfig_EnvFileParsing(t *testing.T) {
	clearEnv(t)

	envContent := `
# Archivo de configuración para pruebas
LLM_API_KEY="sk-file-key"
LLM_BASE_URL='https://custom.api.endpoint'
LLM_MODEL="deepseek-custom"
LLM_TIMEOUT_MS=12000
TWITCH_CLIENT_ID="client_123"
TWITCH_BROADCASTER_LOGIN="streamer_login"
TWITCH_BOT_LOGIN="bot_login"
TWITCH_ACCESS_TOKEN="token_abc"
TWITCH_REFRESH_TOKEN="refresh_xyz"
TWITCH_TOKEN_FILE="custom_tokens.json"
PERSONA_DIR="custom_personas"
PERSONA_FILE="custom_personas/custom.yaml"
STORE_PATH="custom/social.db"
GLOBAL_COOLDOWN_MS=4500
USER_COOLDOWN_MS=20000
ROOM_BUFFER_SIZE=50
ROOM_BUFFER_MAX_AGE_SEC=300
HTTP_ADDR="0.0.0.0:9090"
`
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env.test")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatalf("failed to create temp env file: %v", err)
	}

	cfg, err := config.Load(envPath)
	if err != nil {
		t.Fatalf("expected no error loading config from file, got %v", err)
	}

	if cfg.LLMAPIKey != "sk-file-key" {
		t.Errorf("expected LLMAPIKey 'sk-file-key', got '%s'", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://custom.api.endpoint" {
		t.Errorf("expected LLMBaseURL 'https://custom.api.endpoint', got '%s'", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "deepseek-custom" {
		t.Errorf("expected LLMModel 'deepseek-custom', got '%s'", cfg.LLMModel)
	}
	if cfg.LLMTimeout != 12*time.Second {
		t.Errorf("expected LLMTimeout 12s, got %v", cfg.LLMTimeout)
	}
	if cfg.TwitchClientID != "client_123" {
		t.Errorf("expected TwitchClientID 'client_123', got '%s'", cfg.TwitchClientID)
	}
	if cfg.TwitchBroadcasterLogin != "streamer_login" {
		t.Errorf("expected TwitchBroadcasterLogin 'streamer_login', got '%s'", cfg.TwitchBroadcasterLogin)
	}
	if cfg.TwitchBotLogin != "bot_login" {
		t.Errorf("expected TwitchBotLogin 'bot_login', got '%s'", cfg.TwitchBotLogin)
	}
	if cfg.TwitchAccessToken != "token_abc" {
		t.Errorf("expected TwitchAccessToken 'token_abc', got '%s'", cfg.TwitchAccessToken)
	}
	if cfg.TwitchRefreshToken != "refresh_xyz" {
		t.Errorf("expected TwitchRefreshToken 'refresh_xyz', got '%s'", cfg.TwitchRefreshToken)
	}
	if cfg.TokenFile != "custom_tokens.json" {
		t.Errorf("expected TokenFile 'custom_tokens.json', got '%s'", cfg.TokenFile)
	}
	if cfg.PersonaDir != "custom_personas" {
		t.Errorf("expected PersonaDir 'custom_personas', got '%s'", cfg.PersonaDir)
	}
	if cfg.PersonaFile != "custom_personas/custom.yaml" {
		t.Errorf("expected PersonaFile 'custom_personas/custom.yaml', got '%s'", cfg.PersonaFile)
	}
	if cfg.StorePath != "custom/social.db" {
		t.Errorf("expected StorePath 'custom/social.db', got '%s'", cfg.StorePath)
	}
	if cfg.GlobalCooldown != 4500*time.Millisecond {
		t.Errorf("expected GlobalCooldown 4500ms, got %v", cfg.GlobalCooldown)
	}
	if cfg.UserCooldown != 20*time.Second {
		t.Errorf("expected UserCooldown 20s, got %v", cfg.UserCooldown)
	}
	if cfg.RoomBufferSize != 50 {
		t.Errorf("expected RoomBufferSize 50, got %d", cfg.RoomBufferSize)
	}
	if cfg.RoomBufferMaxAge != 5*time.Minute {
		t.Errorf("expected RoomBufferMaxAge 5m, got %v", cfg.RoomBufferMaxAge)
	}
	if cfg.HTTPAddr != "0.0.0.0:9090" {
		t.Errorf("expected HTTPAddr '0.0.0.0:9090', got '%s'", cfg.HTTPAddr)
	}
}

func TestConfig_EnvOverridePrecedence(t *testing.T) {
	clearEnv(t)

	envContent := `
LLM_API_KEY="from-file"
LLM_MODEL="model-from-file"
ROOM_BUFFER_SIZE=30
`
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env.test")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatalf("failed to create temp env file: %v", err)
	}

	t.Setenv("LLM_API_KEY", "from-env-override")
	t.Setenv("LLM_MODEL", "model-from-env")

	cfg, err := config.Load(envPath)
	if err != nil {
		t.Fatalf("expected no error loading config, got %v", err)
	}

	if cfg.LLMAPIKey != "from-env-override" {
		t.Errorf("expected LLMAPIKey to be overridden by env to 'from-env-override', got '%s'", cfg.LLMAPIKey)
	}
	if cfg.LLMModel != "model-from-env" {
		t.Errorf("expected LLMModel to be overridden by env to 'model-from-env', got '%s'", cfg.LLMModel)
	}
	if cfg.RoomBufferSize != 30 {
		t.Errorf("expected RoomBufferSize from file 30, got %d", cfg.RoomBufferSize)
	}
}

func TestConfig_Validation(t *testing.T) {
	t.Run("missing LLM_API_KEY", func(t *testing.T) {
		clearEnv(t)
		_, err := config.Load("")
		if err == nil {
			t.Fatal("expected error when LLM_API_KEY is missing, got nil")
		}
	})

	t.Run("negative GLOBAL_COOLDOWN_MS", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LLM_API_KEY", "valid-key")
		t.Setenv("GLOBAL_COOLDOWN_MS", "-100")
		_, err := config.Load("")
		if err == nil {
			t.Fatal("expected error for negative GLOBAL_COOLDOWN_MS, got nil")
		}
	})

	t.Run("negative USER_COOLDOWN_MS", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LLM_API_KEY", "valid-key")
		t.Setenv("USER_COOLDOWN_MS", "-500")
		_, err := config.Load("")
		if err == nil {
			t.Fatal("expected error for negative USER_COOLDOWN_MS, got nil")
		}
	})

	t.Run("non-positive ROOM_BUFFER_SIZE", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LLM_API_KEY", "valid-key")
		t.Setenv("ROOM_BUFFER_SIZE", "0")
		_, err := config.Load("")
		if err == nil {
			t.Fatal("expected error for ROOM_BUFFER_SIZE <= 0, got nil")
		}
	})

	t.Run("non-positive MAX_VIEWER_FACTS", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LLM_API_KEY", "valid-key")
		t.Setenv("MAX_VIEWER_FACTS", "0")
		_, err := config.Load("")
		if err == nil {
			t.Fatal("expected error for MAX_VIEWER_FACTS <= 0, got nil")
		}
	})
}

func clearEnv(t *testing.T) {
	keys := []string{
		"LLM_API_KEY",
		"LLM_BASE_URL",
		"LLM_MODEL",
		"LLM_TIMEOUT_MS",
		"TWITCH_CLIENT_ID",
		"TWITCH_BROADCASTER_LOGIN",
		"TWITCH_BOT_LOGIN",
		"TWITCH_ACCESS_TOKEN",
		"TWITCH_REFRESH_TOKEN",
		"TWITCH_TOKEN_FILE",
		"PERSONA_DIR",
		"PERSONA_FILE",
		"STORE_PATH",
		"GLOBAL_COOLDOWN_MS",
		"USER_COOLDOWN_MS",
		"ROOM_BUFFER_SIZE",
		"MAX_VIEWER_FACTS",
		"ROOM_BUFFER_MAX_AGE_SEC",
		"HTTP_ADDR",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}
