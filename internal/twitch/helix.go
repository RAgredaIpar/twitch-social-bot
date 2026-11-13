package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

var _ domain.ChatSender = (*HelixClient)(nil)

type HelixConfig struct {
	ClientID     string
	BaseURL      string
	TokenManager *TokenManager
	HTTPClient   *http.Client
}

type HelixClient struct {
	cfg          HelixConfig
	tokenManager *TokenManager
	httpClient   *http.Client
}

func NewHelixClient(cfg HelixConfig) *HelixClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.twitch.tv/helix"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &HelixClient{
		cfg:          cfg,
		tokenManager: cfg.TokenManager,
		httpClient:   cfg.HTTPClient,
	}
}

type sendReplyRequest struct {
	BroadcasterID        string `json:"broadcaster_id"`
	SenderID             string `json:"sender_id"`
	Message              string `json:"message"`
	ReplyParentMessageID string `json:"reply_parent_message_id,omitempty"`
}

func (h *HelixClient) SendReply(ctx context.Context, broadcasterID, senderID, replyParentID, text string) error {
	payload := sendReplyRequest{
		BroadcasterID:        broadcasterID,
		SenderID:             senderID,
		Message:              text,
		ReplyParentMessageID: replyParentID,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal reply payload: %w", err)
	}

	endpoint := strings.TrimRight(h.cfg.BaseURL, "/") + "/chat/messages"
	return h.doRequestWithRetry(ctx, http.MethodPost, endpoint, bodyBytes, func(statusCode int) bool {
		return statusCode >= 200 && statusCode < 300
	})
}

type createSubRequest struct {
	Type      string                  `json:"type"`
	Version   string                  `json:"version"`
	Condition chatSubConditionPayload `json:"condition"`
	Transport chatSubTransportPayload `json:"transport"`
}

type chatSubConditionPayload struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	UserID            string `json:"user_id"`
}

type chatSubTransportPayload struct {
	Method    string `json:"method"`
	SessionID string `json:"session_id"`
}

func (h *HelixClient) CreateChatSubscription(ctx context.Context, sessionID, broadcasterID, botUserID string) error {
	payload := createSubRequest{
		Type:    "channel.chat.message",
		Version: "1",
		Condition: chatSubConditionPayload{
			BroadcasterUserID: broadcasterID,
			UserID:            botUserID,
		},
		Transport: chatSubTransportPayload{
			Method:    "websocket",
			SessionID: sessionID,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal subscription payload: %w", err)
	}

	endpoint := strings.TrimRight(h.cfg.BaseURL, "/") + "/eventsub/subscriptions"
	return h.doRequestWithRetry(ctx, http.MethodPost, endpoint, bodyBytes, func(statusCode int) bool {
		return statusCode >= 200 && statusCode < 300
	})
}

func (h *HelixClient) ResolveUsers(ctx context.Context, logins []string) (map[string]string, error) {
	if len(logins) == 0 {
		return map[string]string{}, nil
	}

	values := url.Values{}
	for _, login := range logins {
		trimmed := strings.TrimSpace(login)
		if trimmed != "" {
			values.Add("login", trimmed)
		}
	}

	endpoint := strings.TrimRight(h.cfg.BaseURL, "/") + "/users?" + values.Encode()

	var responseData struct {
		Data []struct {
			ID          string `json:"id"`
			Login       string `json:"login"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}

	err := h.doRequestWithRetry(ctx, http.MethodGet, endpoint, nil, func(statusCode int) bool {
		return statusCode == http.StatusOK
	}, &responseData)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(responseData.Data))
	for _, u := range responseData.Data {
		result[strings.ToLower(u.Login)] = u.ID
	}

	return result, nil
}

// GetChannelInfo fetches the current category/game and stream title for broadcasterID.
func (h *HelixClient) GetChannelInfo(ctx context.Context, broadcasterID string) (domain.ChannelInfo, error) {
	if strings.TrimSpace(broadcasterID) == "" {
		return domain.ChannelInfo{}, nil
	}
	endpoint := strings.TrimRight(h.cfg.BaseURL, "/") + "/channels?broadcaster_id=" + url.QueryEscape(broadcasterID)

	var responseData struct {
		Data []struct {
			GameName string `json:"game_name"`
			Title    string `json:"title"`
		} `json:"data"`
	}

	err := h.doRequestWithRetry(ctx, http.MethodGet, endpoint, nil, func(statusCode int) bool {
		return statusCode == http.StatusOK
	}, &responseData)
	if err != nil {
		return domain.ChannelInfo{}, err
	}
	if len(responseData.Data) == 0 {
		return domain.ChannelInfo{}, nil
	}
	return domain.ChannelInfo{
		GameName: responseData.Data[0].GameName,
		Title:    responseData.Data[0].Title,
	}, nil
}

func (h *HelixClient) doRequestWithRetry(
	ctx context.Context,
	method string,
	endpoint string,
	body []byte,
	isSuccess func(int) bool,
	out ...interface{},
) error {
	token := ""
	if h.tokenManager != nil {
		var err error
		token, err = h.tokenManager.AccessToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get access token: %w", err)
		}
	}

	execute := func(tok string) (int, []byte, error) {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Client-Id", h.cfg.ClientID)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := h.httpClient.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("http request failed: %w", err)
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		return resp.StatusCode, respBytes, nil
	}

	statusCode, respBytes, err := execute(token)
	if err != nil {
		return err
	}

	// 401 Unauthorized retry logic
	if statusCode == http.StatusUnauthorized && h.tokenManager != nil {
		_, refErr := h.tokenManager.Refresh(ctx)
		if refErr == nil {
			newToken, tokErr := h.tokenManager.AccessToken(ctx)
			if tokErr == nil {
				statusCode, respBytes, err = execute(newToken)
				if err != nil {
					return err
				}
			}
		}
	}

	if !isSuccess(statusCode) {
		return fmt.Errorf("helix request to %s failed (status %d): %s", endpoint, statusCode, string(respBytes))
	}

	if len(out) > 0 && out[0] != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out[0]); err != nil {
			return fmt.Errorf("failed to unmarshal helix response: %w", err)
		}
	}

	return nil
}
