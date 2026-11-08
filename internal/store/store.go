package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/twitch-social-bot/internal/domain"
)

var (
	// ErrStoreClosed indicates that the store has been closed and cannot perform further operations.
	ErrStoreClosed = errors.New("store is closed")

	// ErrEmptyUserID indicates an attempt to save or reset a profile with an empty user_id.
	ErrEmptyUserID = errors.New("user_id cannot be empty")
)

// Ensure HybridStore implements domain.Store at compile time.
var _ domain.Store = (*HybridStore)(nil)

type snapshot struct {
	Profiles map[string]domain.ViewerProfile           `json:"profiles"`
	Slots    map[string]map[string][]domain.SlotHolder `json:"slots"`
}

// HybridStore provides concurrent in-memory caching with deep-copy isolation
// and atomic file-backed persistence.
type HybridStore struct {
	mu       sync.RWMutex
	path     string
	isMemory bool
	closed   bool
	profiles map[string]domain.ViewerProfile
	slots    map[string]map[string][]domain.SlotHolder
}

// Open creates a new HybridStore.
// If path is empty or ":memory:", the store operates purely in-memory without disk I/O.
// If path is a file path, it loads any existing snapshot and flushes mutations atomically.
func Open(path string) (*HybridStore, error) {
	isMemory := path == "" || path == ":memory:"

	s := &HybridStore{
		path:     path,
		isMemory: isMemory,
		profiles: make(map[string]domain.ViewerProfile),
		slots:    make(map[string]map[string][]domain.SlotHolder),
	}

	if isMemory {
		return s, nil
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create store directory: %w", err)
		}
	}

	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read store file: %w", err)
		}

		var snap snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, fmt.Errorf("unmarshal store data: %w", err)
		}

		if snap.Profiles != nil {
			s.profiles = snap.Profiles
		}
		if snap.Slots != nil {
			s.slots = snap.Slots
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat store file: %w", err)
	}

	return s, nil
}

// GetProfile retrieves a viewer's profile by userID with deep-copy isolation.
func (s *HybridStore) GetProfile(ctx context.Context, userID string) (domain.ViewerProfile, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.ViewerProfile{}, false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return domain.ViewerProfile{}, false, err
	}
	if s.closed {
		return domain.ViewerProfile{}, false, ErrStoreClosed
	}

	p, exists := s.profiles[userID]
	if !exists {
		return domain.ViewerProfile{}, false, nil
	}

	return cloneProfile(p), true, nil
}

// SaveProfile stores a deep copy of profile in memory and flushes atomically to disk if file-backed.
func (s *HybridStore) SaveProfile(ctx context.Context, profile domain.ViewerProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(profile.UserID) == "" {
		return ErrEmptyUserID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	s.profiles[profile.UserID] = cloneProfile(profile)

	return s.flushLocked()
}

// ResetProfile deletes the viewer's profile and removes them from all slots across all personas.
func (s *HybridStore) ResetProfile(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrEmptyUserID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	delete(s.profiles, userID)

	for personaID, personaSlots := range s.slots {
		for slotID, holders := range personaSlots {
			filtered := make([]domain.SlotHolder, 0, len(holders))
			for _, h := range holders {
				if h.UserID != userID {
					filtered = append(filtered, h)
				}
			}
			personaSlots[slotID] = filtered
		}
		s.slots[personaID] = personaSlots
	}

	return s.flushLocked()
}

// FindProfileByUsername finds a profile case-insensitively by Username or DisplayName,
// ignoring an optional leading '@'.
func (s *HybridStore) FindProfileByUsername(ctx context.Context, username string) (domain.ViewerProfile, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.ViewerProfile{}, false, err
	}

	clean := strings.TrimPrefix(username, "@")
	if clean == "" {
		return domain.ViewerProfile{}, false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return domain.ViewerProfile{}, false, err
	}
	if s.closed {
		return domain.ViewerProfile{}, false, ErrStoreClosed
	}

	for _, p := range s.profiles {
		if strings.EqualFold(p.Username, clean) || strings.EqualFold(p.DisplayName, clean) {
			return cloneProfile(p), true, nil
		}
	}

	return domain.ViewerProfile{}, false, nil
}

// GetSlots returns a deep-copied map of slotID -> []SlotHolder for the given persona.
// Returns an empty, non-nil map if none exist.
func (s *HybridStore) GetSlots(ctx context.Context, personaID string) (map[string][]domain.SlotHolder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, ErrStoreClosed
	}

	personaSlots, exists := s.slots[personaID]
	if !exists || personaSlots == nil {
		return make(map[string][]domain.SlotHolder), nil
	}

	return cloneSlotsMap(personaSlots), nil
}

// SaveSlots stores a deep copy of slots for personaID and flushes atomically to disk if file-backed.
func (s *HybridStore) SaveSlots(ctx context.Context, personaID string, slots map[string][]domain.SlotHolder) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	s.slots[personaID] = cloneSlotsMap(slots)

	return s.flushLocked()
}

// Close marks the store closed and flushes pending data if file-backed.
func (s *HybridStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	if err := s.flushLocked(); err != nil {
		return err
	}

	s.closed = true
	return nil
}

// flushLocked writes a JSON snapshot atomically to disk using a temp file.
// Must be called with s.mu held.
func (s *HybridStore) flushLocked() error {
	if s.isMemory {
		return nil
	}

	data, err := json.MarshalIndent(snapshot{
		Profiles: s.profiles,
		Slots:    s.slots,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store snapshot: %w", err)
	}

	tmpPath := s.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create temp snapshot file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write snapshot file: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync snapshot file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp snapshot file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename snapshot file: %w", err)
	}

	return nil
}

func cloneProfile(p domain.ViewerProfile) domain.ViewerProfile {
	cp := p
	if p.Facts != nil {
		cp.Facts = make([]string, len(p.Facts))
		copy(cp.Facts, p.Facts)
	}
	if p.RecentTurns != nil {
		cp.RecentTurns = make([]domain.ConversationTurn, len(p.RecentTurns))
		copy(cp.RecentTurns, p.RecentTurns)
	}
	return cp
}

func cloneSlotsMap(slots map[string][]domain.SlotHolder) map[string][]domain.SlotHolder {
	if slots == nil {
		return make(map[string][]domain.SlotHolder)
	}
	res := make(map[string][]domain.SlotHolder, len(slots))
	for k, list := range slots {
		if list == nil {
			res[k] = []domain.SlotHolder{}
			continue
		}
		clonedList := make([]domain.SlotHolder, len(list))
		copy(clonedList, list)
		res[k] = clonedList
	}
	return res
}
