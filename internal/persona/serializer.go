package persona

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/twitch-social-bot/internal/domain"
)

// WriteYAML serializes a PersonaSpec to disk in clean modular YAML compatible with ParseYAML.
func WriteYAML(path string, spec *domain.PersonaSpec) error {
	if spec == nil {
		return fmt.Errorf("cannot write nil persona spec")
	}

	prefix := spec.TriggerPrefix
	if prefix == "" {
		prefix = "!bot"
	}
	posDelta := spec.MaxPositiveDelta
	if posDelta <= 0 {
		posDelta = 5
	}
	negDelta := spec.MaxNegativeDelta
	if negDelta >= 0 {
		negDelta = -8
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("id: %s\n", strconv.Quote(spec.ID)))
	sb.WriteString(fmt.Sprintf("name: %s\n", strconv.Quote(spec.Name)))
	sb.WriteString(fmt.Sprintf("bot_name: %s\n", strconv.Quote(spec.BotName)))
	if spec.ThemeColor != "" {
		sb.WriteString(fmt.Sprintf("theme_color: %s\n", strconv.Quote(spec.ThemeColor)))
	}
	sb.WriteString(fmt.Sprintf("trigger_prefix: %s\n", strconv.Quote(prefix)))
	sb.WriteString(fmt.Sprintf("max_positive_delta: %d\n", posDelta))
	sb.WriteString(fmt.Sprintf("max_negative_delta: %d\n\n", negDelta))

	if strings.TrimSpace(spec.BasePersona) != "" {
		sb.WriteString("base_persona: |\n")
		for _, line := range strings.Split(spec.BasePersona, "\n") {
			sb.WriteString("  " + strings.TrimSpace(line) + "\n")
		}
		sb.WriteString("\n")
	}

	writeListSection(&sb, "bio", spec.Bio)
	writeListSection(&sb, "lore", spec.Lore)
	writeListSection(&sb, "adjectives", spec.Adjectives)
	writeListSection(&sb, "topics", spec.Topics)
	writeListSection(&sb, "style", spec.Style)

	if len(spec.MessageExamples) > 0 {
		sb.WriteString("message_examples:\n")
		for _, ex := range spec.MessageExamples {
			sb.WriteString("  - user: " + strconv.Quote(ex.User) + "\n")
			sb.WriteString("    reply: " + strconv.Quote(ex.Reply) + "\n")
		}
		sb.WriteString("\n")
	}

	if len(spec.BlockedWords) == 0 {
		sb.WriteString("blocked_words: []\n")
	} else {
		writeListSection(&sb, "blocked_words", spec.BlockedWords)
	}

	if len(spec.AvatarRows) > 0 {
		sb.WriteString("\navatar_rows:\n")
		for _, r := range spec.AvatarRows {
			sb.WriteString("  - " + strconv.Quote(r) + "\n")
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func writeListSection(sb *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	sb.WriteString(key + ":\n")
	for _, it := range items {
		trimmed := strings.TrimSpace(it)
		if trimmed != "" {
			sb.WriteString("  - " + strconv.Quote(trimmed) + "\n")
		}
	}
	sb.WriteString("\n")
}
