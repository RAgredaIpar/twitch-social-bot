package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/cli"
	"github.com/twitch-social-bot/internal/config"
	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
)

func TestRenderCard_ContainsTitleWithoutV2AndShowsStatus(t *testing.T) {
	loc, err := i18n.NewManager("", "en")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		BotLanguage:            "en",
		LLMAPIKey:              "sk-test1234",
		LLMModel:               "deepseek-flash",
		TwitchClientID:         "cid123",
		TwitchBroadcasterLogin: "rhfjfgzrdg",
		TwitchBotLogin:         "MochiBot__",
		MaxViewerFacts:         15,
		GlobalCooldown:         3 * time.Second,
	}
	active := &domain.PersonaSpec{
		ID:      "cohost_amigos",
		Name:    "Co-Host Amigos",
		BotName: "Mochi",
	}

	var buf bytes.Buffer
	cli.RenderCard(&buf, cfg, loc, active)
	out := buf.String()

	if !strings.Contains(out, "Twitch Social Bot") {
		t.Fatalf("expected card to contain 'Twitch Social Bot'")
	}
	if strings.Contains(out, "v2.0") || strings.Contains(out, "V2.0") {
		t.Fatalf("card must NOT contain 'v2.0'")
	}
	if !strings.Contains(out, "Mochi (cohost_amigos)") {
		t.Fatalf("expected active persona 'Mochi (cohost_amigos)' in card")
	}
	if !strings.Contains(out, "[en] English") {
		t.Fatalf("expected '[en] English' in card")
	}
	cli.AppVersion = "v1.0.1"
	defer func() { cli.AppVersion = "" }()
	var bufVer bytes.Buffer
	cli.RenderCard(&bufVer, cfg, loc, nil)
	if !strings.Contains(bufVer.String(), "v1.0.1") {
		t.Fatalf("expected AppVersion 'v1.0.1' in card")
	}
}

func TestRunInteractive_FirstTimeOnboarding(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		BotLanguage:    "en",
		LocaleDir:      "locales",
		LLMBaseURL:     "https://api.deepseek.com",
		LLMModel:       "deepseek-chat",
		LLMTimeout:     8 * time.Second,
		GlobalCooldown: 3 * time.Second,
		UserCooldown:   15 * time.Second,
		RoomBufferSize: 25,
		MaxViewerFacts: 15,
		HTTPAddr:       "127.0.0.1:8080",
	}

	// Simulate first-time onboarding in non-TTY fallback mode:
	// 2 -> Español
	// sk-onboard-key -> DeepSeek API key
	// cid-onboard -> Twitch Client ID
	// streamer_one -> Broadcaster login
	// 1 -> Dedicated bot account
	// mochi_bot -> Bot login
	// "" -> Press Enter to open main menu
	// 5 -> Exit
	input := strings.Join([]string{
		"2",
		"sk-onboard-key",
		"cid-onboard",
		"streamer_one",
		"1",
		"mochi_bot",
		"",
		"5",
	}, "\n") + "\n"

	var out bytes.Buffer
	start, updatedCfg, err := cli.RunInteractive(strings.NewReader(input), &out, envPath, cfg, loc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start {
		t.Fatalf("expected start=false after choosing exit")
	}
	if loc.ActiveID() != "es" {
		t.Fatalf("expected language switched to 'es', got %q", loc.ActiveID())
	}
	if updatedCfg.LLMAPIKey != "sk-onboard-key" || updatedCfg.TwitchBotLogin != "mochi_bot" {
		t.Fatalf("unexpected updated config: %+v", updatedCfg)
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected .env file to be created: %v", err)
	}
	envContent := string(envBytes)
	if !strings.Contains(envContent, "BOT_LANGUAGE=es") || !strings.Contains(envContent, "LLM_API_KEY=sk-onboard-key") {
		t.Fatalf("saved .env missing expected entries:\n%s", envContent)
	}
}

func TestRunInteractive_SwitchLanguageAndSetupEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("BOT_LANGUAGE=en\nLLM_API_KEY=sk-existing\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		BotLanguage:    "en",
		LocaleDir:      "locales",
		LLMAPIKey:      "sk-existing",
		LLMBaseURL:     "https://api.deepseek.com",
		LLMModel:       "deepseek-flash",
		LLMTimeout:     8 * time.Second,
		GlobalCooldown: 3 * time.Second,
		UserCooldown:   15 * time.Second,
		RoomBufferSize: 25,
		MaxViewerFacts: 15,
		HTTPAddr:       "127.0.0.1:8080",
	}

	// Simulate returning user (skips onboarding):
	// 3 -> Switch language -> 2 (es)
	// 2 -> Setup wizard -> sk-1234567890, cid-abc, streamer_one, mochi_bot, 20
	// 5 -> Exit
	input := strings.Join([]string{
		"3",
		"2",
		"2",
		"sk-1234567890",
		"cid-abc",
		"streamer_one",
		"mochi_bot",
		"20",
		"5",
	}, "\n") + "\n"

	var out bytes.Buffer
	start, updatedCfg, err := cli.RunInteractive(strings.NewReader(input), &out, envPath, cfg, loc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start {
		t.Fatalf("expected start=false after choosing exit")
	}
	if loc.ActiveID() != "es" {
		t.Fatalf("expected language switched to 'es', got %q", loc.ActiveID())
	}
	if updatedCfg.LLMAPIKey != "sk-1234567890" || updatedCfg.MaxViewerFacts != 20 {
		t.Fatalf("unexpected updated config: %+v", updatedCfg)
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected .env file to be created: %v", err)
	}
	envContent := string(envBytes)
	if !strings.Contains(envContent, "BOT_LANGUAGE=es") || !strings.Contains(envContent, "LLM_API_KEY=sk-1234567890") {
		t.Fatalf("saved .env missing expected entries:\n%s", envContent)
	}
}

func TestPromptMandatoryUpdate_AcceptAndExit(t *testing.T) {
	loc, err := i18n.NewManager(filepath.Join("..", "..", "locales"), "es")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BotLanguage: "es", LLMAPIKey: "sk-test"}

	var outAccept bytes.Buffer
	ok, err := cli.PromptMandatoryUpdate(strings.NewReader("1\n"), &outAccept, cfg, loc, nil, "abc1234", "def5678")
	if err != nil || !ok {
		t.Fatalf("expected accept=true, got %v (err=%v)", ok, err)
	}
	if !strings.Contains(outAccept.String(), "abc1234 -> def5678") {
		t.Fatalf("expected version diff in prompt output:\n%s", outAccept.String())
	}

	var outExit bytes.Buffer
	ok, err = cli.PromptMandatoryUpdate(strings.NewReader("2\n"), &outExit, cfg, loc, nil, "abc1234", "def5678")
	if err != nil || ok {
		t.Fatalf("expected accept=false on exit, got %v (err=%v)", ok, err)
	}
}
