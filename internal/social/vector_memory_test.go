package social_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/twitch-social-bot/internal/social"
)

func TestSelectRelevantFacts_SemanticAndRecent(t *testing.T) {
	facts := []string{
		"Tiene un perro llamado Toby que ladra en los directos",
		"Estudia ingeniería de sistemas",
		"Vive en Bogotá",
	}
	for i := 0; i < 40; i++ {
		facts = append(facts, fmt.Sprintf("Le gusta jugar partidas casuales número %d", i))
	}
	facts = append(facts,
		"Hoy cenó tacos al pastor",
		"Acaba de llegar del gimnasio",
		"Preguntó por el horario de mañana",
	)

	// Query about his puppy -> should surface the old dog fact despite 40 filler facts + morphological variation ("perrito" ~ "perro")
	got := social.SelectRelevantFacts(facts, "hoy saqué a pasear a mi perrito", 5, 3)

	if len(got) > 8 {
		t.Fatalf("expected at most 8 facts, got %d", len(got))
	}
	if !slices.Contains(got, "Tiene un perro llamado Toby que ladra en los directos") {
		t.Fatalf("expected semantic dog fact in result, got: %+v", got)
	}
	// Latest 3 facts must always be retained
	for _, recent := range facts[len(facts)-3:] {
		if !slices.Contains(got, recent) {
			t.Fatalf("expected recent fact %q in result, got: %+v", recent, got)
		}
	}
}

func TestSelectContextualFacts_CoolsDownFactJustMentionedByBot(t *testing.T) {
	facts := []string{
		"Tiene un perro llamado Firulais",
		"Mainea Yasuo en LoL",
		"Su comida favorita es el ceviche",
	}

	// Bot just joked about Yasuo; viewer now talks about ceviche (unrelated to Yasuo).
	got := social.SelectContextualFacts(
		facts,
		"acuerdate que mi comida favorita es el ceviche",
		"tu yasuo, ese es el chiste",
		5, 3,
	)
	if slices.Contains(got, "Mainea Yasuo en LoL") {
		t.Fatalf("expected just-mentioned Yasuo fact to cool down on unrelated query, got: %v", got)
	}
	if !slices.Contains(got, "Su comida favorita es el ceviche") {
		t.Fatalf("expected relevant ceviche fact to remain available, got: %v", got)
	}

	// If viewer explicitly asks about Yasuo right after, it MUST NOT be hidden.
	gotAsked := social.SelectContextualFacts(
		facts,
		"qué build le armo a mi yasuo hoy?",
		"tu yasuo, ese es el chiste",
		5, 3,
	)
	if !slices.Contains(gotAsked, "Mainea Yasuo en LoL") {
		t.Fatalf("expected Yasuo fact kept when viewer explicitly mentions Yasuo, got: %v", gotAsked)
	}
}
