package social

import (
	"strings"

	"github.com/twitch-social-bot/internal/domain"
)

const (
	DefaultMaxViewerFacts = 15
	MaxRecentTurns        = 4
	MaxEarnedTitleRunes   = 60
)

type Engine struct {
	clock domain.Clock
}

func NewEngine(clock domain.Clock) *Engine {
	if clock == nil {
		clock = domain.NewRealClock()
	}
	return &Engine{
		clock: clock,
	}
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func (e *Engine) ResolveTier(spec *domain.PersonaSpec, affinity int) domain.TierSpec {
	if spec == nil || len(spec.Tiers) == 0 {
		return domain.TierSpec{}
	}

	aff := clamp(affinity, -100, 100)
	for _, tier := range spec.Tiers {
		if aff >= tier.Min && aff <= tier.Max {
			return tier
		}
	}
	return domain.TierSpec{}
}

func (e *Engine) SetAffinity(spec *domain.PersonaSpec, profile domain.ViewerProfile, targetAffinity int) domain.ViewerProfile {
	profile.Affinity = clamp(targetAffinity, -100, 100)
	profile.TierID = e.ResolveTier(spec, profile.Affinity).ID
	profile.UpdatedAt = e.clock.Now()
	return profile
}

func (e *Engine) ApplyDecision(
	spec *domain.PersonaSpec,
	profile domain.ViewerProfile,
	currentSlots map[string][]domain.SlotHolder,
	userMsg string,
	decision domain.LLMDecision,
) (domain.ViewerProfile, map[string][]domain.SlotHolder, string) {
	// 1. Delta Clamping
	delta := decision.AffinityDelta
	if spec != nil {
		if spec.MaxPositiveDelta > 0 && delta > spec.MaxPositiveDelta {
			delta = spec.MaxPositiveDelta
		}
		if spec.MaxNegativeDelta < 0 && delta < spec.MaxNegativeDelta {
			delta = spec.MaxNegativeDelta
		}
	}
	newAffinity := clamp(profile.Affinity+delta, -100, 100)
	profile.Affinity = newAffinity
	profile.TierID = e.ResolveTier(spec, newAffinity).ID

	// 2. Earned Title
	trimmedTitle := strings.TrimSpace(decision.NewTitle)
	if trimmedTitle != "" {
		runes := []rune(trimmedTitle)
		if len(runes) > MaxEarnedTitleRunes {
			trimmedTitle = string(runes[:MaxEarnedTitleRunes])
		}
		profile.EarnedTitle = trimmedTitle
	}

	// 3. Facts (Max 5, case-insensitive deduplication, FIFO eviction of oldest when > 5)
	trimmedFact := strings.TrimSpace(decision.NewFact)
	if trimmedFact != "" {
		exists := false
		for _, f := range profile.Facts {
			if strings.EqualFold(strings.TrimSpace(f), trimmedFact) {
				exists = true
				break
			}
		}
		if !exists {
			maxFacts := DefaultMaxViewerFacts
			if spec != nil && spec.MaxViewerFacts > 0 {
				maxFacts = spec.MaxViewerFacts
			}
			facts := make([]string, len(profile.Facts), len(profile.Facts)+1)
			copy(facts, profile.Facts)
			facts = append(facts, trimmedFact)
			if len(facts) > maxFacts {
				facts = facts[len(facts)-maxFacts:]
			}
			profile.Facts = facts
		}
	}

	// 4. RecentTurns (Max 4, FIFO eviction of oldest when > 4)
	now := e.clock.Now()
	turns := make([]domain.ConversationTurn, len(profile.RecentTurns), len(profile.RecentTurns)+1)
	copy(turns, profile.RecentTurns)
	turns = append(turns, domain.ConversationTurn{
		UserMessage: strings.TrimSpace(userMsg),
		BotReply:    strings.TrimSpace(decision.Reply),
		Timestamp:   now,
	})
	if len(turns) > MaxRecentTurns {
		turns = turns[len(turns)-MaxRecentTurns:]
	}
	profile.RecentTurns = turns
	profile.InteractionCount++
	profile.UpdatedAt = now

	// 5. SocialSlots Validation & Assignment
	nextSlots := make(map[string][]domain.SlotHolder, len(currentSlots))
	for k, v := range currentSlots {
		holdersCopy := make([]domain.SlotHolder, len(v))
		copy(holdersCopy, v)
		nextSlots[k] = holdersCopy
	}

	findSlotSpec := func(slotID string) *domain.SlotSpec {
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

	// Synchronize existing slots held by profile.UserID
	for slotID, holders := range nextSlots {
		slotSpec := findSlotSpec(slotID)
		var updated []domain.SlotHolder
		for _, h := range holders {
			if h.UserID == profile.UserID {
				h.DisplayName = profile.DisplayName
				h.Affinity = profile.Affinity
				eligible := true
				if slotSpec != nil {
					if slotSpec.MinAffinity != nil && profile.Affinity < *slotSpec.MinAffinity {
						eligible = false
					}
					if slotSpec.MaxAffinity != nil && profile.Affinity > *slotSpec.MaxAffinity {
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
		nextSlots[slotID] = updated
	}

	slotEvent := ""
	slotClaim := strings.TrimSpace(decision.SlotClaim)
	if slotClaim != "" {
		slotSpec := findSlotSpec(slotClaim)
		if slotSpec != nil {
			eligible := true
			if slotSpec.MinAffinity != nil && profile.Affinity < *slotSpec.MinAffinity {
				eligible = false
			}
			if slotSpec.MaxAffinity != nil && profile.Affinity > *slotSpec.MaxAffinity {
				eligible = false
			}

			if eligible {
				holders := nextSlots[slotSpec.ID]
				alreadyIdx := -1
				for i, h := range holders {
					if h.UserID == profile.UserID {
						alreadyIdx = i
						break
					}
				}

				if alreadyIdx >= 0 {
					holders[alreadyIdx].DisplayName = profile.DisplayName
					holders[alreadyIdx].Affinity = profile.Affinity
					nextSlots[slotSpec.ID] = holders
				} else if len(holders) < slotSpec.MaxHolders {
					holders = append(holders, domain.SlotHolder{
						SlotID:      slotSpec.ID,
						UserID:      profile.UserID,
						DisplayName: profile.DisplayName,
						Affinity:    profile.Affinity,
						AssignedAt:  now,
					})
					nextSlots[slotSpec.ID] = holders
					slotEvent = slotSpec.ID
				} else if slotSpec.AllowOutrankSteal && len(holders) > 0 {
					if slotSpec.MaxAffinity != nil && slotSpec.MinAffinity == nil {
						// Negative slot: find highest (least negative) Affinity
						weakestIdx := 0
						for i := 1; i < len(holders); i++ {
							if holders[i].Affinity > holders[weakestIdx].Affinity {
								weakestIdx = i
							}
						}
						if profile.Affinity < holders[weakestIdx].Affinity {
							holders[weakestIdx] = domain.SlotHolder{
								SlotID:      slotSpec.ID,
								UserID:      profile.UserID,
								DisplayName: profile.DisplayName,
								Affinity:    profile.Affinity,
								AssignedAt:  now,
							}
							nextSlots[slotSpec.ID] = holders
							slotEvent = slotSpec.ID
						}
					} else {
						// Positive slot (MinAffinity != nil) or Bounded slot: find lowest Affinity
						weakestIdx := 0
						for i := 1; i < len(holders); i++ {
							if holders[i].Affinity < holders[weakestIdx].Affinity {
								weakestIdx = i
							}
						}
						if profile.Affinity > holders[weakestIdx].Affinity {
							holders[weakestIdx] = domain.SlotHolder{
								SlotID:      slotSpec.ID,
								UserID:      profile.UserID,
								DisplayName: profile.DisplayName,
								Affinity:    profile.Affinity,
								AssignedAt:  now,
							}
							nextSlots[slotSpec.ID] = holders
							slotEvent = slotSpec.ID
						}
					}
				}
			}
		}
	}

	return profile, nextSlots, slotEvent
}
