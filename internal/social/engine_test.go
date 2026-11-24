package social_test

import (
	"strings"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/social"
)

func samplePersona() *domain.PersonaSpec {
	minAff := 75
	maxAff := -35
	return &domain.PersonaSpec{
		ID:               "cohost_amigos",
		Name:             "Co-Host Amigos",
		BotName:          "Kiro",
		TriggerPrefix:    "!bot",
		MaxPositiveDelta: 5,
		MaxNegativeDelta: -8,
		BasePersona:      "Eres Kiro el bot sarcástico.",
		Tiers: []domain.TierSpec{
			{ID: "rival", Name: "Rival", Min: -100, Max: -25, Behavior: "Cortante"},
			{ID: "neutral", Name: "Neutral", Min: -24, Max: 15, Behavior: "Distante"},
			{ID: "amigo", Name: "Amigo", Min: 16, Max: 44, Behavior: "Casual"},
			{ID: "pana", Name: "Pana", Min: 45, Max: 74, Behavior: "Confianza"},
			{ID: "favorito", Name: "Favorito", Min: 75, Max: 100, Behavior: "Máxima lealtad"},
		},
		Slots: []domain.SlotSpec{
			{
				ID:                "favorito_del_bot",
				Title:             "El Favorito de Kiro",
				MaxHolders:        1,
				MinAffinity:       &minAff,
				AllowOutrankSteal: true,
			},
			{
				ID:                "enemigo_publico",
				Title:             "Enemigo Público #1",
				MaxHolders:        1,
				MaxAffinity:       &maxAff,
				AllowOutrankSteal: true,
			},
			{
				ID:                "complice",
				Title:             "Cómplice Oficial",
				MaxHolders:        2,
				MinAffinity:       &minAff,
				AllowOutrankSteal: false,
			},
		},
	}
}

func TestResolveTier(t *testing.T) {
	eng := social.NewEngine(nil)
	spec := samplePersona()

	tests := []struct {
		name     string
		affinity int
		expected string
	}{
		{"clamped below min", -150, "rival"},
		{"exact min rival", -100, "rival"},
		{"boundary rival max", -25, "rival"},
		{"boundary neutral min", -24, "neutral"},
		{"neutral zero", 0, "neutral"},
		{"boundary amigo min", 16, "amigo"},
		{"boundary pana min", 45, "pana"},
		{"boundary favorito min", 75, "favorito"},
		{"max affinity", 100, "favorito"},
		{"clamped above max", 200, "favorito"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tier := eng.ResolveTier(spec, tc.affinity)
			if tier.ID != tc.expected {
				t.Fatalf("expected tier %q for affinity %d, got %q", tc.expected, tc.affinity, tier.ID)
			}
		})
	}

	// Nil spec returns zero tier
	if tier := eng.ResolveTier(nil, 50); tier.ID != "" {
		t.Fatalf("expected empty tier for nil spec, got %+v", tier)
	}
}

func TestSetAffinity(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(now)
	eng := social.NewEngine(clock)
	spec := samplePersona()

	profile := domain.ViewerProfile{
		UserID:      "u123",
		DisplayName: "Ana",
		Affinity:    0,
	}

	updated := eng.SetAffinity(spec, profile, 80)
	if updated.Affinity != 80 {
		t.Fatalf("expected affinity 80, got %d", updated.Affinity)
	}
	if updated.TierID != "favorito" {
		t.Fatalf("expected tier favorito, got %q", updated.TierID)
	}
	if !updated.UpdatedAt.Equal(now) {
		t.Fatalf("expected UpdatedAt %v, got %v", now, updated.UpdatedAt)
	}

	// Clamping check
	clampedLow := eng.SetAffinity(spec, profile, -999)
	if clampedLow.Affinity != -100 || clampedLow.TierID != "rival" {
		t.Fatalf("expected clamped -100 rival, got %d %s", clampedLow.Affinity, clampedLow.TierID)
	}

	clampedHigh := eng.SetAffinity(spec, profile, 999)
	if clampedHigh.Affinity != 100 || clampedHigh.TierID != "favorito" {
		t.Fatalf("expected clamped 100 favorito, got %d %s", clampedHigh.Affinity, clampedHigh.TierID)
	}
}

func TestApplyDecision_DeltaClamping(t *testing.T) {
	clock := domain.NewFakeClock(time.Now())
	eng := social.NewEngine(clock)
	spec := samplePersona()

	// MaxPositiveDelta is 5
	p1 := domain.ViewerProfile{UserID: "u1", Affinity: 10}
	d1 := domain.LLMDecision{Reply: "Hola!", AffinityDelta: 25}
	res1, _, _ := eng.ApplyDecision(spec, p1, nil, "buenas", d1)
	if res1.Affinity != 15 {
		t.Fatalf("expected affinity clamped delta +5 -> 15, got %d", res1.Affinity)
	}

	// MaxNegativeDelta is -8
	p2 := domain.ViewerProfile{UserID: "u2", Affinity: 10}
	d2 := domain.LLMDecision{Reply: "No me caes bien", AffinityDelta: -30}
	res2, _, _ := eng.ApplyDecision(spec, p2, nil, "odio", d2)
	if res2.Affinity != 2 {
		t.Fatalf("expected affinity clamped delta -8 -> 2, got %d", res2.Affinity)
	}

	// Hard boundary clamp at 100
	p3 := domain.ViewerProfile{UserID: "u3", Affinity: 98}
	d3 := domain.LLMDecision{Reply: "Genial", AffinityDelta: 5}
	res3, _, _ := eng.ApplyDecision(spec, p3, nil, "buena", d3)
	if res3.Affinity != 100 {
		t.Fatalf("expected affinity clamped at 100, got %d", res3.Affinity)
	}

	// Hard boundary clamp at -100
	p4 := domain.ViewerProfile{UserID: "u4", Affinity: -95}
	d4 := domain.LLMDecision{Reply: "Vete", AffinityDelta: -8}
	res4, _, _ := eng.ApplyDecision(spec, p4, nil, "chao", d4)
	if res4.Affinity != -100 {
		t.Fatalf("expected affinity clamped at -100, got %d", res4.Affinity)
	}
}

func TestApplyDecision_EarnedTitle(t *testing.T) {
	eng := social.NewEngine(nil)
	spec := samplePersona()

	profile := domain.ViewerProfile{UserID: "u1", EarnedTitle: "Viejo Titulo"}
	longTitle := strings.Repeat("A", 100)

	decision := domain.LLMDecision{Reply: "ok", NewTitle: longTitle}
	res, _, _ := eng.ApplyDecision(spec, profile, nil, "msg", decision)

	if len([]rune(res.EarnedTitle)) != social.MaxEarnedTitleRunes {
		t.Fatalf("expected title length %d runes, got %d", social.MaxEarnedTitleRunes, len([]rune(res.EarnedTitle)))
	}

	// Empty title should not overwrite
	decisionEmpty := domain.LLMDecision{Reply: "ok", NewTitle: "   "}
	resEmpty, _, _ := eng.ApplyDecision(spec, profile, nil, "msg", decisionEmpty)
	if resEmpty.EarnedTitle != "Viejo Titulo" {
		t.Fatalf("expected old title to remain, got %q", resEmpty.EarnedTitle)
	}
}

func TestApplyDecision_FactsFIFOAndDeduplication(t *testing.T) {
	eng := social.NewEngine(nil)
	spec := samplePersona()

	profile := domain.ViewerProfile{
		UserID: "u1",
		Facts:  []string{"Fact 1", "Fact 2"},
	}

	// Case-insensitive duplicate should not be added
	dDup := domain.LLMDecision{Reply: "ok", NewFact: "fact 1"}
	resDup, _, _ := eng.ApplyDecision(spec, profile, nil, "msg", dDup)
	if len(resDup.Facts) != 2 {
		t.Fatalf("expected 2 facts after dup attempt, got %d", len(resDup.Facts))
	}

	// Adding facts up to max (default 150) and FIFO eviction
	var initialFacts []string
	for i := 1; i <= social.DefaultMaxViewerFacts; i++ {
		initialFacts = append(initialFacts, strings.Repeat("F", i))
	}
	p := domain.ViewerProfile{
		UserID: "u1",
		Facts:  initialFacts,
	}
	dNew := domain.LLMDecision{Reply: "ok", NewFact: "F_Nuevo"}
	resFIFO, _, _ := eng.ApplyDecision(spec, p, nil, "msg", dNew)

	if len(resFIFO.Facts) != social.DefaultMaxViewerFacts {
		t.Fatalf("expected %d facts, got %d", social.DefaultMaxViewerFacts, len(resFIFO.Facts))
	}
	if resFIFO.Facts[0] != initialFacts[1] || resFIFO.Facts[social.DefaultMaxViewerFacts-1] != "F_Nuevo" {
		t.Fatalf("expected FIFO eviction (F2..F_Nuevo), got %+v", resFIFO.Facts)
	}

	// Custom MaxViewerFacts from PersonaSpec
	customSpec := samplePersona()
	customSpec.MaxViewerFacts = 3
	pCustom := domain.ViewerProfile{
		UserID: "u1",
		Facts:  []string{"A", "B", "C"},
	}
	resCustom, _, _ := eng.ApplyDecision(customSpec, pCustom, nil, "msg", domain.LLMDecision{Reply: "ok", NewFact: "D"})
	if len(resCustom.Facts) != 3 {
		t.Fatalf("expected 3 facts with custom spec limit, got %d", len(resCustom.Facts))
	}
	if resCustom.Facts[0] != "B" || resCustom.Facts[2] != "D" {
		t.Fatalf("expected B, C, D, got %+v", resCustom.Facts)
	}
}

func TestApplyDecision_RecentTurnsFIFO(t *testing.T) {
	fakeNow := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(fakeNow)
	eng := social.NewEngine(clock)
	spec := samplePersona()

	p := domain.ViewerProfile{
		UserID:           "u1",
		InteractionCount: 2,
		RecentTurns: []domain.ConversationTurn{
			{UserMessage: "m1", BotReply: "r1"},
			{UserMessage: "m2", BotReply: "r2"},
			{UserMessage: "m3", BotReply: "r3"},
			{UserMessage: "m4", BotReply: "r4"},
		},
	}

	decision := domain.LLMDecision{Reply: "r5"}
	res, _, _ := eng.ApplyDecision(spec, p, nil, "m5", decision)

	if res.InteractionCount != 3 {
		t.Fatalf("expected InteractionCount 3, got %d", res.InteractionCount)
	}
	if !res.UpdatedAt.Equal(fakeNow) {
		t.Fatalf("expected UpdatedAt %v, got %v", fakeNow, res.UpdatedAt)
	}
	if len(res.RecentTurns) != social.MaxRecentTurns {
		t.Fatalf("expected %d turns, got %d", social.MaxRecentTurns, len(res.RecentTurns))
	}
	if res.RecentTurns[0].UserMessage != "m2" || res.RecentTurns[3].UserMessage != "m5" {
		t.Fatalf("expected FIFO eviction of m1, got %+v", res.RecentTurns)
	}
	if res.RecentTurns[3].BotName != spec.BotName || res.RecentTurns[3].PersonaID != spec.ID {
		t.Fatalf("expected newest turn attributed to %s/%s, got %+v", spec.BotName, spec.ID, res.RecentTurns[3])
	}
}

func TestApplyDecision_SlotsSyncAndRevocation(t *testing.T) {
	eng := social.NewEngine(nil)
	spec := samplePersona()

	// User currently holds favorito_del_bot (min_affinity: 75), but affinity is dropping to 30
	currentSlots := map[string][]domain.SlotHolder{
		"favorito_del_bot": {
			{SlotID: "favorito_del_bot", UserID: "u1", DisplayName: "ViejoNombre", Affinity: 75},
		},
	}
	profile := domain.ViewerProfile{
		UserID:      "u1",
		DisplayName: "NuevoNombre",
		Affinity:    35,
	}

	decision := domain.LLMDecision{Reply: "Me has decepcionado", AffinityDelta: -5} // final affinity 30 < 75
	resProfile, nextSlots, _ := eng.ApplyDecision(spec, profile, currentSlots, "chao", decision)

	if resProfile.Affinity != 30 {
		t.Fatalf("expected affinity 30, got %d", resProfile.Affinity)
	}
	if len(nextSlots["favorito_del_bot"]) != 0 {
		t.Fatalf("expected slot to be revoked due to affinity < 75, got %+v", nextSlots["favorito_del_bot"])
	}

	// Slot holder kept and updated if still eligible
	profile2 := domain.ViewerProfile{
		UserID:      "u1",
		DisplayName: "NuevoNombre",
		Affinity:    75,
	}
	d2 := domain.LLMDecision{Reply: "Bien", AffinityDelta: 5} // final affinity 80 >= 75
	_, nextSlots2, _ := eng.ApplyDecision(spec, profile2, currentSlots, "hola", d2)

	if len(nextSlots2["favorito_del_bot"]) != 1 {
		t.Fatalf("expected 1 holder in slot, got %d", len(nextSlots2["favorito_del_bot"]))
	}
	h := nextSlots2["favorito_del_bot"][0]
	if h.DisplayName != "NuevoNombre" || h.Affinity != 80 {
		t.Fatalf("expected updated holder DisplayName and Affinity 80, got %+v", h)
	}
}

func TestApplyDecision_SlotClaim(t *testing.T) {
	now := time.Date(2026, 11, 12, 14, 0, 0, 0, time.UTC)
	clock := domain.NewFakeClock(now)
	eng := social.NewEngine(clock)
	spec := samplePersona()

	// 1. Ineligible claim (Affinity too low for favorito_del_bot min: 75)
	pLow := domain.ViewerProfile{UserID: "uLow", DisplayName: "Low", Affinity: 50}
	dLow := domain.LLMDecision{Reply: "no", SlotClaim: "favorito_del_bot"}
	_, slots1, event1 := eng.ApplyDecision(spec, pLow, nil, "quiero ser favorito", dLow)
	if event1 != "" || len(slots1["favorito_del_bot"]) != 0 {
		t.Fatalf("expected claim rejection for ineligible affinity, got event=%q slots=%+v", event1, slots1)
	}

	// 2. Eligible claim with open slot
	pHigh := domain.ViewerProfile{UserID: "uHigh", DisplayName: "High", Affinity: 80}
	dHigh := domain.LLMDecision{Reply: "eres mi fav", SlotClaim: "favorito_del_bot"}
	_, slots2, event2 := eng.ApplyDecision(spec, pHigh, nil, "quiero ser favorito", dHigh)
	if event2 != "favorito_del_bot" {
		t.Fatalf("expected slotEvent favorito_del_bot, got %q", event2)
	}
	if len(slots2["favorito_del_bot"]) != 1 || slots2["favorito_del_bot"][0].UserID != "uHigh" {
		t.Fatalf("expected uHigh in slot, got %+v", slots2["favorito_del_bot"])
	}

	// 3. User already holds the slot - no new slotEvent, updates data
	dSame := domain.LLMDecision{Reply: "sigues siendo el fav", SlotClaim: "favorito_del_bot"}
	_, slots3, event3 := eng.ApplyDecision(spec, pHigh, slots2, "hola", dSame)
	if event3 != "" {
		t.Fatalf("expected empty slotEvent for existing holder, got %q", event3)
	}
	if len(slots3["favorito_del_bot"]) != 1 {
		t.Fatalf("expected 1 holder, got %d", len(slots3["favorito_del_bot"]))
	}

	// 4. Outrank steal positive slot: rival with affinity 90 steals from uHigh (80)
	pRival := domain.ViewerProfile{UserID: "uRival", DisplayName: "Rival", Affinity: 90}
	dSteal := domain.LLMDecision{Reply: "nuevo fav", SlotClaim: "favorito_del_bot"}
	_, slots4, event4 := eng.ApplyDecision(spec, pRival, slots2, "soy mejor", dSteal)
	if event4 != "favorito_del_bot" {
		t.Fatalf("expected steal slotEvent favorito_del_bot, got %q", event4)
	}
	if len(slots4["favorito_del_bot"]) != 1 || slots4["favorito_del_bot"][0].UserID != "uRival" {
		t.Fatalf("expected uRival to steal slot, got %+v", slots4["favorito_del_bot"])
	}

	// 5. Outrank steal positive slot rejected if candidate affinity <= weakest holder
	pWeak := domain.ViewerProfile{UserID: "uWeak", DisplayName: "Weak", Affinity: 75}
	dWeak := domain.LLMDecision{Reply: "no me ganas", SlotClaim: "favorito_del_bot"}
	_, slots5, event5 := eng.ApplyDecision(spec, pWeak, slots4, "intento", dWeak)
	if event5 != "" {
		t.Fatalf("expected no steal event, got %q", event5)
	}
	if slots5["favorito_del_bot"][0].UserID != "uRival" {
		t.Fatalf("expected uRival to remain, got %+v", slots5["favorito_del_bot"])
	}

	// 6. Outrank steal negative slot (enemigo_publico: max_affinity: -35)
	// Weakest holder is the one with highest (least negative) affinity, e.g. -40 vs -70
	slotsNeg := map[string][]domain.SlotHolder{
		"enemigo_publico": {
			{SlotID: "enemigo_publico", UserID: "bad1", DisplayName: "Bad1", Affinity: -40},
		},
	}
	pMoreBad := domain.ViewerProfile{UserID: "bad2", DisplayName: "Bad2", Affinity: -70}
	dBad := domain.LLMDecision{Reply: "el peor enemigo", SlotClaim: "enemigo_publico"}
	_, slotsNegRes, eventNeg := eng.ApplyDecision(spec, pMoreBad, slotsNeg, "te odio mas", dBad)
	if eventNeg != "enemigo_publico" {
		t.Fatalf("expected enemigo_publico steal event, got %q", eventNeg)
	}
	if slotsNegRes["enemigo_publico"][0].UserID != "bad2" {
		t.Fatalf("expected bad2 (-70) to steal from bad1 (-40), got %+v", slotsNegRes["enemigo_publico"])
	}

	// Candidate with -38 cannot steal from bad2 (-70)
	pLessBad := domain.ViewerProfile{UserID: "bad3", DisplayName: "Bad3", Affinity: -38}
	_, slotsNegRes2, eventNeg2 := eng.ApplyDecision(spec, pLessBad, slotsNegRes, "te odio", dBad)
	if eventNeg2 != "" {
		t.Fatalf("expected no steal event for bad3, got %q", eventNeg2)
	}
	if slotsNegRes2["enemigo_publico"][0].UserID != "bad2" {
		t.Fatalf("expected bad2 to remain, got %+v", slotsNegRes2["enemigo_publico"])
	}

	// 7. Slot full and AllowOutrankSteal is false (complice: max_holders 2, allow_outrank_steal: false)
	slotsComplice := map[string][]domain.SlotHolder{
		"complice": {
			{SlotID: "complice", UserID: "c1", DisplayName: "C1", Affinity: 75},
			{SlotID: "complice", UserID: "c2", DisplayName: "C2", Affinity: 80},
		},
	}
	pC3 := domain.ViewerProfile{UserID: "c3", DisplayName: "C3", Affinity: 95}
	dC3 := domain.LLMDecision{Reply: "quiero entrar", SlotClaim: "complice"}
	_, slotsCRes, eventC := eng.ApplyDecision(spec, pC3, slotsComplice, "hola", dC3)
	if eventC != "" {
		t.Fatalf("expected no event when AllowOutrankSteal=false, got %q", eventC)
	}
	if len(slotsCRes["complice"]) != 2 {
		t.Fatalf("expected 2 holders in complice, got %d", len(slotsCRes["complice"]))
	}
}
