package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/ratelimit"
	"github.com/twitch-social-bot/internal/room"
	"github.com/twitch-social-bot/internal/safety"
	"github.com/twitch-social-bot/internal/social"
)

// Status represents a snapshot of the orchestrator state.
type Status struct {
	Paused            bool                           `json:"paused"`
	ActivePersonaID   string                         `json:"active_persona_id"`
	ActivePersonaName string                         `json:"active_persona_name"`
	BotName           string                         `json:"bot_name"`
	TriggerPrefix     string                         `json:"trigger_prefix"`
	ActiveSlots       map[string][]domain.SlotHolder `json:"active_slots"`
	ProcessedMessages int64                          `json:"processed_messages"`
	GeneratedReplies  int64                          `json:"generated_replies"`
}

// OrchestratorConfig holds the dependencies required by Orchestrator.
type OrchestratorConfig struct {
	BotUserID string
	BotLogin  string
	Clock     domain.Clock
	Store     domain.Store
	LLM       domain.LLMClient
	Sender    domain.ChatSender
	Persona   domain.PersonaProvider
	Limiter   *ratelimit.CooldownLimiter
	Room      *room.Buffer
	Social      *social.Engine
	Guard       *safety.Guard
	Locale      *i18n.Manager
	ChannelInfo domain.ChannelInfoFetcher
}

// Orchestrator coordinates chat messaging, rate limiting, room buffer,
// social state machine, safety filtering, and LLM completions.
type Orchestrator struct {
	mu                sync.RWMutex
	paused            bool
	processedMessages int64
	generatedReplies  int64
	cfg               OrchestratorConfig
	loc               *i18n.Manager
	mood              *social.MoodTracker
	channelMu         sync.Mutex
	cachedChannel     domain.ChannelInfo
	cachedChannelAt   time.Time
	sentMu            sync.Mutex
	sentReplies       map[string]time.Time
	sentMessageIDs    map[string]time.Time
}

// NewOrchestrator creates a new conversational orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	if cfg.Clock == nil {
		cfg.Clock = domain.NewRealClock()
	}
	loc := cfg.Locale
	if loc == nil {
		loc, _ = i18n.NewManager("", "es")
	}
	return &Orchestrator{
		cfg:            cfg,
		loc:            loc,
		mood:           social.NewMoodTracker(cfg.Clock),
		sentReplies:    make(map[string]time.Time),
		sentMessageIDs: make(map[string]time.Time),
	}
}

// IsPaused returns true if conversational generation is paused.
func (o *Orchestrator) IsPaused() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.paused
}

// SetPaused updates the paused state of conversational generation.
func (o *Orchestrator) SetPaused(paused bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.paused = paused
}

// StatusSnapshot returns a point-in-time snapshot of the orchestrator status.
func (o *Orchestrator) StatusSnapshot(ctx context.Context) Status {
	o.mu.RLock()
	paused := o.paused
	o.mu.RUnlock()

	var activePersonaID, activePersonaName, botName, triggerPrefix string
	var activeSlots map[string][]domain.SlotHolder

	if o.cfg.Persona != nil {
		spec := o.cfg.Persona.Current()
		if spec != nil {
			activePersonaID = spec.ID
			activePersonaName = spec.Name
			botName = spec.BotName
			triggerPrefix = spec.TriggerPrefix

			if o.cfg.Store != nil {
				slots, err := o.cfg.Store.GetSlots(ctx, spec.ID)
				if err == nil {
					activeSlots = slots
				}
			}
		}
	}
	if activeSlots == nil {
		activeSlots = make(map[string][]domain.SlotHolder)
	}

	return Status{
		Paused:            paused,
		ActivePersonaID:   activePersonaID,
		ActivePersonaName: activePersonaName,
		BotName:           botName,
		TriggerPrefix:     triggerPrefix,
		ActiveSlots:       activeSlots,
		ProcessedMessages: atomic.LoadInt64(&o.processedMessages),
		GeneratedReplies:  atomic.LoadInt64(&o.generatedReplies),
	}
}

// HandleMessage processes an incoming chat message from Twitch.
func (o *Orchestrator) HandleMessage(ctx context.Context, msg domain.ChatMessage) error {
	trimmedText := strings.TrimSpace(msg.Text)
	if trimmedText == "" {
		return nil
	}
	if o.wasSentMessageID(msg.MessageID) {
		return nil
	}

	var spec *domain.PersonaSpec
	if o.cfg.Persona != nil {
		spec = o.cfg.Persona.Current()
	}

	isSelf := (o.cfg.BotUserID != "" && msg.UserID == o.cfg.BotUserID) ||
		(o.cfg.BotLogin != "" && strings.EqualFold(msg.Username, o.cfg.BotLogin))
	sameAccountMode := (o.cfg.BotUserID != "" && msg.BroadcasterID != "" && o.cfg.BotUserID == msg.BroadcasterID) ||
		(o.cfg.BotLogin != "" && msg.BroadcasterName != "" && strings.EqualFold(o.cfg.BotLogin, msg.BroadcasterName))
	if isSelf && msg.IsBroadcaster {
		sameAccountMode = true
	}

	payload, triggered := matchTrigger(msg.Text, spec, o.cfg.BotLogin, sameAccountMode)

	// Filter self messages: in dedicated-bot mode, drop everything from the bot account.
	// In same-account mode (broadcaster == bot), allow triggered commands/prompts from the
	// streamer while dropping untriggered echoes and recently sent bot replies.
	if isSelf {
		if !sameAccountMode || !triggered || o.wasRecentlySent(trimmedText) {
			return nil
		}
	}
	if !triggered {
		// Non-triggered message: push to room buffer
		if o.cfg.Room != nil && o.cfg.Guard != nil {
			cleanText := o.cfg.Guard.SanitizeInput(msg.Text)
			o.cfg.Room.Push(domain.RoomMessage{
				UserID:      msg.UserID,
				DisplayName: msg.DisplayName,
				Text:        cleanText,
				Timestamp:   msg.Timestamp,
			})
		}
		atomic.AddInt64(&o.processedMessages, 1)
		return nil
	}

	atomic.AddInt64(&o.processedMessages, 1)

	// Check for administrative subcommands
	fields := strings.Fields(payload)
	var cmd string
	if len(fields) > 0 {
		cmd = strings.ToLower(fields[0])
	}

	isAdminCmd := cmd == "pause" || cmd == "resume" || cmd == "status" || cmd == "persona" || cmd == "set" || cmd == "reset"
	if isAdminCmd {
		if !msg.IsBroadcaster && !msg.IsModerator {
			// Silently ignore unauthorized administrative subcommands
			return nil
		}

		switch cmd {
		case "pause":
			o.SetPaused(true)
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_paused"))
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "resume":
			o.SetPaused(false)
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_resumed"))
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "status":
			stateStr := strings.ToLower(o.loc.Get("bot_state_active"))
			if o.IsPaused() {
				stateStr = strings.ToLower(o.loc.Get("bot_state_paused"))
			}
			activeName := ""
			if spec != nil {
				activeName = spec.Name
			}
			var slotsSummary []string
			if spec != nil && o.cfg.Store != nil {
				slots, err := o.cfg.Store.GetSlots(ctx, spec.ID)
				if err == nil {
					for slotID, holders := range slots {
						for _, h := range holders {
							slotsSummary = append(slotsSummary, fmt.Sprintf("%s: %s (%d)", slotID, h.DisplayName, h.Affinity))
						}
					}
				}
			}
			puestosStr := o.loc.Get("bot_no_slots")
			if len(slotsSummary) > 0 {
				puestosStr = strings.Join(slotsSummary, ", ")
			}
			reply := o.loc.Format("bot_status_fmt", activeName, stateStr, puestosStr)
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "persona":
			if len(fields) < 2 {
				if o.cfg.Sender != nil {
					_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_usage_persona"))
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			targetPersona := fields[1]
			newSpec, err := o.cfg.Persona.Switch(targetPersona)
			if err != nil {
				if o.cfg.Sender != nil {
					_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_persona_error")+err.Error())
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Format("bot_persona_activated", newSpec.Name))
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "set":
			if len(fields) < 3 {
				if o.cfg.Sender != nil {
					_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_usage_set"))
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			targetUser := strings.TrimPrefix(fields[1], "@")
			score, err := strconv.Atoi(fields[2])
			if err != nil {
				if o.cfg.Sender != nil {
					_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_invalid_affinity"))
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}

			profile, found, err := o.cfg.Store.FindProfileByUsername(ctx, targetUser)
			if err != nil {
				return fmt.Errorf("find profile: %w", err)
			}
			if !found {
				profile = domain.ViewerProfile{
					UserID:      targetUser,
					Username:    targetUser,
					DisplayName: targetUser,
					Affinity:    0,
					TierID:      o.cfg.Social.ResolveTier(spec, 0).ID,
				}
			}

			updatedProfile := o.cfg.Social.SetAffinity(spec, profile, score)

			// Update slots
			if spec != nil && o.cfg.Store != nil {
				slots, err := o.cfg.Store.GetSlots(ctx, spec.ID)
				if err == nil {
					for slotID, holders := range slots {
						slotSpec := findSlotSpec(spec, slotID)
						var updated []domain.SlotHolder
						for _, h := range holders {
							if h.UserID == updatedProfile.UserID {
								h.DisplayName = updatedProfile.DisplayName
								h.Affinity = updatedProfile.Affinity
								eligible := true
								if slotSpec != nil {
									if slotSpec.MinAffinity != nil && updatedProfile.Affinity < *slotSpec.MinAffinity {
										eligible = false
									}
									if slotSpec.MaxAffinity != nil && updatedProfile.Affinity > *slotSpec.MaxAffinity {
										eligible = false
									}
								}
								if eligible {
									updated = append(updated, h)
								}
							} else {
								updated = append(updated, h)
							}
						}
						slots[slotID] = updated
					}
					_ = o.cfg.Store.SaveSlots(ctx, spec.ID, slots)
				}
			}

			if err := o.cfg.Store.SaveProfile(ctx, updatedProfile); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}

			reply := o.loc.Format("bot_affinity_updated", updatedProfile.DisplayName, updatedProfile.Affinity, updatedProfile.TierID)
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "reset":
			if len(fields) < 2 {
				if o.cfg.Sender != nil {
					_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, o.loc.Get("bot_usage_reset"))
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			targetUser := strings.TrimPrefix(fields[1], "@")
			profile, found, err := o.cfg.Store.FindProfileByUsername(ctx, targetUser)
			if err != nil {
				return fmt.Errorf("find profile: %w", err)
			}
			targetUserID := targetUser
			displayName := targetUser
			if found {
				targetUserID = profile.UserID
				displayName = profile.DisplayName
			}

			if err := o.cfg.Store.ResetProfile(ctx, targetUserID); err != nil {
				return fmt.Errorf("reset profile: %w", err)
			}
			if o.cfg.Limiter != nil {
				o.cfg.Limiter.ResetUser(targetUserID)
			}

			reply := o.loc.Format("bot_profile_reset", displayName)
			if o.cfg.Sender != nil {
				_ = o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil
		}
	}

	// Normal conversational message
	if o.IsPaused() {
		return nil
	}

	if o.cfg.Limiter != nil && !o.cfg.Limiter.AllowForUser(msg.UserID, msg.IsBroadcaster) {
		return nil
	}

	cleanInput := payload
	if o.cfg.Guard != nil {
		cleanInput = o.cfg.Guard.SanitizeInput(payload)
	}
	if strings.TrimSpace(cleanInput) == "" {
		return nil
	}

	profile, found, err := o.cfg.Store.GetProfile(ctx, msg.UserID)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}
	if !found {
		tierID := ""
		if o.cfg.Social != nil {
			tierID = o.cfg.Social.ResolveTier(spec, 0).ID
		}
		profile = domain.ViewerProfile{
			UserID:      msg.UserID,
			Username:    msg.Username,
			DisplayName: msg.DisplayName,
			Affinity:    0,
			TierID:      tierID,
		}
	}
	profile.Username = msg.Username
	if msg.DisplayName != "" {
		profile.DisplayName = msg.DisplayName
	}

	var personaID string
	if spec != nil {
		personaID = spec.ID
	}

	slots, err := o.cfg.Store.GetSlots(ctx, personaID)
	if err != nil {
		return fmt.Errorf("get slots: %w", err)
	}

	var recentRoom []domain.RoomMessage
	if o.cfg.Room != nil {
		recentRoom = o.cfg.Room.Recent(15)
	}

	currentTier := domain.TierSpec{}
	if o.cfg.Social != nil {
		currentTier = o.cfg.Social.ResolveTier(spec, profile.Affinity)
	}

	streamInfo := o.getChannelInfo(ctx, msg.BroadcasterID)
	viewerRole := resolveViewerRole(msg, profile)
	currentMood := o.mood.Current()

	decision, err := o.cfg.LLM.Generate(ctx, domain.PromptInput{
		Persona:       spec,
		Profile:       profile,
		CurrentTier:   currentTier,
		ActiveSlots:   slots,
		RecentRoom:    recentRoom,
		ViewerMessage: cleanInput,
		ViewerRole:    viewerRole,
		CurrentMood:   currentMood,
		StreamGame:    streamInfo.GameName,
		StreamTitle:   streamInfo.Title,
	})
	if err != nil {
		return fmt.Errorf("llm generate: %w", err)
	}
	o.mood.Record(decision.AffinityDelta)

	// Push user message to room buffer
	if o.cfg.Room != nil && o.cfg.Guard != nil {
		o.cfg.Room.Push(domain.RoomMessage{
			UserID:      msg.UserID,
			DisplayName: msg.DisplayName,
			Text:        cleanInput,
			Timestamp:   msg.Timestamp,
		})
	}

	safeReply := decision.Reply
	var blockedWords []string
	if spec != nil {
		blockedWords = spec.BlockedWords
	}
	if o.cfg.Guard != nil {
		safeReply = o.cfg.Guard.SanitizeReply(decision.Reply, msg.DisplayName, blockedWords)
	}

	updatedProfile, updatedSlots, _ := o.cfg.Social.ApplyDecision(
		spec,
		profile,
		slots,
		cleanInput,
		domain.LLMDecision{
			Reply:         safeReply,
			AffinityDelta: decision.AffinityDelta,
			NewTitle:      decision.NewTitle,
			NewFact:       decision.NewFact,
			SlotClaim:     decision.SlotClaim,
		},
	)

	if err := o.cfg.Store.SaveProfile(ctx, updatedProfile); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	if err := o.cfg.Store.SaveSlots(ctx, personaID, updatedSlots); err != nil {
		return fmt.Errorf("save slots: %w", err)
	}

	if o.cfg.Sender != nil {
		if err := o.sendTrackedReply(ctx, msg.BroadcasterID, msg.MessageID, safeReply); err != nil {
			return fmt.Errorf("send reply: %w", err)
		}
		atomic.AddInt64(&o.generatedReplies, 1)
	}

	return nil
}

func matchTrigger(text string, spec *domain.PersonaSpec, botLogin string, sameAccountMode bool) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}

	var prefixes []string
	if spec != nil && spec.TriggerPrefix != "" {
		prefixes = append(prefixes, spec.TriggerPrefix)
	}
	if spec != nil && spec.BotName != "" {
		prefixes = append(prefixes, "@"+spec.BotName)
	}
	// In 2-account mode, @<botLogin> triggers the bot. In 1-account mode (broadcaster == bot),
	// @<botLogin> is the streamer's own handle, so exclude it to prevent false triggers when
	// viewers mention the streamer or when Twitch prepends @<streamer> to threaded replies.
	if botLogin != "" && !sameAccountMode {
		prefixes = append(prefixes, "@"+botLogin)
	}

	for _, prefix := range prefixes {
		pLen := len(prefix)
		if len(trimmed) >= pLen && strings.EqualFold(trimmed[:pLen], prefix) {
			if len(trimmed) == pLen {
				return "", true
			}
			r, _ := utf8.DecodeRuneInString(trimmed[pLen:])
			if unicode.IsSpace(r) {
				return strings.TrimSpace(trimmed[pLen:]), true
			}
		}
	}

	return "", false
}

func findSlotSpec(spec *domain.PersonaSpec, slotID string) *domain.SlotSpec {
	if spec == nil {
		return nil
	}
	for i := range spec.Slots {
		if spec.Slots[i].ID == slotID {
			return &spec.Slots[i]
		}
	}
	return nil
}

const channelInfoTTL = 3 * time.Minute

func (o *Orchestrator) getChannelInfo(ctx context.Context, broadcasterID string) domain.ChannelInfo {
	if o.cfg.ChannelInfo == nil || strings.TrimSpace(broadcasterID) == "" {
		return domain.ChannelInfo{}
	}

	now := o.cfg.Clock.Now()
	o.channelMu.Lock()
	if !o.cachedChannelAt.IsZero() && now.Sub(o.cachedChannelAt) < channelInfoTTL {
		cached := o.cachedChannel
		o.channelMu.Unlock()
		return cached
	}
	o.channelMu.Unlock()

	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	info, err := o.cfg.ChannelInfo.GetChannelInfo(fetchCtx, broadcasterID)
	if err != nil {
		o.channelMu.Lock()
		cached := o.cachedChannel
		o.channelMu.Unlock()
		return cached
	}

	o.channelMu.Lock()
	o.cachedChannel = info
	o.cachedChannelAt = now
	o.channelMu.Unlock()
	return info
}

func resolveViewerRole(msg domain.ChatMessage, profile domain.ViewerProfile) string {
	switch {
	case msg.IsBroadcaster:
		return "broadcaster"
	case msg.IsModerator:
		return "moderator"
	case msg.IsVIP:
		return "vip"
	case msg.IsSubscriber:
		return "subscriber"
	case profile.InteractionCount == 0:
		return "new"
	default:
		return "viewer"
	}
}

const sentReplyTTL = 60 * time.Second

func (o *Orchestrator) now() time.Time {
	if o.cfg.Clock != nil {
		return o.cfg.Clock.Now()
	}
	return time.Now()
}

func (o *Orchestrator) recordSentReply(text string) {
	key := strings.TrimSpace(text)
	if key == "" {
		return
	}
	now := o.now()
	o.sentMu.Lock()
	defer o.sentMu.Unlock()
	if o.sentReplies == nil {
		o.sentReplies = make(map[string]time.Time)
	}
	for k, ts := range o.sentReplies {
		if now.Sub(ts) > sentReplyTTL {
			delete(o.sentReplies, k)
		}
	}
	o.sentReplies[key] = now
}

func (o *Orchestrator) recordSentMessageID(messageID string) {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return
	}
	now := o.now()
	o.sentMu.Lock()
	defer o.sentMu.Unlock()
	if o.sentMessageIDs == nil {
		o.sentMessageIDs = make(map[string]time.Time)
	}
	for k, ts := range o.sentMessageIDs {
		if now.Sub(ts) > sentReplyTTL {
			delete(o.sentMessageIDs, k)
		}
	}
	o.sentMessageIDs[id] = now
}

func (o *Orchestrator) wasSentMessageID(messageID string) bool {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return false
	}
	now := o.now()
	o.sentMu.Lock()
	defer o.sentMu.Unlock()
	if o.sentMessageIDs == nil {
		return false
	}
	for k, ts := range o.sentMessageIDs {
		if now.Sub(ts) > sentReplyTTL {
			delete(o.sentMessageIDs, k)
		}
	}
	ts, ok := o.sentMessageIDs[id]
	if !ok {
		return false
	}
	return now.Sub(ts) <= sentReplyTTL
}

func stripLeadingMention(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "@") {
		return trimmed
	}
	idx := strings.IndexFunc(trimmed, unicode.IsSpace)
	if idx == -1 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[idx:])
}

func (o *Orchestrator) wasRecentlySent(text string) bool {
	key := strings.TrimSpace(text)
	if key == "" {
		return false
	}
	stripped := stripLeadingMention(key)
	now := o.now()
	o.sentMu.Lock()
	defer o.sentMu.Unlock()
	if o.sentReplies == nil {
		return false
	}
	for k, ts := range o.sentReplies {
		if now.Sub(ts) > sentReplyTTL {
			delete(o.sentReplies, k)
		}
	}
	if ts, ok := o.sentReplies[key]; ok && now.Sub(ts) <= sentReplyTTL {
		return true
	}
	if stripped != key {
		if ts, ok := o.sentReplies[stripped]; ok && now.Sub(ts) <= sentReplyTTL {
			return true
		}
	}
	return false
}

func (o *Orchestrator) sendTrackedReply(ctx context.Context, broadcasterID, replyParentID, text string) error {
	if o.cfg.Sender == nil {
		return nil
	}
	o.recordSentReply(text)
	if tracked, ok := o.cfg.Sender.(domain.TrackedChatSender); ok {
		msgID, err := tracked.SendTrackedReply(ctx, broadcasterID, o.cfg.BotUserID, replyParentID, text)
		if err != nil {
			return err
		}
		o.recordSentMessageID(msgID)
		return nil
	}
	return o.cfg.Sender.SendReply(ctx, broadcasterID, o.cfg.BotUserID, replyParentID, text)
}
