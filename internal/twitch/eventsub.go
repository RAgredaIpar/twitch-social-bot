package twitch

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/twitch-social-bot/internal/domain"
)

const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

type EventSubFrame struct {
	MessageType  string
	SessionID    string
	ReconnectURL string
	ChatMessage  *domain.ChatMessage
}

type eventSubMetadataJSON struct {
	MessageID           string `json:"message_id"`
	MessageType         string `json:"message_type"`
	MessageTimestamp    string `json:"message_timestamp"`
	SubscriptionType    string `json:"subscription_type"`
	SubscriptionVersion string `json:"subscription_version"`
}

type eventSubBadgeJSON struct {
	SetID string `json:"set_id"`
	ID    string `json:"id"`
	Info  string `json:"info"`
}

type eventSubChatMessageJSON struct {
	Text string `json:"text"`
}

type eventSubChatEventJSON struct {
	BroadcasterUserID    string                  `json:"broadcaster_user_id"`
	BroadcasterUserLogin string                  `json:"broadcaster_user_login"`
	BroadcasterUserName  string                  `json:"broadcaster_user_name"`
	ChatterUserID        string                  `json:"chatter_user_id"`
	ChatterUserLogin     string                  `json:"chatter_user_login"`
	ChatterUserName      string                  `json:"chatter_user_name"`
	MessageID            string                  `json:"message_id"`
	Message              eventSubChatMessageJSON `json:"message"`
	Badges               []eventSubBadgeJSON     `json:"badges"`
}

type eventSubSessionJSON struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	ReconnectURL *string `json:"reconnect_url"`
}

type eventSubSubscriptionJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

type rawEventSubEnvelope struct {
	Metadata eventSubMetadataJSON `json:"metadata"`
	Payload  struct {
		Session      eventSubSessionJSON      `json:"session"`
		Subscription eventSubSubscriptionJSON `json:"subscription"`
		Event        eventSubChatEventJSON    `json:"event"`
	} `json:"payload"`
}

func ParseEventSubFrame(raw []byte, now time.Time) (messageType string, sessionID string, reconnectURL string, chatMsg *domain.ChatMessage, err error) {
	var env rawEventSubEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", "", "", nil, fmt.Errorf("failed to parse eventsub frame JSON: %w", err)
	}

	messageType = env.Metadata.MessageType

	switch messageType {
	case "session_welcome":
		sessionID = env.Payload.Session.ID

	case "session_keepalive":
		// Keepalive frame received, nothing further to extract

	case "session_reconnect":
		if env.Payload.Session.ReconnectURL != nil {
			reconnectURL = *env.Payload.Session.ReconnectURL
		}

	case "notification":
		subType := env.Payload.Subscription.Type
		if subType == "" {
			subType = env.Metadata.SubscriptionType
		}

		if subType == "channel.chat.message" {
			ev := env.Payload.Event

			ts := now
			if env.Metadata.MessageTimestamp != "" {
				if parsed, pErr := time.Parse(time.RFC3339Nano, env.Metadata.MessageTimestamp); pErr == nil {
					ts = parsed
				} else if parsed, pErr := time.Parse(time.RFC3339, env.Metadata.MessageTimestamp); pErr == nil {
					ts = parsed
				}
			}

			isMod := false
			isBroadcaster := false
			isSub := false
			isVIP := false

			if ev.ChatterUserID != "" && ev.ChatterUserID == ev.BroadcasterUserID {
				isBroadcaster = true
			}

			for _, b := range ev.Badges {
				switch b.SetID {
				case "moderator":
					isMod = true
				case "broadcaster":
					isBroadcaster = true
				case "subscriber", "founder":
					isSub = true
				case "vip":
					isVIP = true
				}
			}

			displayName := ev.ChatterUserName
			if displayName == "" {
				displayName = ev.ChatterUserLogin
			}

			broadcasterName := ev.BroadcasterUserLogin
			if broadcasterName == "" {
				broadcasterName = ev.BroadcasterUserName
			}

			chatMsg = &domain.ChatMessage{
				MessageID:       ev.MessageID,
				BroadcasterID:   ev.BroadcasterUserID,
				BroadcasterName: broadcasterName,
				UserID:          ev.ChatterUserID,
				Username:        ev.ChatterUserLogin,
				DisplayName:     displayName,
				Text:            ev.Message.Text,
				IsModerator:     isMod,
				IsBroadcaster:   isBroadcaster,
				IsSubscriber:    isSub,
				IsVIP:           isVIP,
				Timestamp:       ts,
			}
		}
	}

	return messageType, sessionID, reconnectURL, chatMsg, nil
}

type WSConn struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	closed  bool
}

func DialWebSocket(ctx context.Context, wsURL string) (*WSConn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url: %w", err)
	}

	isTLS := u.Scheme == "wss" || u.Scheme == "https"
	host := u.Host
	if !strings.Contains(host, ":") {
		if isTLS {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var d net.Dialer
	rawConn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("tcp dial failed: %w", err)
	}

	var conn net.Conn = rawConn
	if isTLS {
		serverName := u.Hostname()
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName: serverName,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("tls handshake failed: %w", err)
		}
		conn = tlsConn
	}

	var keyBytes [16]byte
	if _, err := io.ReadFull(rand.Reader, keyBytes[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to generate websocket key: %w", err)
	}
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	reqPath := u.RequestURI()
	if reqPath == "" {
		reqPath = "/"
	}

	upgradeReq := fmt.Sprintf(
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n\r\n",
		reqPath, u.Host, secKey,
	)

	if _, err := conn.Write([]byte(upgradeReq)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to write websocket upgrade: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read websocket handshake response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake failed with status: %d", resp.StatusCode)
	}

	h := sha1.New()
	h.Write([]byte(secKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expectedAccept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	actualAccept := resp.Header.Get("Sec-WebSocket-Accept")
	if actualAccept != "" && actualAccept != expectedAccept {
		_ = conn.Close()
		return nil, fmt.Errorf("Sec-WebSocket-Accept mismatch: expected %s, got %s", expectedAccept, actualAccept)
	}

	return &WSConn{
		conn:   conn,
		reader: reader,
	}, nil
}

func (c *WSConn) ReadFrame() (opcode int, payload []byte, err error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.reader, hdr[:]); err != nil {
		return 0, nil, err
	}

	opcode = int(hdr[0] & 0x0f)
	masked := (hdr[1] & 0x80) != 0
	lenByte := int64(hdr[1] & 0x7f)

	var payloadLen int64
	if lenByte == 126 {
		var l [2]byte
		if _, err := io.ReadFull(c.reader, l[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint16(l[:]))
	} else if lenByte == 127 {
		var l [8]byte
		if _, err := io.ReadFull(c.reader, l[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint64(l[:]))
	} else {
		payloadLen = lenByte
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	return opcode, payload, nil
}

func (c *WSConn) WriteFrame(opcode int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return net.ErrClosed
	}

	var buf []byte
	buf = append(buf, byte(0x80|(opcode&0x0f))) // FIN=1

	n := len(payload)
	// Client-to-server frames MUST be masked (RFC 6455 5.1)
	if n <= 125 {
		buf = append(buf, byte(0x80|n))
	} else if n <= 65535 {
		buf = append(buf, 0x80|126)
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(n))
		buf = append(buf, lenBuf[:]...)
	} else {
		buf = append(buf, 0x80|127)
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(n))
		buf = append(buf, lenBuf[:]...)
	}

	var mask [4]byte
	if _, err := io.ReadFull(rand.Reader, mask[:]); err != nil {
		return fmt.Errorf("failed to generate frame mask: %w", err)
	}
	buf = append(buf, mask[:]...)

	maskedPayload := make([]byte, n)
	for i := range payload {
		maskedPayload[i] = payload[i] ^ mask[i%4]
	}
	buf = append(buf, maskedPayload...)

	_, err := c.conn.Write(buf)
	return err
}

func (c *WSConn) ReadMessage() (opcode int, payload []byte, err error) {
	for {
		op, p, err := c.ReadFrame()
		if err != nil {
			return 0, nil, err
		}

		switch op {
		case OpPing:
			// Auto-respond with Pong
			_ = c.WriteFrame(OpPong, p)
			continue
		case OpPong:
			continue
		case OpClose:
			// Echo close frame
			_ = c.WriteFrame(OpClose, p)
			return OpClose, p, io.EOF
		default:
			return op, p, nil
		}
	}
}

func (c *WSConn) WriteText(text string) error {
	return c.WriteFrame(OpText, []byte(text))
}

func (c *WSConn) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

type EventSubConfig struct {
	WebSocketURL   string
	Clock          domain.Clock
	OnWelcome      func(sessionID string)
	OnChatMessage  func(msg *domain.ChatMessage)
	OnNotification func(msg *domain.ChatMessage)
	OnReconnect    func(reconnectURL string)
	OnKeepalive    func()
}

type EventSubClient struct {
	cfg       EventSubConfig
	conn      *WSConn
	sessionID string
	mu        sync.Mutex
	closed    bool
	cancel    context.CancelFunc
}

func NewEventSubClient(cfg EventSubConfig) *EventSubClient {
	if cfg.WebSocketURL == "" {
		cfg.WebSocketURL = "wss://eventsub.wss.twitch.tv/ws"
	}
	if cfg.Clock == nil {
		cfg.Clock = domain.NewRealClock()
	}
	return &EventSubClient{cfg: cfg}
}

func (esc *EventSubClient) SessionID() string {
	esc.mu.Lock()
	defer esc.mu.Unlock()
	return esc.sessionID
}

func (esc *EventSubClient) Close() error {
	esc.mu.Lock()
	esc.closed = true
	cancel := esc.cancel
	conn := esc.conn
	esc.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (esc *EventSubClient) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	esc.mu.Lock()
	esc.cancel = cancel
	esc.mu.Unlock()
	defer cancel()

	currentURL := esc.cfg.WebSocketURL

	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		default:
		}

		conn, err := DialWebSocket(runCtx, currentURL)
		if err != nil {
			return err
		}

		esc.mu.Lock()
		if esc.closed {
			_ = conn.Close()
			esc.mu.Unlock()
			return nil
		}
		esc.conn = conn
		esc.mu.Unlock()

		reconnectCh := make(chan string, 1)

		readErr := func() error {
			defer conn.Close()
			for {
				select {
				case <-runCtx.Done():
					return runCtx.Err()
				default:
				}

				opcode, payload, err := conn.ReadMessage()
				if err != nil {
					return err
				}

				if opcode == OpText {
					msgType, sessionID, reconnectURL, chatMsg, err := ParseEventSubFrame(payload, esc.cfg.Clock.Now())
					if err != nil {
						continue
					}

					switch msgType {
					case "session_welcome":
						esc.mu.Lock()
						esc.sessionID = sessionID
						esc.mu.Unlock()
						if esc.cfg.OnWelcome != nil {
							esc.cfg.OnWelcome(sessionID)
						}
					case "session_keepalive":
						if esc.cfg.OnKeepalive != nil {
							esc.cfg.OnKeepalive()
						}
					case "session_reconnect":
						if esc.cfg.OnReconnect != nil {
							esc.cfg.OnReconnect(reconnectURL)
						}
						if reconnectURL != "" {
							reconnectCh <- reconnectURL
							return nil
						}
					case "notification":
						if chatMsg != nil {
							if esc.cfg.OnChatMessage != nil {
								esc.cfg.OnChatMessage(chatMsg)
							}
							if esc.cfg.OnNotification != nil {
								esc.cfg.OnNotification(chatMsg)
							}
						}
					}
				}
			}
		}()

		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case newURL := <-reconnectCh:
			currentURL = newURL
			continue
		default:
			esc.mu.Lock()
			isClosed := esc.closed
			esc.mu.Unlock()

			if isClosed || errors.Is(readErr, io.EOF) || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			return nil
		}
	}
}
