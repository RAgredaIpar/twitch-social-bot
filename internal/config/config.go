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

	if cfg.LLMAPIKey == "" {
		return nil, errors.New("LLM_API_KEY is required and cannot be empty")
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
