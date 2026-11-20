package persona

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/twitch-social-bot/internal/domain"
)

// ErrNoCardMetadata indicates a PNG image contains no embedded ccv3 or chara metadata chunk.
var ErrNoCardMetadata = errors.New("png image has no embedded character card metadata (ccv3/chara)")

var (
	reAsteriskAction = regexp.MustCompile(`\*[^*\n]{2,120}\*`)
	reCharMacro      = regexp.MustCompile(`(?i)\{\{char\}\}|<char>|<bot>`)
	reUserMacro      = regexp.MustCompile(`(?i)\{\{user\}\}|<user>`)
)

// ImportFile parses a community character card (.png, .json, or .charx) into a clean
// PersonaSpec plus optional embedded image bytes (for .png and .charx avatars).
func ImportFile(path string) (*domain.PersonaSpec, []byte, error) {
	cleaned := strings.TrimSpace(path)
	cleaned = strings.Trim(cleaned, `"'`)
	if cleaned == "" {
		return nil, nil, errors.New("empty file path")
	}

	raw, err := os.ReadFile(cleaned)
	if err != nil {
		return nil, nil, fmt.Errorf("read card file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(cleaned))
	switch ext {
	case ".png":
		jsonBytes, pErr := extractPNGCardJSON(raw)
		if pErr != nil {
			return nil, nil, pErr
		}
		spec, cErr := parseCharacterJSON(jsonBytes)
		if cErr != nil {
			return nil, nil, cErr
		}
		return spec, raw, nil

	case ".charx":
		jsonBytes, imgBytes, cErr := extractCHARX(raw)
		if cErr != nil {
			return nil, nil, cErr
		}
		spec, pErr := parseCharacterJSON(jsonBytes)
		if pErr != nil {
			return nil, nil, pErr
		}
		return spec, imgBytes, nil

	case ".json":
		spec, pErr := parseCharacterJSON(raw)
		if pErr != nil {
			return nil, nil, pErr
		}
		return spec, nil, nil

	default:
		// Attempt PNG magic header detection or JSON fallback
		if len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n" {
			jsonBytes, pErr := extractPNGCardJSON(raw)
			if pErr != nil {
				return nil, nil, pErr
			}
			spec, cErr := parseCharacterJSON(jsonBytes)
			return spec, raw, cErr
		}
		spec, pErr := parseCharacterJSON(raw)
		return spec, nil, pErr
	}
}

func extractPNGCardJSON(raw []byte) ([]byte, error) {
	if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, errors.New("invalid PNG header")
	}

	chunks := make(map[string]string)
	pos := 8
	for pos+12 <= len(raw) {
		length := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
		chunkType := string(raw[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		if length < 0 || dataEnd+4 > len(raw) {
			break
		}
		data := raw[dataStart:dataEnd]
		pos = dataEnd + 4

		switch chunkType {
		case "tEXt":
			if k, v, ok := parsePNGTextChunk(data); ok {
				chunks[strings.ToLower(k)] = v
			}
		case "zTXt":
			if k, v, ok := parsePNGZtxtChunk(data); ok {
				chunks[strings.ToLower(k)] = v
			}
		case "iTXt":
			if k, v, ok := parsePNGItxtChunk(data); ok {
				chunks[strings.ToLower(k)] = v
			}
		case "IEND":
			pos = len(raw)
		}
	}

	// Spec priority: V3 ("ccv3") takes precedence over V2 ("chara").
	encoded := chunks["ccv3"]
	if encoded == "" {
		encoded = chunks["chara"]
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, ErrNoCardMetadata
	}

	return decodeCardPayload(encoded)
}

func parsePNGTextChunk(data []byte) (string, string, bool) {
	nul := bytes.IndexByte(data, 0)
	if nul <= 0 || nul+1 >= len(data) {
		return "", "", false
	}
	return string(data[:nul]), string(data[nul+1:]), true
}

func parsePNGZtxtChunk(data []byte) (string, string, bool) {
	nul := bytes.IndexByte(data, 0)
	if nul <= 0 || nul+2 >= len(data) {
		return "", "", false
	}
	key := string(data[:nul])
	zr, err := zlib.NewReader(bytes.NewReader(data[nul+2:]))
	if err != nil {
		return "", "", false
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", "", false
	}
	return key, string(out), true
}

func parsePNGItxtChunk(data []byte) (string, string, bool) {
	nul := bytes.IndexByte(data, 0)
	if nul <= 0 || nul+3 >= len(data) {
		return "", "", false
	}
	key := string(data[:nul])
	compFlag := data[nul+1]
	rest := data[nul+3:]
	// Skip language tag (nul-terminated) and translated keyword (nul-terminated)
	for skip := 0; skip < 2; skip++ {
		idx := bytes.IndexByte(rest, 0)
		if idx == -1 {
			return "", "", false
		}
		rest = rest[idx+1:]
	}
	if compFlag == 1 {
		zr, err := zlib.NewReader(bytes.NewReader(rest))
		if err != nil {
			return "", "", false
		}
		defer zr.Close()
		out, err := io.ReadAll(zr)
		if err != nil {
			return "", "", false
		}
		return key, string(out), true
	}
	return key, string(rest), true
}

func decodeCardPayload(encoded string) ([]byte, error) {
	trimmed := strings.TrimSpace(encoded)
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(trimmed); err == nil && bytes.Contains(b, []byte("{")) {
			return b, nil
		}
	}
	return nil, errors.New("failed to decode base64 character card payload")
}

func extractCHARX(raw []byte) ([]byte, []byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, fmt.Errorf("open .charx archive: %w", err)
	}

	var cardJSON, imgBytes []byte
	var imgPriority int // 2 = icon/portrait match, 1 = any image

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		nameLower := strings.ToLower(filepath.ToSlash(f.Name))
		if nameLower == "card.json" || nameLower == "character.json" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			cardJSON, _ = io.ReadAll(rc)
			rc.Close()
			continue
		}

		if strings.HasSuffix(nameLower, ".png") || strings.HasSuffix(nameLower, ".jpg") || strings.HasSuffix(nameLower, ".jpeg") {
			prio := 1
			if strings.Contains(nameLower, "icon") || strings.Contains(nameLower, "avatar") || strings.Contains(nameLower, "main") {
				prio = 2
			}
			if prio > imgPriority {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, rErr := io.ReadAll(rc)
				rc.Close()
				if rErr == nil && len(data) > 0 {
					imgBytes = data
					imgPriority = prio
				}
			}
		}
	}

	if len(cardJSON) == 0 {
		return nil, nil, errors.New(".charx archive missing card.json")
	}
	return cardJSON, imgBytes, nil
}

type rawCardData struct {
	Name                    string   `json:"name"`
	Nickname                string   `json:"nickname"`
	Description             string   `json:"description"`
	Personality             string   `json:"personality"`
	Scenario                string   `json:"scenario"`
	FirstMes                string   `json:"first_mes"`
	MesExample              string   `json:"mes_example"`
	SystemPrompt            string   `json:"system_prompt"`
	PostHistoryInstructions string   `json:"post_history_instructions"`
	GroupOnlyGreetings      []string `json:"group_only_greetings"`
	Tags                    []string `json:"tags"`
	CharacterBook           *struct {
		Entries []struct {
			Content string `json:"content"`
			Enabled bool   `json:"enabled"`
		} `json:"entries"`
	} `json:"character_book"`
}

type rawModularCharacter struct {
	Name            string            `json:"name"`
	Username        string            `json:"username"`
	System          string            `json:"system"`
	Bio             json.RawMessage   `json:"bio"` // string or []string
	Lore            []string          `json:"lore"`
	Adjectives      []string          `json:"adjectives"`
	Topics          []string          `json:"topics"`
	Style           json.RawMessage   `json:"style"` // []string or {all, chat, post}
	MessageExamples json.RawMessage   `json:"messageExamples"`
}

type rawCardEnvelope struct {
	Spec string       `json:"spec"`
	Data *rawCardData `json:"data"`
}

func parseCharacterJSON(raw []byte) (*domain.PersonaSpec, error) {
	var env rawCardEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Data != nil && strings.TrimSpace(env.Data.Name) != "" {
		return mapCCDataToPersona(env.Data), nil
	}

	// Check V1 flat card vs modular character JSON
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("invalid character JSON: %w", err)
	}

	if _, hasBio := rawMap["bio"]; hasBio {
		var mod rawModularCharacter
		if err := json.Unmarshal(raw, &mod); err == nil && strings.TrimSpace(mod.Name) != "" {
			return mapModularToPersona(&mod), nil
		}
	}

	var v1 rawCardData
	if err := json.Unmarshal(raw, &v1); err == nil && strings.TrimSpace(v1.Name) != "" {
		return mapCCDataToPersona(&v1), nil
	}

	return nil, errors.New("unrecognized character card JSON format (expected V1/V2/V3 card or modular JSON)")
}

func mapCCDataToPersona(d *rawCardData) *domain.PersonaSpec {
	botName := strings.TrimSpace(d.Nickname)
	if botName == "" {
		botName = strings.TrimSpace(d.Name)
	}
	displayName := strings.TrimSpace(d.Name)
	if displayName == "" {
		displayName = botName
	}

	var bio []string
	if sys := cleanCardText(d.SystemPrompt, botName); sys != "" {
		bio = append(bio, sys)
	}
	for _, para := range splitParagraphs(cleanCardText(d.Description, botName), 4) {
		bio = append(bio, para)
	}

	var adjectives []string
	for _, part := range strings.FieldsFunc(d.Personality, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		if t := strings.TrimSpace(cleanCardText(part, botName)); t != "" {
			adjectives = append(adjectives, t)
		}
	}

	var lore []string
	if scen := cleanCardText(d.Scenario, botName); scen != "" {
		lore = append(lore, scen)
	}
	if d.CharacterBook != nil {
		added := 0
		for _, entry := range d.CharacterBook.Entries {
			if !entry.Enabled {
				continue
			}
			if c := cleanCardText(entry.Content, botName); c != "" {
				lore = append(lore, c)
				added++
				if added >= 8 {
					break
				}
			}
		}
	}

	var style []string
	if phi := cleanCardText(d.PostHistoryInstructions, botName); phi != "" {
		style = append(style, phi)
	}
	style = append(style, "Reply in first person as rapid Twitch chat; NEVER write third-person action narration inside *asterisks*.")

	examples := parseMesExamples(d.MesExample, botName)
	if len(examples) == 0 {
		greeting := ""
		if len(d.GroupOnlyGreetings) > 0 {
			greeting = cleanCardText(d.GroupOnlyGreetings[0], botName)
		} else if strings.TrimSpace(d.FirstMes) != "" {
			greeting = cleanCardText(d.FirstMes, botName)
		}
		if greeting != "" {
			examples = append(examples, domain.MessageExample{
				User:  "hola",
				Reply: greeting,
			})
		}
	}

	if len(bio) == 0 {
		bio = []string{fmt.Sprintf("You are %s.", botName)}
	}

	return &domain.PersonaSpec{
		ID:               SanitizeID(displayName),
		Name:             displayName,
		BotName:          botName,
		TriggerPrefix:    "!bot",
		MaxPositiveDelta: 5,
		MaxNegativeDelta: -8,
		Bio:              bio,
		Lore:             lore,
		Adjectives:       adjectives,
		Topics:           cleanStringSlice(d.Tags),
		Style:            style,
		MessageExamples:  examples,
	}
}

func mapModularToPersona(m *rawModularCharacter) *domain.PersonaSpec {
	botName := strings.TrimSpace(m.Name)
	var bio []string
	if sys := cleanCardText(m.System, botName); sys != "" {
		bio = append(bio, sys)
	}
	if len(m.Bio) > 0 {
		var bioArr []string
		if err := json.Unmarshal(m.Bio, &bioArr); err == nil {
			for _, b := range bioArr {
				if c := cleanCardText(b, botName); c != "" {
					bio = append(bio, c)
				}
			}
		} else {
			var bioStr string
			if err := json.Unmarshal(m.Bio, &bioStr); err == nil {
				bio = append(bio, splitParagraphs(cleanCardText(bioStr, botName), 5)...)
			}
		}
	}
	if len(bio) == 0 {
		bio = []string{fmt.Sprintf("You are %s.", botName)}
	}

	var style []string
	if len(m.Style) > 0 {
		var styleArr []string
		if err := json.Unmarshal(m.Style, &styleArr); err == nil {
			style = append(style, cleanStringSlice(styleArr)...)
		} else {
			var styleObj struct {
				All  []string `json:"all"`
				Chat []string `json:"chat"`
			}
			if err := json.Unmarshal(m.Style, &styleObj); err == nil {
				style = append(style, cleanStringSlice(styleObj.All)...)
				style = append(style, cleanStringSlice(styleObj.Chat)...)
			}
		}
	}

	var examples []domain.MessageExample
	if len(m.MessageExamples) > 0 {
		var threads [][]struct {
			Name    string `json:"name"`
			User    string `json:"user"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(m.MessageExamples, &threads); err == nil {
			for _, thread := range threads {
				if len(thread) >= 2 {
					uTxt := cleanCardText(thread[0].Content.Text, botName)
					rTxt := cleanCardText(thread[len(thread)-1].Content.Text, botName)
					if uTxt != "" && rTxt != "" {
						examples = append(examples, domain.MessageExample{User: uTxt, Reply: rTxt})
					}
				}
				if len(examples) >= 5 {
					break
				}
			}
		}
	}

	return &domain.PersonaSpec{
		ID:               SanitizeID(botName),
		Name:             botName,
		BotName:          botName,
		TriggerPrefix:    "!bot",
		MaxPositiveDelta: 5,
		MaxNegativeDelta: -8,
		Bio:              bio,
		Lore:             cleanStringSlice(m.Lore),
		Adjectives:       cleanStringSlice(m.Adjectives),
		Topics:           cleanStringSlice(m.Topics),
		Style:            style,
		MessageExamples:  examples,
	}
}

func parseMesExamples(raw, botName string) []domain.MessageExample {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var examples []domain.MessageExample
	var curUser string

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.EqualFold(trimmed, "<START>") {
			curUser = ""
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "{{user}}:") || strings.HasPrefix(lower, "<user>:"):
			idx := strings.Index(trimmed, ":")
			curUser = cleanCardText(trimmed[idx+1:], botName)
		case strings.HasPrefix(lower, "{{char}}:") || strings.HasPrefix(lower, "<char>:") || strings.HasPrefix(lower, strings.ToLower(botName)+":"):
			idx := strings.Index(trimmed, ":")
			reply := cleanCardText(trimmed[idx+1:], botName)
			if curUser != "" && reply != "" {
				examples = append(examples, domain.MessageExample{User: curUser, Reply: reply})
				curUser = ""
				if len(examples) >= 5 {
					return examples
				}
			}
		}
	}
	return examples
}

func cleanCardText(s, botName string) string {
	out := reCharMacro.ReplaceAllString(s, botName)
	out = reUserMacro.ReplaceAllString(out, "viewer")
	out = reAsteriskAction.ReplaceAllString(out, "")
	out = strings.Join(strings.Fields(out), " ")
	return strings.TrimSpace(out)
}

func splitParagraphs(s string, maxItems int) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	if len(runes) <= 220 {
		return []string{s}
	}
	var out []string
	start := 0
	for start < len(runes) && len(out) < maxItems {
		end := start + 220
		if end >= len(runes) {
			out = append(out, strings.TrimSpace(string(runes[start:])))
			break
		}
		cut := end
		for i := end; i > start+80; i-- {
			if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
				cut = i + 1
				break
			}
		}
		out = append(out, strings.TrimSpace(string(runes[start:cut])))
		start = cut
	}
	return out
}

func cleanStringSlice(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// SanitizeID normalizes an arbitrary persona name into a safe lowercase filename/ID slug.
func SanitizeID(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	var sb strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '_' || r == '-' || r == ' ':
			sb.WriteByte('_')
		}
	}
	res := strings.Trim(sb.String(), "_")
	if res == "" {
		return "custom_persona"
	}
	return res
}
