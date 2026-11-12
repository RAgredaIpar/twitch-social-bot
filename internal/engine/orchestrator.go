package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/twitch-social-bot/internal/domain"
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
	Social    *social.Engine
	Guard     *safety.Guard
}

// Orchestrator coordinates chat messaging, rate limiting, room buffer,
// social state machine, safety filtering, and LLM completions.
type Orchestrator struct {
	mu                sync.RWMutex
	paused            bool
	processedMessages int64
	generatedReplies  int64
	cfg               OrchestratorConfig
}

// NewOrchestrator creates a new conversational orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	if cfg.Clock == nil {
		cfg.Clock = domain.NewRealClock()
	}
	return &Orchestrator{
		cfg: cfg,
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

	// Filter self messages
	if o.cfg.BotUserID != "" && msg.UserID == o.cfg.BotUserID {
		return nil
	}
	if o.cfg.BotLogin != "" && strings.EqualFold(msg.Username, o.cfg.BotLogin) {
		return nil
	}

	var spec *domain.PersonaSpec
	if o.cfg.Persona != nil {
		spec = o.cfg.Persona.Current()
	}

	payload, triggered := matchTrigger(msg.Text, spec, o.cfg.BotLogin)
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
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Bot pausado.")
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "resume":
			o.SetPaused(false)
			if o.cfg.Sender != nil {
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Bot reanudado.")
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "status":
			stateStr := "activo"
			if o.IsPaused() {
				stateStr = "pausado"
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
			puestosStr := "ninguno"
			if len(slotsSummary) > 0 {
				puestosStr = strings.Join(slotsSummary, ", ")
			}
			reply := fmt.Sprintf("Persona: %s | Estado: %s | Puestos: %s", activeName, stateStr, puestosStr)
			if o.cfg.Sender != nil {
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "persona":
			if len(fields) < 2 {
				if o.cfg.Sender != nil {
					_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Uso: !bot persona <id>")
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			targetPersona := fields[1]
			newSpec, err := o.cfg.Persona.Switch(targetPersona)
			if err != nil {
				if o.cfg.Sender != nil {
					_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Error al cambiar persona: "+err.Error())
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			if o.cfg.Sender != nil {
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, fmt.Sprintf("Persona activada: %s.", newSpec.Name))
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "set":
			if len(fields) < 3 {
				if o.cfg.Sender != nil {
					_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Uso: !bot set @usuario <afinidad>")
					atomic.AddInt64(&o.generatedReplies, 1)
				}
				return nil
			}
			targetUser := strings.TrimPrefix(fields[1], "@")
			score, err := strconv.Atoi(fields[2])
			if err != nil {
				if o.cfg.Sender != nil {
					_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Puntuación de afinidad inválida.")
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

			reply := fmt.Sprintf("Afinidad de %s actualizada a %d (Tier: %s).", updatedProfile.DisplayName, updatedProfile.Affinity, updatedProfile.TierID)
			if o.cfg.Sender != nil {
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil

		case "reset":
			if len(fields) < 2 {
				if o.cfg.Sender != nil {
					_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, "Uso: !bot reset @usuario")
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

			reply := fmt.Sprintf("Perfil de %s restablecido correctamente.", displayName)
			if o.cfg.Sender != nil {
				_ = o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, reply)
				atomic.AddInt64(&o.generatedReplies, 1)
			}
			return nil
		}
	}

	// Normal conversational message
	if o.IsPaused() {
		return nil
	}

	if o.cfg.Limiter != nil && !o.cfg.Limiter.Allow(msg.UserID) {
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

	decision, err := o.cfg.LLM.Generate(ctx, domain.PromptInput{
		Persona:       spec,
		Profile:       profile,
		CurrentTier:   currentTier,
		ActiveSlots:   slots,
		RecentRoom:    recentRoom,
		ViewerMessage: cleanInput,
	})
	if err != nil {
		return fmt.Errorf("llm generate: %w", err)
	}

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
		if err := o.cfg.Sender.SendReply(ctx, msg.BroadcasterID, o.cfg.BotUserID, msg.MessageID, safeReply); err != nil {
			return fmt.Errorf("send reply: %w", err)
		}
		atomic.AddInt64(&o.generatedReplies, 1)
	}

	return nil
}

func matchTrigger(text string, spec *domain.PersonaSpec, botLogin string) (string, bool) {
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
	if botLogin != "" {
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
