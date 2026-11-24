package twitch_test

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/twitch"
)

// ---------------------------------------------------------------------------
// 1. OAuth & Persistence Tests
// ---------------------------------------------------------------------------

func TestTokenManager_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	tokenFile := filepath.Join(tempDir, "tokens.json")
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	cfg := twitch.TokenManagerConfig{
		ClientID:            "test_client_id",
		TokenFile:           tokenFile,
		InitialAccessToken:  "initial_acc",
		InitialRefreshToken: "initial_ref",
	}

	tm := twitch.NewTokenManager(cfg, clock)

	// File doesn't exist yet: LoadTokens should fall back to initial tokens
	tokens, err := tm.LoadTokens()
	if err != nil {
		t.Fatalf("expected fallback tokens, got error: %v", err)
	}
	if tokens.AccessToken != "initial_acc" || tokens.RefreshToken != "initial_ref" {
		t.Fatalf("unexpected fallback tokens: %+v", tokens)
	}

	// Save new tokens
	pair := twitch.TokenPair{
		AccessToken:  "new_acc",
		RefreshToken: "new_ref",
		ExpiresAt:    clock.Now().Add(4 * time.Hour),
	}
	if err := tm.SaveTokens(pair); err != nil {
		t.Fatalf("failed to save tokens: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("failed to read written token file: %v", err)
	}
	var loaded twitch.TokenPair
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal token json: %v", err)
	}
	if loaded.AccessToken != "new_acc" || loaded.RefreshToken != "new_ref" {
		t.Fatalf("loaded tokens mismatch: %+v", loaded)
	}

	// New manager loading the same file
	tm2 := twitch.NewTokenManager(cfg, clock)
	loaded2, err := tm2.LoadTokens()
	if err != nil {
		t.Fatalf("failed to load tokens from existing file: %v", err)
	}
	if loaded2.AccessToken != "new_acc" || loaded2.RefreshToken != "new_ref" {
		t.Fatalf("loaded2 tokens mismatch: %+v", loaded2)
	}
}

func TestTokenManager_StartDeviceFlow(t *testing.T) {
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	var requestedClientID, requestedScopes string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/device" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "invalid method", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		requestedClientID = r.Form.Get("client_id")
		requestedScopes = r.Form.Get("scopes")

		resp := twitch.DeviceFlowResponse{
			DeviceCode:      "dev_12345",
			UserCode:        "WXYZ-6789",
			VerificationURI: "https://www.twitch.tv/activate",
			ExpiresIn:       1800,
			Interval:        5,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := twitch.TokenManagerConfig{
		ClientID:    "twitch_app_123",
		AuthBaseURL: server.URL,
		HTTPClient:  server.Client(),
	}
	tm := twitch.NewTokenManager(cfg, clock)

	res, err := tm.StartDeviceFlow(context.Background(), []string{"user:read:chat", "user:write:chat"})
	if err != nil {
		t.Fatalf("StartDeviceFlow failed: %v", err)
	}

	if requestedClientID != "twitch_app_123" {
		t.Errorf("expected client_id twitch_app_123, got %s", requestedClientID)
	}
	if requestedScopes != "user:read:chat user:write:chat" {
		t.Errorf("expected space-joined scopes, got %s", requestedScopes)
	}
	if res.DeviceCode != "dev_12345" || res.UserCode != "WXYZ-6789" {
		t.Errorf("unexpected response: %+v", res)
	}
}

func TestTokenManager_PollDeviceToken(t *testing.T) {
	tempDir := t.TempDir()
	tokenFile := filepath.Join(tempDir, "tokens.json")
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}

		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status": 400, "message": "authorization_pending"}`))
			return
		}
		if attempts == 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status": 400, "message": "slow_down"}`))
			return
		}

		// Third attempt: success
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "acc_polled_999",
			"refresh_token": "ref_polled_888",
			"expires_in": 3600,
			"scope": ["user:read:chat"],
			"token_type": "bearer"
		}`))
	}))
	defer server.Close()

	cfg := twitch.TokenManagerConfig{
		ClientID:    "twitch_app_123",
		AuthBaseURL: server.URL,
		TokenFile:   tokenFile,
		HTTPClient:  server.Client(),
	}
	tm := twitch.NewTokenManager(cfg, clock)

	pair, err := tm.PollDeviceToken(context.Background(), "dev_12345", 5*time.Millisecond)
	if err != nil {
		t.Fatalf("PollDeviceToken failed: %v", err)
	}
	if pair.AccessToken != "acc_polled_999" || pair.RefreshToken != "ref_polled_888" {
		t.Fatalf("unexpected pair: %+v", pair)
	}
	expectedExpiry := clock.Now().Add(3600 * time.Second)
	if !pair.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected expiry %v, got %v", expectedExpiry, pair.ExpiresAt)
	}

	// Verify it was saved to disk
	loaded, err := tm.LoadTokens()
	if err != nil {
		t.Fatalf("failed to load saved tokens: %v", err)
	}
	if loaded.AccessToken != "acc_polled_999" {
		t.Errorf("saved token mismatch: %+v", loaded)
	}
}

func TestTokenManager_AutomaticRefresh(t *testing.T) {
	tempDir := t.TempDir()
	tokenFile := filepath.Join(tempDir, "tokens.json")
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	refreshCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}
		if r.Form.Get("refresh_token") != "ref_valid_1" {
			http.Error(w, "invalid refresh_token", http.StatusBadRequest)
			return
		}

		refreshCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "acc_refreshed_2",
			"refresh_token": "ref_valid_1",
			"expires_in": 7200,
			"scope": ["user:read:chat"]
		}`))
	}))
	defer server.Close()

	cfg := twitch.TokenManagerConfig{
		ClientID:    "twitch_app_123",
		AuthBaseURL: server.URL,
		TokenFile:   tokenFile,
		HTTPClient:  server.Client(),
	}
	tm := twitch.NewTokenManager(cfg, clock)

	// Save initial token expiring in 10 minutes
	initialExpiry := clock.Now().Add(10 * time.Minute)
	_ = tm.SaveTokens(twitch.TokenPair{
		AccessToken:  "acc_valid_1",
		RefreshToken: "ref_valid_1",
		ExpiresAt:    initialExpiry,
	})

	// 1. Token is still valid (not within 60s of expiration)
	tok, err := tm.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken failed: %v", err)
	}
	if tok != "acc_valid_1" {
		t.Errorf("expected acc_valid_1, got %s", tok)
	}
	if refreshCalls != 0 {
		t.Errorf("expected 0 refresh calls, got %d", refreshCalls)
	}

	// 2. Advance clock so token is within 30 seconds of expiration (< 60s)
	clock.Advance(9*time.Minute + 35*time.Second) // 25s left

	tokRefreshed, err := tm.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken with refresh failed: %v", err)
	}
	if tokRefreshed != "acc_refreshed_2" {
		t.Errorf("expected acc_refreshed_2, got %s", tokRefreshed)
	}
	if refreshCalls != 1 {
		t.Errorf("expected 1 refresh call, got %d", refreshCalls)
	}
}

// ---------------------------------------------------------------------------
// 2. Helix API Client Tests
// ---------------------------------------------------------------------------

func TestHelixClient_SendReply(t *testing.T) {
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	var receivedBody map[string]interface{}
	var receivedAuth, receivedClientID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/messages" && r.Method == http.MethodPost {
			receivedAuth = r.Header.Get("Authorization")
			receivedClientID = r.Header.Get("Client-Id")
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"message_id": "sent_123", "is_sent": true}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	tm := twitch.NewTokenManager(twitch.TokenManagerConfig{
		ClientID:           "helix_client_id",
		TokenFile:          filepath.Join(t.TempDir(), "tokens.json"),
		InitialAccessToken: "helix_token_abc",
	}, clock)

	helix := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID:     "helix_client_id",
		BaseURL:      server.URL,
		TokenManager: tm,
		HTTPClient:   server.Client(),
	})

	// Case A: Reply with reply_parent_message_id
	err := helix.SendReply(context.Background(), "broadcaster_1", "bot_2", "parent_msg_99", "Hola chat!")
	if err != nil {
		t.Fatalf("SendReply failed: %v", err)
	}
	if receivedAuth != "Bearer helix_token_abc" {
		t.Errorf("expected Bearer helix_token_abc, got %s", receivedAuth)
	}
	if receivedClientID != "helix_client_id" {
		t.Errorf("expected helix_client_id, got %s", receivedClientID)
	}
	if receivedBody["reply_parent_message_id"] != "parent_msg_99" {
		t.Errorf("expected reply_parent_message_id parent_msg_99, got %v", receivedBody["reply_parent_message_id"])
	}
	if receivedBody["message"] != "Hola chat!" {
		t.Errorf("expected message 'Hola chat!', got %v", receivedBody["message"])
	}

	// Case B: Send message without replyParentID (must omit reply_parent_message_id)
	receivedBody = nil
	err = helix.SendReply(context.Background(), "broadcaster_1", "bot_2", "", "Mensaje general")
	if err != nil {
		t.Fatalf("SendReply without parent failed: %v", err)
	}
	if _, exists := receivedBody["reply_parent_message_id"]; exists {
		t.Errorf("expected reply_parent_message_id to be omitted, got %v", receivedBody["reply_parent_message_id"])
	}
}

func TestHelixClient_SendReply_401RefreshRetry(t *testing.T) {
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	sendAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/messages":
			sendAttempts++
			if sendAttempts == 1 {
				// Expired token: return 401
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "Unauthorized", "status": 401, "message": "Invalid OAuth token"}`))
				return
			}
			// Verify retried with new token
			auth := r.Header.Get("Authorization")
			if auth != "Bearer token_refreshed_xyz" {
				http.Error(w, "unexpected token", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"message_id": "ok_123", "is_sent": true}]}`))

		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"access_token": "token_refreshed_xyz",
				"refresh_token": "ref_valid_123",
				"expires_in": 3600
			}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tm := twitch.NewTokenManager(twitch.TokenManagerConfig{
		ClientID:            "client_retry_id",
		AuthBaseURL:         server.URL,
		TokenFile:           filepath.Join(t.TempDir(), "tokens.json"),
		InitialAccessToken:  "token_old",
		InitialRefreshToken: "ref_valid_123",
		HTTPClient:          server.Client(),
	}, clock)

	helix := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID:     "client_retry_id",
		BaseURL:      server.URL,
		TokenManager: tm,
		HTTPClient:   server.Client(),
	})

	err := helix.SendReply(context.Background(), "b1", "bot1", "p1", "reintento exitoso")
	if err != nil {
		t.Fatalf("SendReply failed after 401 retry: %v", err)
	}
	if sendAttempts != 2 {
		t.Errorf("expected 2 send attempts, got %d", sendAttempts)
	}
}

func TestHelixClient_CreateChatSubscription(t *testing.T) {
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	var subBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eventsub/subscriptions" && r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&subBody)
			w.WriteHeader(http.StatusAccepted) // 202 Accepted
			_, _ = w.Write([]byte(`{"data": [{"id": "sub_123", "status": "enabled"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	tm := twitch.NewTokenManager(twitch.TokenManagerConfig{
		ClientID:           "client_eventsub",
		TokenFile:          filepath.Join(t.TempDir(), "tokens.json"),
		InitialAccessToken: "token_eventsub",
	}, clock)

	helix := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID:     "client_eventsub",
		BaseURL:      server.URL,
		TokenManager: tm,
		HTTPClient:   server.Client(),
	})

	err := helix.CreateChatSubscription(context.Background(), "session_ws_99", "streamer_id", "bot_id")
	if err != nil {
		t.Fatalf("CreateChatSubscription failed: %v", err)
	}

	if subBody["type"] != "channel.chat.message" {
		t.Errorf("expected type channel.chat.message, got %v", subBody["type"])
	}
	transport, ok := subBody["transport"].(map[string]interface{})
	if !ok || transport["session_id"] != "session_ws_99" || transport["method"] != "websocket" {
		t.Errorf("unexpected transport: %v", transport)
	}
	condition, ok := subBody["condition"].(map[string]interface{})
	if !ok || condition["broadcaster_user_id"] != "streamer_id" || condition["user_id"] != "bot_id" {
		t.Errorf("unexpected condition: %v", condition)
	}
}

func TestHelixClient_ResolveUsers(t *testing.T) {
	clock := domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users" && r.Method == http.MethodGet {
			queryLogins := r.URL.Query()["login"]
			if len(queryLogins) != 2 {
				http.Error(w, "expected 2 logins", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"data": [
					{"id": "111", "login": "StreamerOne", "display_name": "StreamerOne"},
					{"id": "222", "login": "mybot", "display_name": "MyBot"}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	tm := twitch.NewTokenManager(twitch.TokenManagerConfig{
		ClientID:           "client_resolve",
		TokenFile:          filepath.Join(t.TempDir(), "tokens.json"),
		InitialAccessToken: "token_resolve",
	}, clock)

	helix := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID:     "client_resolve",
		BaseURL:      server.URL,
		TokenManager: tm,
		HTTPClient:   server.Client(),
	})

	resolved, err := helix.ResolveUsers(context.Background(), []string{"streamerone", "mybot"})
	if err != nil {
		t.Fatalf("ResolveUsers failed: %v", err)
	}

	if resolved["streamerone"] != "111" {
		t.Errorf("expected streamerone -> 111, got %s", resolved["streamerone"])
	}
	if resolved["mybot"] != "222" {
		t.Errorf("expected mybot -> 222, got %s", resolved["mybot"])
	}
}

// ---------------------------------------------------------------------------
// 3. EventSub WebSocket & Parsing Tests
// ---------------------------------------------------------------------------

func TestParseEventSubFrame(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	t.Run("session_welcome", func(t *testing.T) {
		raw := []byte(`{
			"metadata": {
				"message_id": "msg_w1",
				"message_type": "session_welcome",
				"message_timestamp": "2026-11-12T12:00:00Z"
			},
			"payload": {
				"session": {
					"id": "sess_welcome_999",
					"status": "connected",
					"connected_at": "2026-11-12T12:00:00Z",
					"keepalive_timeout_seconds": 10
				}
			}
		}`)

		msgType, sessionID, reconnectURL, chatMsg, err := twitch.ParseEventSubFrame(raw, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msgType != "session_welcome" {
			t.Errorf("expected session_welcome, got %s", msgType)
		}
		if sessionID != "sess_welcome_999" {
			t.Errorf("expected sess_welcome_999, got %s", sessionID)
		}
		if reconnectURL != "" || chatMsg != nil {
			t.Errorf("expected empty reconnectURL and nil chatMsg")
		}
	})

	t.Run("session_keepalive", func(t *testing.T) {
		raw := []byte(`{
			"metadata": {
				"message_id": "msg_k1",
				"message_type": "session_keepalive",
				"message_timestamp": "2026-11-12T12:00:10Z"
			},
			"payload": {}
		}`)

		msgType, sessionID, reconnectURL, chatMsg, err := twitch.ParseEventSubFrame(raw, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msgType != "session_keepalive" {
			t.Errorf("expected session_keepalive, got %s", msgType)
		}
		if sessionID != "" || reconnectURL != "" || chatMsg != nil {
			t.Errorf("expected empty fields for keepalive")
		}
	})

	t.Run("session_reconnect", func(t *testing.T) {
		raw := []byte(`{
			"metadata": {
				"message_id": "msg_r1",
				"message_type": "session_reconnect",
				"message_timestamp": "2026-11-12T12:01:00Z"
			},
			"payload": {
				"session": {
					"id": "sess_old_1",
					"reconnect_url": "wss://eventsub.wss.twitch.tv/ws?id=sess_old_1"
				}
			}
		}`)

		msgType, _, reconnectURL, _, err := twitch.ParseEventSubFrame(raw, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msgType != "session_reconnect" {
			t.Errorf("expected session_reconnect, got %s", msgType)
		}
		if reconnectURL != "wss://eventsub.wss.twitch.tv/ws?id=sess_old_1" {
			t.Errorf("unexpected reconnectURL: %s", reconnectURL)
		}
	})

	t.Run("notification_chat_message", func(t *testing.T) {
		raw := []byte(`{
			"metadata": {
				"message_id": "event_msg_42",
				"message_type": "notification",
				"message_timestamp": "2026-11-12T12:00:05.123Z",
				"subscription_type": "channel.chat.message"
			},
			"payload": {
				"subscription": {
					"type": "channel.chat.message",
					"version": "1"
				},
				"event": {
					"broadcaster_user_id": "broadcaster_888",
					"broadcaster_user_login": "streamer",
					"broadcaster_user_name": "Streamer",
					"chatter_user_id": "mod_777",
					"chatter_user_login": "moderator_guy",
					"chatter_user_name": "ModeratorGuy",
					"message_id": "chat_msg_id_101",
					"message": {
						"text": "!bot hola persona"
					},
					"badges": [
						{"set_id": "moderator", "id": "1", "info": ""},
						{"set_id": "subscriber", "id": "12", "info": "12"}
					]
				}
			}
		}`)

		msgType, _, _, chatMsg, err := twitch.ParseEventSubFrame(raw, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msgType != "notification" {
			t.Fatalf("expected notification, got %s", msgType)
		}
		if chatMsg == nil {
			t.Fatal("expected chatMsg to be non-nil")
		}

		if chatMsg.MessageID != "chat_msg_id_101" {
			t.Errorf("expected MessageID chat_msg_id_101, got %s", chatMsg.MessageID)
		}
		if chatMsg.BroadcasterID != "broadcaster_888" {
			t.Errorf("expected BroadcasterID broadcaster_888, got %s", chatMsg.BroadcasterID)
		}
		if chatMsg.BroadcasterName != "streamer" {
			t.Errorf("expected BroadcasterName streamer, got %s", chatMsg.BroadcasterName)
		}
		if chatMsg.UserID != "mod_777" {
			t.Errorf("expected UserID mod_777, got %s", chatMsg.UserID)
		}
		if chatMsg.Username != "moderator_guy" {
			t.Errorf("expected Username moderator_guy, got %s", chatMsg.Username)
		}
		if chatMsg.DisplayName != "ModeratorGuy" {
			t.Errorf("expected DisplayName ModeratorGuy, got %s", chatMsg.DisplayName)
		}
		if chatMsg.Text != "!bot hola persona" {
			t.Errorf("expected Text '!bot hola persona', got %s", chatMsg.Text)
		}
		if !chatMsg.IsModerator {
			t.Errorf("expected IsModerator = true")
		}
		if chatMsg.IsBroadcaster {
			t.Errorf("expected IsBroadcaster = false")
		}
	})

	t.Run("notification_broadcaster_badge_and_id", func(t *testing.T) {
		raw := []byte(`{
			"metadata": {
				"message_id": "event_bcast_1",
				"message_type": "notification",
				"subscription_type": "channel.chat.message"
			},
			"payload": {
				"subscription": {"type": "channel.chat.message"},
				"event": {
					"broadcaster_user_id": "streamer_id_999",
					"broadcaster_user_login": "streamer",
					"broadcaster_user_name": "Streamer",
					"chatter_user_id": "streamer_id_999",
					"chatter_user_login": "streamer",
					"chatter_user_name": "Streamer",
					"message_id": "m1",
					"message": {"text": "!bot pausa"},
					"badges": [{"set_id": "broadcaster", "id": "1", "info": ""}]
				}
			}
		}`)

		_, _, _, chatMsg, err := twitch.ParseEventSubFrame(raw, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !chatMsg.IsBroadcaster {
			t.Errorf("expected IsBroadcaster = true for broadcaster badge/id")
		}
	})
}

// ---------------------------------------------------------------------------
// 4. Minimal RFC 6455 WebSocket Client & Server Exchange Test
// ---------------------------------------------------------------------------

// Helper to encode an unmasked server-to-client RFC 6455 frame
func encodeServerFrame(opcode int, payload []byte) []byte {
	var buf []byte
	buf = append(buf, byte(0x80|(opcode&0x0f))) // FIN=1
	n := len(payload)
	if n <= 125 {
		buf = append(buf, byte(n))
	} else if n <= 65535 {
		buf = append(buf, 126)
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(n))
		buf = append(buf, lenBuf[:]...)
	} else {
		buf = append(buf, 127)
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(n))
		buf = append(buf, lenBuf[:]...)
	}
	buf = append(buf, payload...)
	return buf
}

// Helper to read a masked client-to-server RFC 6455 frame on the server side
func readClientFrame(r *bufio.Reader) (opcode int, payload []byte, err error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode = int(hdr[0] & 0x0f)
	masked := (hdr[1] & 0x80) != 0
	lenByte := int64(hdr[1] & 0x7f)
	var payloadLen int64
	if lenByte == 126 {
		var l [2]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint16(l[:]))
	} else if lenByte == 127 {
		var l [8]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint64(l[:]))
	} else {
		payloadLen = lenByte
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func TestDialWebSocket_FrameExchange(t *testing.T) {
	// Setup standard library RFC 6455 mock WebSocket server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	serverErrCh := make(chan error, 1)
	pongReceivedCh := make(chan string, 1)

	go func() {
		rawConn, err := listener.Accept()
		if err != nil {
			serverErrCh <- err
			return
		}
		defer rawConn.Close()

		reader := bufio.NewReader(rawConn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			serverErrCh <- fmt.Errorf("read request error: %w", err)
			return
		}

		key := req.Header.Get("Sec-WebSocket-Key")
		if key == "" {
			serverErrCh <- fmt.Errorf("missing Sec-WebSocket-Key")
			return
		}

		// Compute Sec-WebSocket-Accept
		h := sha1.New()
		h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

		// Send 101 Switching Protocols
		upgradeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"
		if _, err := rawConn.Write([]byte(upgradeResp)); err != nil {
			serverErrCh <- err
			return
		}

		// 1. Send Ping frame
		pingPayload := []byte("heartbeat-ping-123")
		if _, err := rawConn.Write(encodeServerFrame(0x9, pingPayload)); err != nil {
			serverErrCh <- err
			return
		}

		// Read Pong response from client
		op, pongData, err := readClientFrame(reader)
		if err != nil {
			serverErrCh <- fmt.Errorf("error reading pong: %w", err)
			return
		}
		if op != 0xA {
			serverErrCh <- fmt.Errorf("expected Pong opcode 0xA, got 0x%X", op)
			return
		}
		pongReceivedCh <- string(pongData)

		// 2. Send Text frame with session_welcome
		welcomeJSON := `{"metadata":{"message_id":"m1","message_type":"session_welcome"},"payload":{"session":{"id":"sess_mock_777"}}}`
		if _, err := rawConn.Write(encodeServerFrame(0x1, []byte(welcomeJSON))); err != nil {
			serverErrCh <- err
			return
		}

		// 3. Send Text frame with notification
		notifJSON := `{"metadata":{"message_id":"m2","message_type":"notification","subscription_type":"channel.chat.message"},"payload":{"subscription":{"type":"channel.chat.message"},"event":{"broadcaster_user_id":"b1","broadcaster_user_login":"streamer","chatter_user_id":"u1","chatter_user_login":"user1","chatter_user_name":"User1","message_id":"msg_test","message":{"text":"hola websocket"},"badges":[]}}}`
		if _, err := rawConn.Write(encodeServerFrame(0x1, []byte(notifJSON))); err != nil {
			serverErrCh <- err
			return
		}

		// 4. Send Close frame
		if _, err := rawConn.Write(encodeServerFrame(0x8, []byte{0x03, 0xE8})); err != nil { // 1000 normal closure
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	wsURL := "ws://" + listener.Addr().String() + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := twitch.DialWebSocket(ctx, wsURL)
	if err != nil {
		t.Fatalf("DialWebSocket failed: %v", err)
	}
	defer conn.Close()

	// Read Message 1: Client should have handled ping transparently and return welcome frame
	op1, payload1, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read welcome message: %v", err)
	}
	if op1 != 0x1 {
		t.Errorf("expected text frame op 0x1, got 0x%X", op1)
	}
	if !strings.Contains(string(payload1), "session_welcome") {
		t.Errorf("expected welcome frame, got %s", string(payload1))
	}

	// Verify server received pong
	select {
	case pongPayload := <-pongReceivedCh:
		if pongPayload != "heartbeat-ping-123" {
			t.Errorf("expected heartbeat-ping-123 pong payload, got %s", pongPayload)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for auto-pong from client")
	}

	// Read Message 2: notification
	op2, payload2, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read notification message: %v", err)
	}
	if op2 != 0x1 {
		t.Errorf("expected text frame op 0x1, got 0x%X", op2)
	}
	if !strings.Contains(string(payload2), "hola websocket") {
		t.Errorf("expected chat text in payload, got %s", string(payload2))
	}

	// Read Message 3: Close frame should yield EOF or net.ErrClosed
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Errorf("expected EOF or close error when reading closed connection, got nil")
	}

	if sErr := <-serverErrCh; sErr != nil {
		t.Fatalf("server encountered error: %v", sErr)
	}
}

func TestEventSubClient_Integration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		rawConn, err := listener.Accept()
		if err != nil {
			return
		}
		defer rawConn.Close()

		reader := bufio.NewReader(rawConn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}

		key := req.Header.Get("Sec-WebSocket-Key")
		h := sha1.New()
		h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

		upgradeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"
		_, _ = rawConn.Write([]byte(upgradeResp))

		// Welcome
		welcomeJSON := `{"metadata":{"message_id":"m1","message_type":"session_welcome"},"payload":{"session":{"id":"sess_integration_42"}}}`
		_, _ = rawConn.Write(encodeServerFrame(0x1, []byte(welcomeJSON)))

		// Notification
		notifJSON := `{"metadata":{"message_id":"m2","message_type":"notification","subscription_type":"channel.chat.message"},"payload":{"subscription":{"type":"channel.chat.message"},"event":{"broadcaster_user_id":"b1","broadcaster_user_login":"streamer","chatter_user_id":"u99","chatter_user_login":"viewer99","chatter_user_name":"Viewer99","message_id":"msg_int_1","message":{"text":"!bot test eventsub"},"badges":[]}}}`
		_, _ = rawConn.Write(encodeServerFrame(0x1, []byte(notifJSON)))

		// Close
		_, _ = rawConn.Write(encodeServerFrame(0x8, []byte{0x03, 0xE8}))
	}()

	var welcomeSessionID string
	var receivedChat *domain.ChatMessage
	var wg sync.WaitGroup
	wg.Add(2)

	cfg := twitch.EventSubConfig{
		WebSocketURL: "ws://" + listener.Addr().String() + "/eventsub",
		Clock:        domain.NewFakeClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)),
		OnWelcome: func(sessionID string) {
			welcomeSessionID = sessionID
			wg.Done()
		},
		OnChatMessage: func(msg *domain.ChatMessage) {
			receivedChat = msg
			wg.Done()
		},
	}

	esc := twitch.NewEventSubClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientErrCh := make(chan error, 1)
	go func() {
		clientErrCh <- esc.Run(ctx)
	}()

	wg.Wait()

	if welcomeSessionID != "sess_integration_42" {
		t.Errorf("expected session sess_integration_42, got %s", welcomeSessionID)
	}
	if receivedChat == nil || receivedChat.Text != "!bot test eventsub" {
		t.Errorf("unexpected chat message: %+v", receivedChat)
	}

	_ = esc.Close()
}

func TestHelixClient_SendTrackedReply_ReturnsMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"message_id":"msg-uuid-999","is_sent":true}]}`))
	}))
	defer srv.Close()

	cli := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID:   "cid",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	msgID, err := cli.SendTrackedReply(context.Background(), "b1", "s1", "p1", "hola")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgID != "msg-uuid-999" {
		t.Fatalf("expected msg-uuid-999, got %q", msgID)
	}
}
