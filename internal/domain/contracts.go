package domain

import "context"

type Store interface {
	GetProfile(ctx context.Context, userID string) (ViewerProfile, bool, error)
	SaveProfile(ctx context.Context, profile ViewerProfile) error
	ResetProfile(ctx context.Context, userID string) error
	FindProfileByUsername(ctx context.Context, username string) (ViewerProfile, bool, error)
	GetSlots(ctx context.Context, personaID string) (map[string][]SlotHolder, error)
	SaveSlots(ctx context.Context, personaID string, slots map[string][]SlotHolder) error
	Close() error
}

type LLMClient interface {
	Generate(ctx context.Context, input PromptInput) (LLMDecision, error)
}

type ChatSender interface {
	SendReply(ctx context.Context, broadcasterID, senderID, replyParentID, text string) error
}

// TrackedChatSender is optionally implemented by senders that return the platform message ID
// assigned to the outgoing message (e.g. Twitch Helix POST /chat/messages).
type TrackedChatSender interface {
	SendTrackedReply(ctx context.Context, broadcasterID, senderID, replyParentID, text string) (string, error)
}

type PersonaProvider interface {
	Current() *PersonaSpec
	Switch(idOrFile string) (*PersonaSpec, error)
	List() []string
}
