package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type TokenManagerConfig struct {
	ClientID            string
	AuthBaseURL         string
	TokenFile           string
	InitialAccessToken  string
	InitialRefreshToken string
	HTTPClient          *http.Client
}

type TokenManager struct {
	cfg        TokenManagerConfig
	clock      domain.Clock
	httpClient *http.Client
	mu         sync.Mutex
	tokens     TokenPair
}

func NewTokenManager(cfg TokenManagerConfig, clock domain.Clock) *TokenManager {
	if cfg.AuthBaseURL == "" {
		cfg.AuthBaseURL = "https://id.twitch.tv"
	}
	if cfg.TokenFile == "" {
		cfg.TokenFile = ".tokens.json"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if clock == nil {
		clock = domain.NewRealClock()
	}

	return &TokenManager{
		cfg:        cfg,
		clock:      clock,
		httpClient: cfg.HTTPClient,
		tokens: TokenPair{
			AccessToken:  cfg.InitialAccessToken,
			RefreshToken: cfg.InitialRefreshToken,
		},
	}
}

func (tm *TokenManager) LoadTokens() (*TokenPair, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	data, err := os.ReadFile(tm.cfg.TokenFile)
	if err != nil {
		if os.IsNotExist(err) && (tm.cfg.InitialAccessToken != "" || tm.cfg.InitialRefreshToken != "") {
			tm.tokens = TokenPair{
				AccessToken:  tm.cfg.InitialAccessToken,
				RefreshToken: tm.cfg.InitialRefreshToken,
			}
			pairCopy := tm.tokens
			return &pairCopy, nil
		}
		return nil, fmt.Errorf("could not read token file: %w", err)
	}

	var pair TokenPair
	if err := json.Unmarshal(data, &pair); err != nil {
		return nil, fmt.Errorf("could not unmarshal token file: %w", err)
	}

	tm.tokens = pair
	pairCopy := tm.tokens
	return &pairCopy, nil
}

func (tm *TokenManager) SaveTokens(pair TokenPair) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	data, err := json.MarshalIndent(pair, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	dir := filepath.Dir(tm.cfg.TokenFile)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create token dir: %w", err)
		}
	}

	tmpFile, err := os.CreateTemp(dir, "tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp token file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tokens: %w", err)
	}

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod tokens: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp token file: %w", err)
	}

	// On Windows, rename fails if destination exists, so remove first
	_ = os.Remove(tm.cfg.TokenFile)
	if err := os.Rename(tmpPath, tm.cfg.TokenFile); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace token file: %w", err)
	}

	tm.tokens = pair
	return nil
}

func (tm *TokenManager) StartDeviceFlow(ctx context.Context, scopes []string) (*DeviceFlowResponse, error) {
	u := strings.TrimRight(tm.cfg.AuthBaseURL, "/") + "/oauth2/device"

	data := url.Values{}
	data.Set("client_id", tm.cfg.ClientID)
	data.Set("scopes", strings.Join(scopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build device flow request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device flow request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device flow response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device flow request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var dfr DeviceFlowResponse
	if err := json.Unmarshal(body, &dfr); err != nil {
		return nil, fmt.Errorf("failed to parse device flow response: %w", err)
	}

	return &dfr, nil
}

func (tm *TokenManager) PollDeviceToken(ctx context.Context, deviceCode string, interval time.Duration) (*TokenPair, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	tokenURL := strings.TrimRight(tm.cfg.AuthBaseURL, "/") + "/oauth2/token"

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data := url.Values{}
		data.Set("client_id", tm.cfg.ClientID)
		data.Set("device_code", deviceCode)
		data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, fmt.Errorf("failed to build poll token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := tm.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll token request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read poll response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			var tr struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.Unmarshal(body, &tr); err != nil {
				return nil, fmt.Errorf("failed to decode token response: %w", err)
			}

			expiresAt := tm.clock.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
			pair := TokenPair{
				AccessToken:  tr.AccessToken,
				RefreshToken: tr.RefreshToken,
				ExpiresAt:    expiresAt,
			}
			if err := tm.SaveTokens(pair); err != nil {
				return nil, err
			}
			return &pair, nil
		}

		bodyStr := string(body)
		if resp.StatusCode == http.StatusBadRequest &&
			(strings.Contains(bodyStr, "authorization_pending") || strings.Contains(bodyStr, "slow_down")) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
				continue
			}
		}

		return nil, fmt.Errorf("poll token failed (status %d): %s", resp.StatusCode, bodyStr)
	}
}

func (tm *TokenManager) Refresh(ctx context.Context) (*TokenPair, error) {
	tm.mu.Lock()
	refToken := tm.tokens.RefreshToken
	if refToken == "" {
		refToken = tm.cfg.InitialRefreshToken
	}
	tm.mu.Unlock()

	if refToken == "" {
		return nil, errors.New("no refresh token available")
	}

	tokenURL := strings.TrimRight(tm.cfg.AuthBaseURL, "/") + "/oauth2/token"

	data := url.Values{}
	data.Set("client_id", tm.cfg.ClientID)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build refresh token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("failed to decode refresh response: %w", err)
	}

	newRefreshToken := tr.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refToken
	}

	expiresAt := tm.clock.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	pair := TokenPair{
		AccessToken:  tr.AccessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
	}

	if err := tm.SaveTokens(pair); err != nil {
		return nil, err
	}

	return &pair, nil
}

func (tm *TokenManager) AccessToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	tokens := tm.tokens
	tm.mu.Unlock()

	if tokens.AccessToken == "" {
		loaded, err := tm.LoadTokens()
		if err == nil && loaded != nil {
			tokens = *loaded
		}
	}

	if tokens.AccessToken == "" && (tokens.RefreshToken != "" || tm.cfg.InitialRefreshToken != "") {
		refreshed, err := tm.Refresh(ctx)
		if err != nil {
			return "", err
		}
		return refreshed.AccessToken, nil
	}

	if tokens.AccessToken == "" {
		return "", errors.New("no access token available")
	}

	if !tokens.ExpiresAt.IsZero() && (tokens.RefreshToken != "" || tm.cfg.InitialRefreshToken != "") {
		if tm.clock.Now().Add(60 * time.Second).After(tokens.ExpiresAt) {
			refreshed, err := tm.Refresh(ctx)
			if err != nil {
				return "", err
			}
			return refreshed.AccessToken, nil
		}
	}

	return tokens.AccessToken, nil
}
