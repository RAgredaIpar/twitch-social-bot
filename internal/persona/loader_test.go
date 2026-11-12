package persona_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/persona"
)

func intPtr(v int) *int {
	return &v
}

func sampleValidYAML() []byte {
	return []byte(`id: "test_bot"
name: "Test Persona"
bot_name: "TestKiro"
trigger_prefix: "!test"
max_positive_delta: 6
max_negative_delta: -9
max_viewer_facts: 12

base_persona: |
  Eres un bot de prueba.
  Segunda linea con indentacion preservada.

blocked_words:
  - "palabrota1"
  - "palabrota2"

tiers:
  - id: "bajo"
    name: "Nivel Bajo"
    min: -100
    max: 0
    behavior: "Comportamiento bajo"
  - id: "alto"
    name: "Nivel Alto"
    min: 1
    max: 100
    behavior: "Comportamiento alto"

slots:
  - id: "vip"
    title: "Miembro VIP"
    max_holders: 2
    min_affinity: 50
    allow_outrank_steal: true
`)
}

func TestParseYAML_Valid(t *testing.T) {
	spec, err := persona.ParseYAML(sampleValidYAML())
	if err != nil {
		t.Fatalf("unexpected error parsing valid YAML: %v", err)
	}

	if spec.ID != "test_bot" {
		t.Errorf("expected ID 'test_bot', got '%s'", spec.ID)
	}
	if spec.Name != "Test Persona" {
		t.Errorf("expected Name 'Test Persona', got '%s'", spec.Name)
	}
	if spec.BotName != "TestKiro" {
		t.Errorf("expected BotName 'TestKiro', got '%s'", spec.BotName)
	}
	if spec.TriggerPrefix != "!test" {
		t.Errorf("expected TriggerPrefix '!test', got '%s'", spec.TriggerPrefix)
	}
	if spec.MaxPositiveDelta != 6 {
		t.Errorf("expected MaxPositiveDelta 6, got %d", spec.MaxPositiveDelta)
	}
	if spec.MaxNegativeDelta != -9 {
		t.Errorf("expected MaxNegativeDelta -9, got %d", spec.MaxNegativeDelta)
	}
	if spec.MaxViewerFacts != 12 {
		t.Errorf("expected MaxViewerFacts 12, got %d", spec.MaxViewerFacts)
	}

	expectedBase := "Eres un bot de prueba.\nSegunda linea con indentacion preservada."
	if spec.BasePersona != expectedBase {
		t.Errorf("expected BasePersona:\n%q\ngot:\n%q", expectedBase, spec.BasePersona)
	}

	expectedBlocked := []string{"palabrota1", "palabrota2"}
	if !reflect.DeepEqual(spec.BlockedWords, expectedBlocked) {
		t.Errorf("expected BlockedWords %v, got %v", expectedBlocked, spec.BlockedWords)
	}

	if len(spec.Tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(spec.Tiers))
	}
	if spec.Tiers[0].ID != "bajo" || spec.Tiers[0].Min != -100 || spec.Tiers[0].Max != 0 {
		t.Errorf("unexpected tier 0: %+v", spec.Tiers[0])
	}
	if spec.Tiers[1].ID != "alto" || spec.Tiers[1].Min != 1 || spec.Tiers[1].Max != 100 {
		t.Errorf("unexpected tier 1: %+v", spec.Tiers[1])
	}

	if len(spec.Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(spec.Slots))
	}
	slot := spec.Slots[0]
	if slot.ID != "vip" || slot.Title != "Miembro VIP" || slot.MaxHolders != 2 {
		t.Errorf("unexpected slot: %+v", slot)
	}
	if slot.MinAffinity == nil || *slot.MinAffinity != 50 {
		t.Errorf("expected slot MinAffinity 50, got %v", slot.MinAffinity)
	}
	if slot.MaxAffinity != nil {
		t.Errorf("expected slot MaxAffinity nil, got %v", slot.MaxAffinity)
	}
	if !slot.AllowOutrankSteal {
		t.Errorf("expected AllowOutrankSteal true, got false")
	}
}

func TestParseYAML_JSONSupport(t *testing.T) {
	jsonData := []byte(`{
		"id": "json_bot",
		"name": "JSON Persona",
		"bot_name": "KiroJSON",
		"trigger_prefix": "!json",
		"max_positive_delta": 5,
		"max_negative_delta": -5,
		"base_persona": "Persona base en JSON.",
		"blocked_words": ["badword"],
		"tiers": [
			{"id": "tier1", "name": "Tier 1", "min": -100, "max": 0, "behavior": "Comportamiento 1"},
			{"id": "tier2", "name": "Tier 2", "min": 1, "max": 100, "behavior": "Comportamiento 2"}
		],
		"slots": [
			{"id": "slot1", "title": "Slot 1", "max_holders": 1, "min_affinity": 50, "allow_outrank_steal": true}
		]
	}`)

	spec, err := persona.ParseYAML(jsonData)
	if err != nil {
		t.Fatalf("unexpected error parsing JSON: %v", err)
	}
	if spec.ID != "json_bot" {
		t.Errorf("expected ID 'json_bot', got '%s'", spec.ID)
	}
}

func TestValidate(t *testing.T) {
	baseValid := func() *domain.PersonaSpec {
		return &domain.PersonaSpec{
			ID:               "valid_bot",
			Name:             "Valid Bot",
			BotName:          "Kiro",
			TriggerPrefix:    "!bot",
			MaxPositiveDelta: 5,
			MaxNegativeDelta: -8,
			BasePersona:      "Base prompt description",
			BlockedWords:     []string{"spam"},
			Tiers: []domain.TierSpec{
				{ID: "low", Name: "Low", Min: -100, Max: -1, Behavior: "Low behavior"},
				{ID: "high", Name: "High", Min: 0, Max: 100, Behavior: "High behavior"},
			},
			Slots: []domain.SlotSpec{
				{ID: "ally", Title: "Ally", MaxHolders: 1, MinAffinity: intPtr(50), AllowOutrankSteal: true},
			},
		}
	}

	t.Run("nil spec", func(t *testing.T) {
		if err := persona.Validate(nil); err == nil {
			t.Errorf("expected error for nil spec, got nil")
		}
	})

	t.Run("valid spec", func(t *testing.T) {
		if err := persona.Validate(baseValid()); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		s := baseValid()
		s.ID = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for missing ID")
		}
	})

	t.Run("missing Name", func(t *testing.T) {
		s := baseValid()
		s.Name = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for missing Name")
		}
	})

	t.Run("missing BotName", func(t *testing.T) {
		s := baseValid()
		s.BotName = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for missing BotName")
		}
	})

	t.Run("missing TriggerPrefix", func(t *testing.T) {
		s := baseValid()
		s.TriggerPrefix = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for missing TriggerPrefix")
		}
	})

	t.Run("missing BasePersona", func(t *testing.T) {
		s := baseValid()
		s.BasePersona = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for missing BasePersona")
		}
	})

	t.Run("invalid MaxPositiveDelta", func(t *testing.T) {
		s := baseValid()
		s.MaxPositiveDelta = 0
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for MaxPositiveDelta <= 0")
		}
	})

	t.Run("invalid MaxNegativeDelta", func(t *testing.T) {
		s := baseValid()
		s.MaxNegativeDelta = 0
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for MaxNegativeDelta >= 0")
		}
	})

	t.Run("empty Tiers", func(t *testing.T) {
		s := baseValid()
		s.Tiers = nil
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for empty Tiers")
		}
	})

	t.Run("tier min > max", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "bad", Name: "Bad", Min: 10, Max: -10, Behavior: "Bad"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for tier Min > Max")
		}
	})

	t.Run("tiers do not start at -100", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "all", Name: "All", Min: -99, Max: 100, Behavior: "All"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error when tiers do not start at -100")
		}
	})

	t.Run("tiers do not end at 100", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "all", Name: "All", Min: -100, Max: 99, Behavior: "All"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error when tiers do not end at 100")
		}
	})

	t.Run("tier gap", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "low", Name: "Low", Min: -100, Max: -10, Behavior: "Low"},
			{ID: "high", Name: "High", Min: 0, Max: 100, Behavior: "High"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for gap between -10 and 0")
		}
	})

	t.Run("tier overlap", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "low", Name: "Low", Min: -100, Max: 0, Behavior: "Low"},
			{ID: "high", Name: "High", Min: 0, Max: 100, Behavior: "High"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for overlap at 0")
		}
	})

	t.Run("duplicate tier ID", func(t *testing.T) {
		s := baseValid()
		s.Tiers = []domain.TierSpec{
			{ID: "same", Name: "Low", Min: -100, Max: 0, Behavior: "Low"},
			{ID: "same", Name: "High", Min: 1, Max: 100, Behavior: "High"},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for duplicate tier ID")
		}
	})

	t.Run("tier missing name or behavior", func(t *testing.T) {
		s := baseValid()
		s.Tiers[0].Behavior = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for tier missing Behavior")
		}
	})

	t.Run("slot missing ID", func(t *testing.T) {
		s := baseValid()
		s.Slots[0].ID = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for slot missing ID")
		}
	})

	t.Run("slot missing Title", func(t *testing.T) {
		s := baseValid()
		s.Slots[0].Title = ""
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for slot missing Title")
		}
	})

	t.Run("slot MaxHolders < 1", func(t *testing.T) {
		s := baseValid()
		s.Slots[0].MaxHolders = 0
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for slot MaxHolders < 1")
		}
	})

	t.Run("slot missing both MinAffinity and MaxAffinity", func(t *testing.T) {
		s := baseValid()
		s.Slots[0].MinAffinity = nil
		s.Slots[0].MaxAffinity = nil
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error when slot has both MinAffinity and MaxAffinity nil")
		}
	})

	t.Run("slot MinAffinity > MaxAffinity", func(t *testing.T) {
		s := baseValid()
		s.Slots[0].MinAffinity = intPtr(50)
		s.Slots[0].MaxAffinity = intPtr(20)
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error when slot MinAffinity > MaxAffinity")
		}
	})

	t.Run("duplicate slot ID", func(t *testing.T) {
		s := baseValid()
		s.Slots = []domain.SlotSpec{
			{ID: "vip", Title: "VIP 1", MaxHolders: 1, MinAffinity: intPtr(50)},
			{ID: "vip", Title: "VIP 2", MaxHolders: 1, MinAffinity: intPtr(80)},
		}
		if err := persona.Validate(s); err == nil {
			t.Errorf("expected error for duplicate slot ID")
		}
	})
}

func TestManager_Lifecycle(t *testing.T) {
	dir := t.TempDir()

	fileA := filepath.Join(dir, "bot_a.yaml")
	contentA := `id: "bot_a"
name: "Bot A Original"
bot_name: "KiroA"
trigger_prefix: "!bot"
max_positive_delta: 5
max_negative_delta: -8
base_persona: "Base A"
tiers:
  - id: "t1"
    name: "T1"
    min: -100
    max: 100
    behavior: "Todo"
slots:
  - id: "s1"
    title: "S1"
    max_holders: 1
    min_affinity: 50
`
	if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
		t.Fatalf("failed to write fileA: %v", err)
	}

	fileB := filepath.Join(dir, "bot_b.yaml")
	contentB := `id: "bot_b"
name: "Bot B"
bot_name: "KiroB"
trigger_prefix: "!bot"
max_positive_delta: 5
max_negative_delta: -8
base_persona: "Base B"
tiers:
  - id: "t1"
    name: "T1"
    min: -100
    max: 100
    behavior: "Todo"
slots:
  - id: "s1"
    title: "S1"
    max_holders: 1
    min_affinity: 50
`
	if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
		t.Fatalf("failed to write fileB: %v", err)
	}

	// Initialize manager
	mgr, err := persona.NewManager(dir, "bot_a")
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Check Current
	curr := mgr.Current()
	if curr == nil || curr.ID != "bot_a" || curr.Name != "Bot A Original" {
		t.Fatalf("unexpected initial current: %+v", curr)
	}

	// Check List
	list := mgr.List()
	expectedList := []string{"bot_a", "bot_b"}
	if !reflect.DeepEqual(list, expectedList) {
		t.Errorf("expected list %v, got %v", expectedList, list)
	}

	// Switch to bot_b
	switched, err := mgr.Switch("bot_b")
	if err != nil {
		t.Fatalf("failed to switch to bot_b: %v", err)
	}
	if switched.ID != "bot_b" {
		t.Errorf("expected switched ID 'bot_b', got '%s'", switched.ID)
	}
	if mgr.Current().ID != "bot_b" {
		t.Errorf("expected current ID 'bot_b', got '%s'", mgr.Current().ID)
	}

	// Hot reload test: update bot_a on disk
	contentAModified := `id: "bot_a"
name: "Bot A Modificado"
bot_name: "KiroA"
trigger_prefix: "!bot"
max_positive_delta: 5
max_negative_delta: -8
base_persona: "Base A Modificada"
tiers:
  - id: "t1"
    name: "T1"
    min: -100
    max: 100
    behavior: "Todo"
slots:
  - id: "s1"
    title: "S1"
    max_holders: 1
    min_affinity: 50
`
	if err := os.WriteFile(fileA, []byte(contentAModified), 0644); err != nil {
		t.Fatalf("failed to update fileA: %v", err)
	}

	// Switch back to bot_a, verify reload
	reloaded, err := mgr.Switch("bot_a")
	if err != nil {
		t.Fatalf("failed to switch to modified bot_a: %v", err)
	}
	if reloaded.Name != "Bot A Modificado" {
		t.Errorf("expected reloaded name 'Bot A Modificado', got '%s'", reloaded.Name)
	}
	if mgr.Current().Name != "Bot A Modificado" {
		t.Errorf("expected current name 'Bot A Modificado', got '%s'", mgr.Current().Name)
	}

	// Switch to non-existent
	if _, err := mgr.Switch("inexistente"); err == nil {
		t.Errorf("expected error when switching to nonexistent persona, got nil")
	}

	// Initial file not found
	if _, err := persona.NewManager(dir, "no_existe"); err == nil {
		t.Errorf("expected error when creating manager with nonexistent initial persona, got nil")
	}
}

func TestManager_Concurrent(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"bot_x", "bot_y"} {
		fPath := filepath.Join(dir, name+".yaml")
		data := `id: "` + name + `"
name: "` + name + `"
bot_name: "Kiro"
trigger_prefix: "!bot"
max_positive_delta: 5
max_negative_delta: -8
base_persona: "Base"
tiers:
  - id: "t1"
    name: "T1"
    min: -100
    max: 100
    behavior: "Todo"
slots:
  - id: "s1"
    title: "S1"
    max_holders: 1
    min_affinity: 50
`
		if err := os.WriteFile(fPath, []byte(data), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", fPath, err)
		}
	}

	mgr, err := persona.NewManager(dir, "bot_x")
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	var wg sync.WaitGroup
	workers := 20
	iterations := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			target := "bot_x"
			if id%2 == 1 {
				target = "bot_y"
			}
			for j := 0; j < iterations; j++ {
				_ = mgr.Current()
				_ = mgr.List()
				if j%10 == 0 {
					_, _ = mgr.Switch(target)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestProductionTemplates(t *testing.T) {
	// personas are in repo root / personas
	templates := []struct {
		filename    string
		expectedID  string
		expectedBot string
		minTiers    int
		minSlots    int
	}{
		{
			filename:    "cohost_amigos.yaml",
			expectedID:  "cohost_amigos",
			expectedBot: "Mochi",
			minTiers:    5,
			minSlots:    3,
		},
		{
			filename:    "clan_jerarquia.yaml",
			expectedID:  "clan_jerarquia",
			expectedBot: "Dante",
			minTiers:    5,
			minSlots:    3,
		},
		{
			filename:    "rpg_facciones.yaml",
			expectedID:  "rpg_facciones",
			expectedBot: "Lyra",
			minTiers:    5,
			minSlots:    3,
		},
		{
			filename:    "drama_romance.yaml",
			expectedID:  "drama_romance",
			expectedBot: "Celeste",
			minTiers:    5,
			minSlots:    3,
		},
	}

	for _, tc := range templates {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join("..", "..", "personas", tc.filename)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read template %s: %v", path, err)
			}

			spec, err := persona.ParseYAML(data)
			if err != nil {
				t.Fatalf("failed to parse template %s: %v", path, err)
			}

			if spec.ID != tc.expectedID {
				t.Errorf("expected ID '%s', got '%s'", tc.expectedID, spec.ID)
			}
			if spec.BotName != tc.expectedBot {
				t.Errorf("expected BotName '%s', got '%s'", tc.expectedBot, spec.BotName)
			}
			if spec.TriggerPrefix != "!bot" {
				t.Errorf("expected TriggerPrefix '!bot', got '%s'", spec.TriggerPrefix)
			}
			if spec.MaxPositiveDelta != 5 {
				t.Errorf("expected MaxPositiveDelta 5, got %d", spec.MaxPositiveDelta)
			}
			if spec.MaxNegativeDelta != -8 {
				t.Errorf("expected MaxNegativeDelta -8, got %d", spec.MaxNegativeDelta)
			}
			if len(spec.Tiers) < tc.minTiers {
				t.Errorf("expected at least %d tiers, got %d", tc.minTiers, len(spec.Tiers))
			}
			if len(spec.Slots) < tc.minSlots {
				t.Errorf("expected at least %d slots, got %d", tc.minSlots, len(spec.Slots))
			}

			if err := persona.Validate(spec); err != nil {
				t.Errorf("template validation failed: %v", err)
			}
		})
	}
}
