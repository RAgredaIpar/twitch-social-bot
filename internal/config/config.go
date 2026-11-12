package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BotLanguage            string        `json:"bot_language"`
	LocaleDir              string        `json:"locale_dir"`
	LLMAPIKey              string        `json:"llm_api_key"`
	LLMBaseURL             string        `json:"llm_base_url"`
	LLMModel               string        `json:"llm_model"`
	LLMTimeout             time.Duration `json:"llm_timeout"`
	TwitchClientID         string        `json:"twitch_client_id"`
	TwitchBroadcasterLogin string        `json:"twitch_broadcaster_login"`
	TwitchBotLogin         string        `json:"twitch_bot_login"`
	TwitchAccessToken      string        `json:"twitch_access_token"`
	TwitchRefreshToken     string        `json:"twitch_refresh_token"`
	TokenFile              string        `json:"token_file"`
	PersonaDir             string        `json:"persona_dir"`
	PersonaFile            string        `json:"persona_file"`
	StorePath              string        `json:"store_path"`
	GlobalCooldown         time.Duration `json:"global_cooldown"`
	UserCooldown           time.Duration `json:"user_cooldown"`
	RoomBufferSize         int           `json:"room_buffer_size"`
	RoomBufferMaxAge       time.Duration `json:"room_buffer_max_age"`
	MaxViewerFacts         int           `json:"max_viewer_facts"`
	HTTPAddr               string        `json:"http_addr"`
}

// Load loads configuration from environment variables and an optional .env file.
// Values in os.Environ take precedence over values in the .env file.
func Load(envFile string) (*Config, error) {
	cfg, err := LoadRaw(envFile)
	if err != nil {
		return nil, err
	}
	if cfg.LLMAPIKey == "" {
		return nil, errors.New("LLM_API_KEY is required and cannot be empty")
	}
	return cfg, nil
}

// LoadRaw loads configuration without enforcing required runtime secrets like LLM_API_KEY.
// Useful for interactive CLI startup and the .env setup wizard.
func LoadRaw(envFile string) (*Config, error) {
	fileEnv := make(map[string]string)
	if envFile != "" {
		if _, err := os.Stat(envFile); err == nil {
			parsed, err := parseEnvFile(envFile)
			if err != nil {
				return nil, fmt.Errorf("failed to parse env file %q: %w", envFile, err)
			}
			fileEnv = parsed
		}
	}

	get := func(key, def string) string {
		if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
		if val, ok := fileEnv[key]; ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
		return def
	}

	cfg := &Config{
		BotLanguage:            strings.ToLower(get("BOT_LANGUAGE", "en")),
		LocaleDir:              get("LOCALE_DIR", "locales"),
		LLMAPIKey:              get("LLM_API_KEY", ""),
		LLMBaseURL:             get("LLM_BASE_URL", "https://api.deepseek.com"),
		LLMModel:               get("LLM_MODEL", "deepseek-flash"),
		TwitchClientID:         get("TWITCH_CLIENT_ID", ""),
		TwitchBroadcasterLogin: get("TWITCH_BROADCASTER_LOGIN", ""),
		TwitchBotLogin:         get("TWITCH_BOT_LOGIN", ""),
		TwitchAccessToken:      get("TWITCH_ACCESS_TOKEN", ""),
		TwitchRefreshToken:     get("TWITCH_REFRESH_TOKEN", ""),
		TokenFile:              get("TWITCH_TOKEN_FILE", ".tokens.json"),
		PersonaDir:             get("PERSONA_DIR", "personas"),
		PersonaFile:            get("PERSONA_FILE", "personas/cohost_amigos.yaml"),
		StorePath:              get("STORE_PATH", "data/social.db"),
		HTTPAddr:               get("HTTP_ADDR", "127.0.0.1:8080"),
	}

	timeoutStr := get("LLM_TIMEOUT_MS", "8000")
	if ms, err := strconv.Atoi(timeoutStr); err == nil {
		cfg.LLMTimeout = time.Duration(ms) * time.Millisecond
	} else if d, err := time.ParseDuration(timeoutStr); err == nil {
		cfg.LLMTimeout = d
	} else {
		return nil, fmt.Errorf("invalid LLM_TIMEOUT_MS value: %q", timeoutStr)
	}

	gcStr := get("GLOBAL_COOLDOWN_MS", "3000")
	if ms, err := strconv.Atoi(gcStr); err == nil {
		cfg.GlobalCooldown = time.Duration(ms) * time.Millisecond
	} else if d, err := time.ParseDuration(gcStr); err == nil {
		cfg.GlobalCooldown = d
	} else {
		return nil, fmt.Errorf("invalid GLOBAL_COOLDOWN_MS value: %q", gcStr)
	}

	ucStr := get("USER_COOLDOWN_MS", "15000")
	if ms, err := strconv.Atoi(ucStr); err == nil {
		cfg.UserCooldown = time.Duration(ms) * time.Millisecond
	} else if d, err := time.ParseDuration(ucStr); err == nil {
		cfg.UserCooldown = d
	} else {
		return nil, fmt.Errorf("invalid USER_COOLDOWN_MS value: %q", ucStr)
	}

	rbStr := get("ROOM_BUFFER_SIZE", "25")
	rbSize, err := strconv.Atoi(rbStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ROOM_BUFFER_SIZE value: %q", rbStr)
	}
	cfg.RoomBufferSize = rbSize

	mvfStr := get("MAX_VIEWER_FACTS", "15")
	mvf, err := strconv.Atoi(mvfStr)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_VIEWER_FACTS value: %q", mvfStr)
	}
	cfg.MaxViewerFacts = mvf

	rmaStr := get("ROOM_BUFFER_MAX_AGE_SEC", "600")
	if sec, err := strconv.Atoi(rmaStr); err == nil {
		cfg.RoomBufferMaxAge = time.Duration(sec) * time.Second
	} else if d, err := time.ParseDuration(rmaStr); err == nil {
		cfg.RoomBufferMaxAge = d
	} else {
		return nil, fmt.Errorf("invalid ROOM_BUFFER_MAX_AGE_SEC value: %q", rmaStr)
	}

	if cfg.GlobalCooldown < 0 {
		return nil, fmt.Errorf("GLOBAL_COOLDOWN_MS must be >= 0, got %v", cfg.GlobalCooldown)
	}
	if cfg.UserCooldown < 0 {
		return nil, fmt.Errorf("USER_COOLDOWN_MS must be >= 0, got %v", cfg.UserCooldown)
	}
	if cfg.RoomBufferSize <= 0 {
		return nil, fmt.Errorf("ROOM_BUFFER_SIZE must be > 0, got %d", cfg.RoomBufferSize)
	}
	if cfg.MaxViewerFacts <= 0 {
		return nil, fmt.Errorf("MAX_VIEWER_FACTS must be > 0, got %d", cfg.MaxViewerFacts)
	}

	return cfg, nil
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		env[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return env, nil
}

// SaveEnv writes the configuration to an .env file in a clean, human-readable layout.
func SaveEnv(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("config cannot be nil")
	}
	var b strings.Builder
	b.WriteString("# Language & Localization\n")
	b.WriteString(fmt.Sprintf("BOT_LANGUAGE=%s\n", cfg.BotLanguage))
	b.WriteString(fmt.Sprintf("LOCALE_DIR=%s\n\n", cfg.LocaleDir))

	b.WriteString("# DeepSeek / OpenAI-Compatible LLM\n")
	b.WriteString(fmt.Sprintf("LLM_API_KEY=%s\n", cfg.LLMAPIKey))
	b.WriteString(fmt.Sprintf("LLM_BASE_URL=%s\n", cfg.LLMBaseURL))
	b.WriteString(fmt.Sprintf("LLM_MODEL=%s\n", cfg.LLMModel))
	b.WriteString(fmt.Sprintf("LLM_TIMEOUT_MS=%d\n\n", cfg.LLMTimeout.Milliseconds()))

	b.WriteString("# Twitch Credentials & Accounts\n")
	b.WriteString(fmt.Sprintf("TWITCH_CLIENT_ID=%s\n", cfg.TwitchClientID))
	b.WriteString(fmt.Sprintf("TWITCH_BROADCASTER_LOGIN=%s\n", cfg.TwitchBroadcasterLogin))
	b.WriteString(fmt.Sprintf("TWITCH_BOT_LOGIN=%s\n", cfg.TwitchBotLogin))
	b.WriteString(fmt.Sprintf("TWITCH_ACCESS_TOKEN=%s\n", cfg.TwitchAccessToken))
	b.WriteString(fmt.Sprintf("TWITCH_REFRESH_TOKEN=%s\n", cfg.TwitchRefreshToken))
	b.WriteString(fmt.Sprintf("TWITCH_TOKEN_FILE=%s\n\n", cfg.TokenFile))

	b.WriteString("# Personas & Persistence\n")
	b.WriteString(fmt.Sprintf("PERSONA_DIR=%s\n", cfg.PersonaDir))
	b.WriteString(fmt.Sprintf("PERSONA_FILE=%s\n", cfg.PersonaFile))
	b.WriteString(fmt.Sprintf("STORE_PATH=%s\n\n", cfg.StorePath))

	b.WriteString("# Rate Limiting, Memory & Diagnostics\n")
	b.WriteString(fmt.Sprintf("GLOBAL_COOLDOWN_MS=%d\n", cfg.GlobalCooldown.Milliseconds()))
	b.WriteString(fmt.Sprintf("USER_COOLDOWN_MS=%d\n", cfg.UserCooldown.Milliseconds()))
	b.WriteString(fmt.Sprintf("ROOM_BUFFER_SIZE=%d\n", cfg.RoomBufferSize))
	b.WriteString(fmt.Sprintf("MAX_VIEWER_FACTS=%d\n", cfg.MaxViewerFacts))
	b.WriteString(fmt.Sprintf("ROOM_BUFFER_MAX_AGE_SEC=%d\n", int(cfg.RoomBufferMaxAge.Seconds())))
	b.WriteString(fmt.Sprintf("HTTP_ADDR=%s\n", cfg.HTTPAddr))

	return os.WriteFile(path, []byte(b.String()), 0600)
}
