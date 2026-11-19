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
