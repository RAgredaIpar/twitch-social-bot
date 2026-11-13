package domain

import (
	"context"
	"time"
)

type ChatMessage struct {
	MessageID       string    `json:"message_id"`
	BroadcasterID   string    `json:"broadcaster_id"`
	BroadcasterName string    `json:"broadcaster_name"`
	UserID          string    `json:"user_id"`
	Username        string    `json:"username"`
	DisplayName     string    `json:"display_name"`
	Text            string    `json:"text"`
	IsModerator     bool      `json:"is_moderator"`
	IsBroadcaster   bool      `json:"is_broadcaster"`
	IsSubscriber    bool      `json:"is_subscriber"`
	IsVIP           bool      `json:"is_vip"`
	Timestamp       time.Time `json:"timestamp"`
}

type RoomMessage struct {
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Text        string    `json:"text"`
	Timestamp   time.Time `json:"timestamp"`
}

type ConversationTurn struct {
	UserMessage string    `json:"user_message"`
	BotReply    string    `json:"bot_reply"`
	Timestamp   time.Time `json:"timestamp"`
}

type ViewerProfile struct {
	UserID           string             `json:"user_id"`
	Username         string             `json:"username"`
	DisplayName      string             `json:"display_name"`
	Affinity         int                `json:"affinity"`
	TierID           string             `json:"tier_id"`
	EarnedTitle      string             `json:"earned_title"`
	Facts            []string           `json:"facts"`
	RecentTurns      []ConversationTurn `json:"recent_turns"`
	InteractionCount int                `json:"interaction_count"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type TierSpec struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Behavior string `json:"behavior"`
}

type SlotSpec struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	MaxHolders        int    `json:"max_holders"`
	MinAffinity       *int   `json:"min_affinity,omitempty"`
	MaxAffinity       *int   `json:"max_affinity,omitempty"`
	AllowOutrankSteal bool   `json:"allow_outrank_steal"`
}

type SlotHolder struct {
	SlotID      string    `json:"slot_id"`
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Affinity    int       `json:"affinity"`
	AssignedAt  time.Time `json:"assigned_at"`
}

type PersonaSpec struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	BotName          string     `json:"bot_name"`
	TriggerPrefix    string     `json:"trigger_prefix"`
	MaxPositiveDelta int        `json:"max_positive_delta"`
	MaxNegativeDelta int        `json:"max_negative_delta"`
	BasePersona      string     `json:"base_persona"`
	BlockedWords     []string   `json:"blocked_words"`
	MaxViewerFacts   int        `json:"max_viewer_facts,omitempty"`
	Tiers            []TierSpec `json:"tiers"`
	Slots            []SlotSpec `json:"slots"`
}

type LLMDecision struct {
	Reply         string `json:"reply"`
	AffinityDelta int    `json:"affinity_delta"`
	NewTitle      string `json:"new_title,omitempty"`
	NewFact       string `json:"new_fact,omitempty"`
	SlotClaim     string `json:"slot_claim,omitempty"`
}

type PromptInput struct {
	Persona       *PersonaSpec            `json:"persona"`
	Profile       ViewerProfile           `json:"profile"`
	CurrentTier   TierSpec                `json:"current_tier"`
	ActiveSlots   map[string][]SlotHolder `json:"active_slots"`
	RecentRoom    []RoomMessage           `json:"recent_room"`
	ViewerMessage string                  `json:"viewer_message"`
	ViewerRole    string                  `json:"viewer_role,omitempty"`
	CurrentMood   string                  `json:"current_mood,omitempty"`
	StreamGame    string                  `json:"stream_game,omitempty"`
	StreamTitle   string                  `json:"stream_title,omitempty"`
}

type ChannelInfo struct {
	GameName string `json:"game_name"`
	Title    string `json:"title"`
}

type ChannelInfoFetcher interface {
	GetChannelInfo(ctx context.Context, broadcasterID string) (ChannelInfo, error)
}
