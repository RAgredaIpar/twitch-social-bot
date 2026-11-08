package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/store"
)

// Ensure HybridStore implements domain.Store at compile time.
var _ domain.Store = (*store.HybridStore)(nil)

func TestStore_Open_Memory(t *testing.T) {
	ctx := context.Background()

	for _, path := range []string{"", ":memory:"} {
		t.Run("path="+path, func(t *testing.T) {
			s, err := store.Open(path)
			if err != nil {
				t.Fatalf("Open(%q) unexpected error: %v", path, err)
			}
			defer s.Close()

			prof := domain.ViewerProfile{
				UserID:      "u1",
				Username:    "alice",
				DisplayName: "Alice",
				Affinity:    42,
			}

			if err := s.SaveProfile(ctx, prof); err != nil {
				t.Fatalf("SaveProfile failed: %v", err)
			}

			got, ok, err := s.GetProfile(ctx, "u1")
			if err != nil {
				t.Fatalf("GetProfile failed: %v", err)
			}
			if !ok || got.UserID != "u1" || got.Affinity != 42 {
				t.Fatalf("unexpected profile: %+v, ok=%v", got, ok)
			}
		})
	}
}

func TestStore_FilePersistence_AcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nested", "data", "social.db")

	// Phase 1: create store and populate state
	s1, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	prof := domain.ViewerProfile{
		UserID:           "12345",
		Username:         "mod_user",
		DisplayName:      "Mod_User",
		Affinity:         88,
		TierID:           "amigo",
		EarnedTitle:      "El Sabio",
		Facts:            []string{"Le gusta el café", "Juega ajedrez"},
		RecentTurns:      []domain.ConversationTurn{{UserMessage: "hola", BotReply: "buenas", Timestamp: now}},
		InteractionCount: 5,
		UpdatedAt:        now,
	}

	if err := s1.SaveProfile(ctx, prof); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	slots := map[string][]domain.SlotHolder{
		"slot_vip": {
			{
				SlotID:      "slot_vip",
				UserID:      "12345",
				DisplayName: "Mod_User",
				Affinity:    88,
				AssignedAt:  now,
			},
		},
	}
	if err := s1.SaveSlots(ctx, "persona_alpha", slots); err != nil {
		t.Fatalf("SaveSlots failed: %v", err)
	}

	if err := s1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify file was written to disk
	if info, err := os.Stat(dbPath); err != nil || info.Size() == 0 {
		t.Fatalf("expected non-empty file at %q, err=%v", dbPath, err)
	}

	// Phase 2: reopen store from same file
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open reopen failed: %v", err)
	}
	defer s2.Close()

	gotProf, ok, err := s2.GetProfile(ctx, "12345")
	if err != nil {
		t.Fatalf("GetProfile on reopened store failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected profile to exist")
	}
	if gotProf.UserID != prof.UserID || gotProf.Username != prof.Username || gotProf.DisplayName != prof.DisplayName {
		t.Errorf("profile identity mismatch: got %+v, want %+v", gotProf, prof)
	}
	if gotProf.Affinity != prof.Affinity || gotProf.TierID != prof.TierID || gotProf.EarnedTitle != prof.EarnedTitle {
		t.Errorf("profile attributes mismatch: got %+v, want %+v", gotProf, prof)
	}
	if len(gotProf.Facts) != 2 || gotProf.Facts[0] != prof.Facts[0] || gotProf.Facts[1] != prof.Facts[1] {
		t.Errorf("profile facts mismatch: got %+v, want %+v", gotProf.Facts, prof.Facts)
	}
	if len(gotProf.RecentTurns) != 1 || gotProf.RecentTurns[0].UserMessage != "hola" {
		t.Errorf("profile turns mismatch: got %+v", gotProf.RecentTurns)
	}

	gotSlots, err := s2.GetSlots(ctx, "persona_alpha")
	if err != nil {
		t.Fatalf("GetSlots on reopened store failed: %v", err)
	}
	holders, ok := gotSlots["slot_vip"]
	if !ok || len(holders) != 1 || holders[0].UserID != "12345" {
		t.Fatalf("slots mismatch: got %+v", gotSlots)
	}
}

func TestStore_GetProfile_NotFoundAndContext(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	prof, found, err := s.GetProfile(ctx, "non_existent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false, got true")
	}
	if prof.UserID != "" {
		t.Fatalf("expected empty profile, got %+v", prof)
	}

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = s.GetProfile(canceledCtx, "any")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestStore_SaveProfile_ValidationAndContext(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Empty UserID validation
	err = s.SaveProfile(ctx, domain.ViewerProfile{UserID: ""})
	if err == nil {
		t.Fatalf("expected error for empty UserID, got nil")
	}
	if !errors.Is(err, store.ErrEmptyUserID) {
		t.Errorf("expected ErrEmptyUserID, got: %v", err)
	}

	// Whitespace-only UserID validation
	err = s.SaveProfile(ctx, domain.ViewerProfile{UserID: "   "})
	if err == nil || !errors.Is(err, store.ErrEmptyUserID) {
		t.Fatalf("expected ErrEmptyUserID for whitespace UserID, got %v", err)
	}

	// Canceled context
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	err = s.SaveProfile(canceledCtx, domain.ViewerProfile{UserID: "valid"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestStore_ResetProfile_RemovesProfileAndSlotHolders(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user 1 and user 2
	p1 := domain.ViewerProfile{UserID: "user_1", Username: "u1"}
	p2 := domain.ViewerProfile{UserID: "user_2", Username: "u2"}
	if err := s.SaveProfile(ctx, p1); err != nil {
		t.Fatalf("SaveProfile 1: %v", err)
	}
	if err := s.SaveProfile(ctx, p2); err != nil {
		t.Fatalf("SaveProfile 2: %v", err)
	}

	// Assign slots across two personas
	slotsP1 := map[string][]domain.SlotHolder{
		"vip": {
			{SlotID: "vip", UserID: "user_1", DisplayName: "U1"},
			{SlotID: "vip", UserID: "user_2", DisplayName: "U2"},
		},
		"mod": {
			{SlotID: "mod", UserID: "user_1", DisplayName: "U1"},
		},
	}
	slotsP2 := map[string][]domain.SlotHolder{
		"nemesis": {
			{SlotID: "nemesis", UserID: "user_1", DisplayName: "U1"},
		},
	}

	if err := s.SaveSlots(ctx, "persona_1", slotsP1); err != nil {
		t.Fatalf("SaveSlots persona_1: %v", err)
	}
	if err := s.SaveSlots(ctx, "persona_2", slotsP2); err != nil {
		t.Fatalf("SaveSlots persona_2: %v", err)
	}

	// Reset user_1
	if err := s.ResetProfile(ctx, "user_1"); err != nil {
		t.Fatalf("ResetProfile failed: %v", err)
	}

	// Profile for user_1 must be gone
	_, found, err := s.GetProfile(ctx, "user_1")
	if err != nil {
		t.Fatalf("GetProfile user_1 failed: %v", err)
	}
	if found {
		t.Fatalf("expected user_1 profile to be removed")
	}

	// Profile for user_2 must remain
	p2Got, found, err := s.GetProfile(ctx, "user_2")
	if err != nil || !found || p2Got.UserID != "user_2" {
		t.Fatalf("expected user_2 to remain intact")
	}

	// Slots for persona_1: user_1 removed, user_2 still in vip, mod is empty
	p1SlotsGot, err := s.GetSlots(ctx, "persona_1")
	if err != nil {
		t.Fatalf("GetSlots persona_1: %v", err)
	}
	if len(p1SlotsGot["vip"]) != 1 || p1SlotsGot["vip"][0].UserID != "user_2" {
		t.Fatalf("expected only user_2 in vip, got %+v", p1SlotsGot["vip"])
	}
	if len(p1SlotsGot["mod"]) != 0 {
		t.Fatalf("expected mod slot to be empty, got %+v", p1SlotsGot["mod"])
	}

	// Slots for persona_2: nemesis is empty
	p2SlotsGot, err := s.GetSlots(ctx, "persona_2")
	if err != nil {
		t.Fatalf("GetSlots persona_2: %v", err)
	}
	if len(p2SlotsGot["nemesis"]) != 0 {
		t.Fatalf("expected nemesis slot to be empty, got %+v", p2SlotsGot["nemesis"])
	}

	// Reset with empty user ID should fail
	if err := s.ResetProfile(ctx, ""); !errors.Is(err, store.ErrEmptyUserID) {
		t.Fatalf("expected ErrEmptyUserID for empty reset, got %v", err)
	}

	// Context canceled
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.ResetProfile(canceledCtx, "user_2"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestStore_FindProfileByUsername(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	p := domain.ViewerProfile{
		UserID:      "u99",
		Username:    "el_rubius",
		DisplayName: "El_Rubius_OMG",
		Affinity:    50,
		Facts:       []string{"Streamer"},
	}
	if err := s.SaveProfile(ctx, p); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	tests := []struct {
		name      string
		query     string
		wantFound bool
	}{
		{"exact username", "el_rubius", true},
		{"uppercase username", "EL_RUBIUS", true},
		{"with @ prefix username", "@el_rubius", true},
		{"with @ prefix uppercase", "@EL_RUBIUS", true},
		{"exact display name", "El_Rubius_OMG", true},
		{"lowercase display name", "el_rubius_omg", true},
		{"with @ prefix display name", "@El_Rubius_OMG", true},
		{"non-existent", "unknown_user", false},
		{"empty query", "", false},
		{"only @", "@", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, found, err := s.FindProfileByUsername(ctx, tc.query)
			if err != nil {
				t.Fatalf("FindProfileByUsername unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("query %q: wantFound=%v, got=%v", tc.query, tc.wantFound, found)
			}
			if found {
				if got.UserID != p.UserID {
					t.Fatalf("query %q: expected UserID %s, got %s", tc.query, p.UserID, got.UserID)
				}
			}
		})
	}

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = s.FindProfileByUsername(canceledCtx, "el_rubius")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestStore_Slots_GetAndSave(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Getting non-existent persona returns non-nil empty map
	emptySlots, err := s.GetSlots(ctx, "non_existent_persona")
	if err != nil {
		t.Fatalf("GetSlots unexpected error: %v", err)
	}
	if emptySlots == nil {
		t.Fatalf("expected non-nil map")
	}
	if len(emptySlots) != 0 {
		t.Fatalf("expected 0 slots, got %d", len(emptySlots))
	}

	// Save and retrieve
	toSave := map[string][]domain.SlotHolder{
		"bestie": {
			{SlotID: "bestie", UserID: "u1", DisplayName: "U1", Affinity: 99},
		},
	}
	if err := s.SaveSlots(ctx, "persona_cohost", toSave); err != nil {
		t.Fatalf("SaveSlots: %v", err)
	}

	got, err := s.GetSlots(ctx, "persona_cohost")
	if err != nil {
		t.Fatalf("GetSlots: %v", err)
	}
	if len(got["bestie"]) != 1 || got["bestie"][0].UserID != "u1" {
		t.Fatalf("slots mismatch: %+v", got)
	}

	// Context canceled
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.GetSlots(canceledCtx, "persona_cohost")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on GetSlots, got %v", err)
	}
	err = s.SaveSlots(canceledCtx, "persona_cohost", toSave)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on SaveSlots, got %v", err)
	}
}

func TestStore_DeepCopy_Immutability(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. SaveProfile isolation: caller mutating original slice does not mutate store
	origFacts := []string{"fact1", "fact2"}
	origTurns := []domain.ConversationTurn{{UserMessage: "m1", BotReply: "r1"}}
	p := domain.ViewerProfile{
		UserID:      "u_iso",
		Username:    "iso_user",
		DisplayName: "Iso User",
		Facts:       origFacts,
		RecentTurns: origTurns,
	}
	if err := s.SaveProfile(ctx, p); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	origFacts[0] = "mutated_fact"
	origTurns[0].UserMessage = "mutated_turn"

	saved, ok, err := s.GetProfile(ctx, "u_iso")
	if err != nil || !ok {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if saved.Facts[0] != "fact1" {
		t.Errorf("store internal facts was mutated via caller slice: got %q, want 'fact1'", saved.Facts[0])
	}
	if saved.RecentTurns[0].UserMessage != "m1" {
		t.Errorf("store internal turns was mutated via caller slice: got %q, want 'm1'", saved.RecentTurns[0].UserMessage)
	}

	// 2. GetProfile isolation: caller mutating returned slice does not mutate store
	saved.Facts[0] = "mutated_again"
	saved.RecentTurns[0].UserMessage = "mutated_again_turn"

	saved2, _, _ := s.GetProfile(ctx, "u_iso")
	if saved2.Facts[0] != "fact1" {
		t.Errorf("store internal facts was mutated via GetProfile return: got %q, want 'fact1'", saved2.Facts[0])
	}
	if saved2.RecentTurns[0].UserMessage != "m1" {
		t.Errorf("store internal turns was mutated via GetProfile return: got %q, want 'm1'", saved2.RecentTurns[0].UserMessage)
	}

	// 3. FindProfileByUsername isolation: mutating returned profile does not mutate store
	foundP, ok, err := s.FindProfileByUsername(ctx, "iso_user")
	if err != nil || !ok {
		t.Fatalf("FindProfileByUsername failed: %v", err)
	}
	foundP.Facts[0] = "mutated_via_find"

	saved3, _, _ := s.GetProfile(ctx, "u_iso")
	if saved3.Facts[0] != "fact1" {
		t.Errorf("store internal facts was mutated via FindProfile return: got %q, want 'fact1'", saved3.Facts[0])
	}

	// 4. SaveSlots & GetSlots isolation
	origHolders := []domain.SlotHolder{
		{SlotID: "s1", UserID: "u_iso", DisplayName: "OrigName"},
	}
	slotsMap := map[string][]domain.SlotHolder{
		"s1": origHolders,
	}
	if err := s.SaveSlots(ctx, "p1", slotsMap); err != nil {
		t.Fatalf("SaveSlots failed: %v", err)
	}

	origHolders[0].DisplayName = "MutatedName"
	slotsMap["s2"] = []domain.SlotHolder{{SlotID: "s2", UserID: "hacker"}}

	gotSlots, err := s.GetSlots(ctx, "p1")
	if err != nil {
		t.Fatalf("GetSlots failed: %v", err)
	}
	if gotSlots["s1"][0].DisplayName != "OrigName" {
		t.Errorf("slots internal slice was mutated via caller slice: got %s, want OrigName", gotSlots["s1"][0].DisplayName)
	}
	if _, exists := gotSlots["s2"]; exists {
		t.Errorf("slots internal map was mutated via caller map")
	}

	gotSlots["s1"][0].DisplayName = "MutatedFromGet"
	gotSlots2, _ := s.GetSlots(ctx, "p1")
	if gotSlots2["s1"][0].DisplayName != "OrigName" {
		t.Errorf("slots internal slice was mutated via GetSlots return")
	}
}

func TestStore_ErrStoreClosed(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	ctx := context.Background()

	// Subsequent operations must return ErrStoreClosed
	if _, _, err := s.GetProfile(ctx, "u1"); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("GetProfile after Close: got %v, want ErrStoreClosed", err)
	}
	if err := s.SaveProfile(ctx, domain.ViewerProfile{UserID: "u1"}); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("SaveProfile after Close: got %v, want ErrStoreClosed", err)
	}
	if err := s.ResetProfile(ctx, "u1"); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("ResetProfile after Close: got %v, want ErrStoreClosed", err)
	}
	if _, _, err := s.FindProfileByUsername(ctx, "u1"); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("FindProfileByUsername after Close: got %v, want ErrStoreClosed", err)
	}
	if _, err := s.GetSlots(ctx, "p1"); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("GetSlots after Close: got %v, want ErrStoreClosed", err)
	}
	if err := s.SaveSlots(ctx, "p1", nil); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("SaveSlots after Close: got %v, want ErrStoreClosed", err)
	}
	if err := s.Close(); !errors.Is(err, store.ErrStoreClosed) {
		t.Errorf("Close after Close: got %v, want ErrStoreClosed", err)
	}
}

func TestStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(badFile, []byte("{invalid-json"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := store.Open(badFile)
	if err == nil {
		t.Fatalf("expected error opening corrupted file, got nil")
	}
}

func TestStore_Concurrency_Stress(t *testing.T) {
	// Concurrent stress testing across multiple goroutines
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	const numWorkers = 20
	const iterationsPerWorker = 50

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerWorker; i++ {
				uid := "user_" + strconv.Itoa(workerID)
				uname := "name_" + strconv.Itoa(workerID)

				// Save
				p := domain.ViewerProfile{
					UserID:      uid,
					Username:    uname,
					DisplayName: uname,
					Affinity:    i,
					Facts:       []string{"fact_" + strconv.Itoa(i)},
				}
				if err := s.SaveProfile(ctx, p); err != nil {
					t.Errorf("concurrent SaveProfile failed: %v", err)
					return
				}

				// Get
				got, ok, err := s.GetProfile(ctx, uid)
				if err != nil || !ok || got.UserID != uid {
					t.Errorf("concurrent GetProfile failed: %v, ok=%v, got=%+v", err, ok, got)
					return
				}

				// Find
				found, ok, err := s.FindProfileByUsername(ctx, "@"+uname)
				if err != nil || !ok || found.UserID != uid {
					t.Errorf("concurrent FindProfileByUsername failed: %v, ok=%v", err, ok)
					return
				}

				// Slots
				slotMap := map[string][]domain.SlotHolder{
					"vip": {
						{SlotID: "vip", UserID: uid, DisplayName: uname, Affinity: i},
					},
				}
				personaID := "persona_" + strconv.Itoa(workerID%3)
				if err := s.SaveSlots(ctx, personaID, slotMap); err != nil {
					t.Errorf("concurrent SaveSlots failed: %v", err)
					return
				}

				if _, err := s.GetSlots(ctx, personaID); err != nil {
					t.Errorf("concurrent GetSlots failed: %v", err)
					return
				}

				// Occasionally reset
				if i%20 == 0 {
					if err := s.ResetProfile(ctx, uid); err != nil {
						t.Errorf("concurrent ResetProfile failed: %v", err)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}
