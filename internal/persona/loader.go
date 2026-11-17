package persona

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/twitch-social-bot/internal/domain"
)

var _ domain.PersonaProvider = (*Manager)(nil)

// ParseYAML parses a YAML (or JSON) encoded PersonaSpec and validates it.
func ParseYAML(data []byte) (*domain.PersonaSpec, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // strip UTF-8 BOM if present
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty persona data")
	}

	// JSON format support
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var spec domain.PersonaSpec
		if err := json.Unmarshal(trimmed, &spec); err != nil {
			return nil, fmt.Errorf("json unmarshal failed: %w", err)
		}
		if err := Validate(&spec); err != nil {
			return nil, err
		}
		return &spec, nil
	}

	spec, err := parseYAMLContent(string(data))
	if err != nil {
		return nil, err
	}

	if err := Validate(spec); err != nil {
		return nil, err
	}

	return spec, nil
}

func parseYAMLContent(content string) (*domain.PersonaSpec, error) {
	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	spec := &domain.PersonaSpec{}

	i := 0
	for i < len(rawLines) {
		line := rawLines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		if countIndent(line) != 0 {
			return nil, fmt.Errorf("line %d: unexpected indentation for top-level key: %s", i+1, line)
		}

		clean := stripComment(line)
		colonIdx := strings.Index(clean, ":")
		if colonIdx == -1 {
			return nil, fmt.Errorf("line %d: invalid top-level key line: %s", i+1, line)
		}

		key := strings.TrimSpace(clean[:colonIdx])
		val := strings.TrimSpace(clean[colonIdx+1:])

		switch key {
		case "id":
			spec.ID = unquoteString(val)
			i++
		case "name":
			spec.Name = unquoteString(val)
			i++
		case "bot_name":
			spec.BotName = unquoteString(val)
			i++
		case "trigger_prefix":
			spec.TriggerPrefix = unquoteString(val)
			i++
		case "theme_color":
			spec.ThemeColor = unquoteString(val)
			i++
		case "max_positive_delta":
			v, err := parseInt(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid max_positive_delta: %w", i+1, err)
			}
			spec.MaxPositiveDelta = v
			i++
		case "max_negative_delta":
			v, err := parseInt(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid max_negative_delta: %w", i+1, err)
			}
			spec.MaxNegativeDelta = v
			i++
		case "max_viewer_facts":
			v, err := parseInt(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid max_viewer_facts: %w", i+1, err)
			}
			spec.MaxViewerFacts = v
			i++
		case "base_persona":
			if strings.HasPrefix(val, "|") {
				i++
				var blockLines []string
				baseIndent := -1
				for i < len(rawLines) {
					bLine := rawLines[i]
					bTrimmed := strings.TrimSpace(bLine)
					if bTrimmed == "" {
						blockLines = append(blockLines, "")
						i++
						continue
					}
					indent := countIndent(bLine)
					if indent == 0 {
						break
					}
					if baseIndent == -1 {
						baseIndent = indent
					}
					if indent >= baseIndent {
						blockLines = append(blockLines, bLine[baseIndent:])
					} else {
						blockLines = append(blockLines, strings.TrimLeft(bLine, " \t"))
					}
					i++
				}
				for len(blockLines) > 0 && blockLines[len(blockLines)-1] == "" {
					blockLines = blockLines[:len(blockLines)-1]
				}
				spec.BasePersona = strings.Join(blockLines, "\n")
			} else {
				spec.BasePersona = unquoteString(val)
				i++
			}
		case "blocked_words":
			i++
			for i < len(rawLines) {
				bLine := rawLines[i]
				bTrimmed := strings.TrimSpace(bLine)
				if bTrimmed == "" || strings.HasPrefix(bTrimmed, "#") {
					i++
					continue
				}
				if countIndent(bLine) == 0 {
					break
				}
				bClean := strings.TrimSpace(stripComment(bLine))
				if strings.HasPrefix(bClean, "-") {
					word := strings.TrimSpace(bClean[1:])
					spec.BlockedWords = append(spec.BlockedWords, unquoteString(word))
				}
				i++
			}
		case "avatar_rows":
			i++
			for i < len(rawLines) {
				aLine := rawLines[i]
				aTrimmed := strings.TrimSpace(aLine)
				if aTrimmed == "" || strings.HasPrefix(aTrimmed, "#") {
					i++
					continue
				}
				if countIndent(aLine) == 0 {
					break
				}
				aClean := strings.TrimSpace(aLine)
				if strings.HasPrefix(aClean, "-") {
					item := strings.TrimSpace(aClean[1:])
					if unq, uErr := strconv.Unquote(item); uErr == nil {
						spec.AvatarRows = append(spec.AvatarRows, unq)
					} else {
						spec.AvatarRows = append(spec.AvatarRows, unquoteString(item))
					}
				}
				i++
			}
		case "tiers":
			i++
			tiers, nextIdx, err := parseTiers(rawLines, i)
			if err != nil {
				return nil, err
			}
			spec.Tiers = tiers
			i = nextIdx
		case "slots":
			i++
			slots, nextIdx, err := parseSlots(rawLines, i)
			if err != nil {
				return nil, err
			}
			spec.Slots = slots
			i = nextIdx
		default:
			i++
		}
	}

	return spec, nil
}

func parseTiers(lines []string, startIdx int) ([]domain.TierSpec, int, error) {
	var tiers []domain.TierSpec
	var current *domain.TierSpec

	i := startIdx
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		if countIndent(line) == 0 {
			break
		}

		clean := stripComment(line)
		cleanTrimmed := strings.TrimSpace(clean)

		if strings.HasPrefix(cleanTrimmed, "-") {
			if current != nil {
				tiers = append(tiers, *current)
			}
			current = &domain.TierSpec{}
			afterDash := strings.TrimSpace(cleanTrimmed[1:])
			if afterDash != "" {
				colonIdx := strings.Index(afterDash, ":")
				if colonIdx != -1 {
					k := strings.TrimSpace(afterDash[:colonIdx])
					v := strings.TrimSpace(afterDash[colonIdx+1:])
					if err := assignTierField(current, k, v); err != nil {
						return nil, i, err
					}
				}
			}
			i++
			continue
		}

		if current == nil {
			return nil, i, fmt.Errorf("line %d: tier property before tier item indicator (-)", i+1)
		}

		colonIdx := strings.Index(clean, ":")
		if colonIdx != -1 {
			k := strings.TrimSpace(clean[:colonIdx])
			v := strings.TrimSpace(clean[colonIdx+1:])
			if k == "behavior" && strings.HasPrefix(v, "|") {
				i++
				var blockLines []string
				baseIndent := -1
				for i < len(lines) {
					bLine := lines[i]
					bTrim := strings.TrimSpace(bLine)
					if bTrim == "" {
						blockLines = append(blockLines, "")
						i++
						continue
					}
					indent := countIndent(bLine)
					if indent == 0 || strings.HasPrefix(bTrim, "-") {
						break
					}
					if baseIndent == -1 {
						baseIndent = indent
					}
					if indent < baseIndent {
						break
					}
					blockLines = append(blockLines, bLine[baseIndent:])
					i++
				}
				for len(blockLines) > 0 && blockLines[len(blockLines)-1] == "" {
					blockLines = blockLines[:len(blockLines)-1]
				}
				current.Behavior = strings.Join(blockLines, "\n")
				continue
			}
			if err := assignTierField(current, k, v); err != nil {
				return nil, i, err
			}
		}
		i++
	}

	if current != nil {
		tiers = append(tiers, *current)
	}

	return tiers, i, nil
}

func assignTierField(t *domain.TierSpec, key, val string) error {
	switch key {
	case "id":
		t.ID = unquoteString(val)
	case "name":
		t.Name = unquoteString(val)
	case "min":
		v, err := parseInt(val)
		if err != nil {
			return fmt.Errorf("invalid tier min %q: %w", val, err)
		}
		t.Min = v
	case "max":
		v, err := parseInt(val)
		if err != nil {
			return fmt.Errorf("invalid tier max %q: %w", val, err)
		}
		t.Max = v
	case "behavior":
		t.Behavior = unquoteString(val)
	}
	return nil
}

func parseSlots(lines []string, startIdx int) ([]domain.SlotSpec, int, error) {
	var slots []domain.SlotSpec
	var current *domain.SlotSpec

	i := startIdx
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		if countIndent(line) == 0 {
			break
		}

		clean := stripComment(line)
		cleanTrimmed := strings.TrimSpace(clean)

		if strings.HasPrefix(cleanTrimmed, "-") {
			if current != nil {
				slots = append(slots, *current)
			}
			current = &domain.SlotSpec{}
			afterDash := strings.TrimSpace(cleanTrimmed[1:])
			if afterDash != "" {
				colonIdx := strings.Index(afterDash, ":")
				if colonIdx != -1 {
					k := strings.TrimSpace(afterDash[:colonIdx])
					v := strings.TrimSpace(afterDash[colonIdx+1:])
					if err := assignSlotField(current, k, v); err != nil {
						return nil, i, err
					}
				}
			}
			i++
			continue
		}

		if current == nil {
			return nil, i, fmt.Errorf("line %d: slot property before slot item indicator (-)", i+1)
		}

		colonIdx := strings.Index(clean, ":")
		if colonIdx != -1 {
			k := strings.TrimSpace(clean[:colonIdx])
			v := strings.TrimSpace(clean[colonIdx+1:])
			if err := assignSlotField(current, k, v); err != nil {
				return nil, i, err
			}
		}
		i++
	}

	if current != nil {
		slots = append(slots, *current)
	}

	return slots, i, nil
}

func assignSlotField(s *domain.SlotSpec, key, val string) error {
	switch key {
	case "id":
		s.ID = unquoteString(val)
	case "title":
		s.Title = unquoteString(val)
	case "max_holders":
		v, err := parseInt(val)
		if err != nil {
			return fmt.Errorf("invalid slot max_holders %q: %w", val, err)
		}
		s.MaxHolders = v
	case "min_affinity":
		v, err := parseInt(val)
		if err != nil {
			return fmt.Errorf("invalid slot min_affinity %q: %w", val, err)
		}
		s.MinAffinity = &v
	case "max_affinity":
		v, err := parseInt(val)
		if err != nil {
			return fmt.Errorf("invalid slot max_affinity %q: %w", val, err)
		}
		s.MaxAffinity = &v
	case "allow_outrank_steal":
		b, err := parseBool(val)
		if err != nil {
			return fmt.Errorf("invalid slot allow_outrank_steal %q: %w", val, err)
		}
		s.AllowOutrankSteal = b
	}
	return nil
}

func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 2
		} else {
			break
		}
	}
	return count
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			if i > 0 && line[i-1] == '\\' {
				bs := 0
				for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
					bs++
				}
				if bs%2 == 1 {
					continue
				}
			}
			inDouble = !inDouble
		} else if ch == '#' && !inSingle && !inDouble {
			return line[:i]
		}
	}
	return line
}

func unquoteString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			if unquoted, err := strconv.Unquote(s); err == nil {
				return unquoted
			}
			return s[1 : len(s)-1]
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
		}
	}
	return s
}

func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return strconv.Atoi(s)
}

func parseBool(s string) (bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Trim(s, `"'`)
	switch s {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	default:
		return strconv.ParseBool(s)
	}
}

// Validate validates that a PersonaSpec satisfies all business and schema invariants.
func Validate(spec *domain.PersonaSpec) error {
	if spec == nil {
		return errors.New("persona spec is nil")
	}
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("persona id is required")
	}
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("persona name is required")
	}
	if strings.TrimSpace(spec.BotName) == "" {
		return errors.New("persona bot_name is required")
	}
	if strings.TrimSpace(spec.TriggerPrefix) == "" {
		return errors.New("persona trigger_prefix is required")
	}
	if strings.TrimSpace(spec.BasePersona) == "" {
		return errors.New("persona base_persona is required")
	}
	if spec.MaxPositiveDelta <= 0 {
		return fmt.Errorf("max_positive_delta must be > 0, got %d", spec.MaxPositiveDelta)
	}
	if spec.MaxNegativeDelta >= 0 {
		return fmt.Errorf("max_negative_delta must be < 0, got %d", spec.MaxNegativeDelta)
	}
	if len(spec.Tiers) == 0 {
		return errors.New("persona must define at least one tier")
	}

	// Validate Tiers continuously cover [-100, 100] without gaps or overlaps
	sortedTiers := make([]domain.TierSpec, len(spec.Tiers))
	copy(sortedTiers, spec.Tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		return sortedTiers[i].Min < sortedTiers[j].Min
	})

	tierIDs := make(map[string]bool)
	for i, t := range sortedTiers {
		if strings.TrimSpace(t.ID) == "" {
			return fmt.Errorf("tier at index %d has empty id", i)
		}
		if tierIDs[t.ID] {
			return fmt.Errorf("duplicate tier id: %s", t.ID)
		}
		tierIDs[t.ID] = true

		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("tier %q has empty name", t.ID)
		}
		if strings.TrimSpace(t.Behavior) == "" {
			return fmt.Errorf("tier %q has empty behavior", t.ID)
		}
		if t.Min > t.Max {
			return fmt.Errorf("tier %q min (%d) is greater than max (%d)", t.ID, t.Min, t.Max)
		}

		if i == 0 {
			if t.Min != -100 {
				return fmt.Errorf("first tier %q must start at -100, got %d", t.ID, t.Min)
			}
		} else {
			prevMax := sortedTiers[i-1].Max
			if t.Min <= prevMax {
				return fmt.Errorf("tier overlap between %q (ends at %d) and %q (starts at %d)", sortedTiers[i-1].ID, prevMax, t.ID, t.Min)
			}
			if t.Min > prevMax+1 {
				return fmt.Errorf("tier gap between %q (ends at %d) and %q (starts at %d)", sortedTiers[i-1].ID, prevMax, t.ID, t.Min)
			}
		}
	}

	if sortedTiers[len(sortedTiers)-1].Max != 100 {
		return fmt.Errorf("last tier %q must end at 100, got %d", sortedTiers[len(sortedTiers)-1].ID, sortedTiers[len(sortedTiers)-1].Max)
	}

	// Validate Slots
	slotIDs := make(map[string]bool)
	for i, s := range spec.Slots {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("slot at index %d has empty id", i)
		}
		if slotIDs[s.ID] {
			return fmt.Errorf("duplicate slot id: %s", s.ID)
		}
		slotIDs[s.ID] = true

		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("slot %q has empty title", s.ID)
		}
		if s.MaxHolders < 1 {
			return fmt.Errorf("slot %q max_holders must be >= 1, got %d", s.ID, s.MaxHolders)
		}
		if s.MinAffinity == nil && s.MaxAffinity == nil {
			return fmt.Errorf("slot %q must have at least one of min_affinity or max_affinity", s.ID)
		}
		if s.MinAffinity != nil && s.MaxAffinity != nil && *s.MinAffinity > *s.MaxAffinity {
			return fmt.Errorf("slot %q min_affinity (%d) cannot be greater than max_affinity (%d)", s.ID, *s.MinAffinity, *s.MaxAffinity)
		}
	}

	return nil
}

// Manager manages loaded persona templates and active persona switching thread-safely.
type Manager struct {
	mu        sync.RWMutex
	dir       string
	personas  map[string]*domain.PersonaSpec // keyed by persona ID
	filePaths map[string]string              // persona ID -> file path
	activeID  string
}

// NewManager loads all persona templates from dir (and/or initialFileOrID) and activates initialFileOrID.
func NewManager(dir string, initialFileOrID string) (*Manager, error) {
	m := &Manager{
		dir:       dir,
		personas:  make(map[string]*domain.PersonaSpec),
		filePaths: make(map[string]string),
	}

	// Scan directory if exists
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if ext != ".yaml" && ext != ".yml" {
					continue
				}
				fullPath := filepath.Join(dir, entry.Name())
				data, err := os.ReadFile(fullPath)
				if err != nil {
					return nil, fmt.Errorf("failed to read persona file %s: %w", fullPath, err)
				}
				spec, err := ParseYAML(data)
				if err != nil {
					return nil, fmt.Errorf("failed to parse persona file %s: %w", fullPath, err)
				}
				m.personas[spec.ID] = spec
				m.filePaths[spec.ID] = fullPath
			}
		}
	}

	// Also check if initialFileOrID is a direct path to a file
	if _, err := os.Stat(initialFileOrID); err == nil {
		data, err := os.ReadFile(initialFileOrID)
		if err == nil {
			spec, err := ParseYAML(data)
			if err == nil {
				m.personas[spec.ID] = spec
				m.filePaths[spec.ID] = initialFileOrID
			}
		}
	}

	// Determine active persona
	matchedID := m.findPersonaID(initialFileOrID)
	if matchedID == "" {
		return nil, fmt.Errorf("initial persona not found: %s", initialFileOrID)
	}

	m.activeID = matchedID
	return m, nil
}

func (m *Manager) findPersonaID(idOrFile string) string {
	if _, ok := m.personas[idOrFile]; ok {
		return idOrFile
	}

	targetBase := strings.TrimSuffix(filepath.Base(idOrFile), filepath.Ext(idOrFile))
	for id, p := range m.filePaths {
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if base == targetBase || base == idOrFile || p == idOrFile {
			return id
		}
	}

	return ""
}

// Current returns a copy of the currently active PersonaSpec.
func (m *Manager) Current() *domain.PersonaSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p := m.personas[m.activeID]
	if p == nil {
		return nil
	}
	return clonePersona(p)
}

// Switch hot-reloads and activates a persona matching idOrFile.
func (m *Manager) Switch(idOrFile string) (*domain.PersonaSpec, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try resolving file on disk to support hot reloading
	var targetPath string

	if path, ok := m.filePaths[idOrFile]; ok {
		targetPath = path
	} else if _, err := os.Stat(idOrFile); err == nil {
		targetPath = idOrFile
	} else if m.dir != "" {
		candidates := []string{
			filepath.Join(m.dir, idOrFile),
			filepath.Join(m.dir, idOrFile+".yaml"),
			filepath.Join(m.dir, idOrFile+".yml"),
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				targetPath = cand
				break
			}
		}
	}

	if targetPath == "" {
		matchedID := m.findPersonaID(idOrFile)
		if matchedID != "" {
			if path, ok := m.filePaths[matchedID]; ok {
				targetPath = path
			}
		}
	}

	if targetPath != "" {
		data, err := os.ReadFile(targetPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read persona file %s: %w", targetPath, err)
		}
		spec, err := ParseYAML(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse persona file %s: %w", targetPath, err)
		}
		m.personas[spec.ID] = spec
		m.filePaths[spec.ID] = targetPath
		m.activeID = spec.ID
		return clonePersona(spec), nil
	}

	// If no file on disk, check if already in memory
	if spec, ok := m.personas[idOrFile]; ok {
		m.activeID = spec.ID
		return clonePersona(spec), nil
	}

	return nil, fmt.Errorf("persona %q not found", idOrFile)
}

// List returns a sorted list of all loaded persona IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.personas))
	for id := range m.personas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func clonePersona(s *domain.PersonaSpec) *domain.PersonaSpec {
	if s == nil {
		return nil
	}
	c := *s
	if s.BlockedWords != nil {
		c.BlockedWords = make([]string, len(s.BlockedWords))
		copy(c.BlockedWords, s.BlockedWords)
	}
	if s.Tiers != nil {
		c.Tiers = make([]domain.TierSpec, len(s.Tiers))
		copy(c.Tiers, s.Tiers)
	}
	if s.Slots != nil {
		c.Slots = make([]domain.SlotSpec, len(s.Slots))
		for i, sl := range s.Slots {
			slotCopy := sl
			if sl.MinAffinity != nil {
				val := *sl.MinAffinity
				slotCopy.MinAffinity = &val
			}
			if sl.MaxAffinity != nil {
				val := *sl.MaxAffinity
				slotCopy.MaxAffinity = &val
			}
			c.Slots[i] = slotCopy
		}
	}
	return &c
}
