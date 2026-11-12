package safety

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reBracketTag   = regexp.MustCompile(`\[([A-Z0-9_]+)\]`)
	reBracketOpen  = regexp.MustCompile(`\[([A-Z_]{3,})`)
	reBracketClose = regexp.MustCompile(`([A-Z_]{3,})\]`)
	reURL          = regexp.MustCompile(`https?://[^\s]+|www\.[^\s]+`)
)

type Guard struct {
	maxInputRunes  int
	maxOutputRunes int
}

func NewGuard(maxInputRunes, maxOutputRunes int) *Guard {
	if maxInputRunes <= 0 {
		maxInputRunes = 400
	}
	if maxOutputRunes <= 0 {
		maxOutputRunes = 480
	}
	return &Guard{
		maxInputRunes:  maxInputRunes,
		maxOutputRunes: maxOutputRunes,
	}
}

func (g *Guard) SanitizeInput(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()

	s = strings.ReplaceAll(s, "```", "'''")
	s = reBracketTag.ReplaceAllString(s, "($1)")
	s = reBracketOpen.ReplaceAllString(s, "($1")
	s = reBracketClose.ReplaceAllString(s, "$1)")

	s = strings.Join(strings.Fields(s), " ")

	runes := []rune(s)
	if len(runes) > g.maxInputRunes {
		s = string(runes[:g.maxInputRunes])
	}
	return s
}

func (g *Guard) SanitizeReply(text, targetUsername string, blockedWords []string) string {
	cleaned := text
	target := strings.TrimSpace(strings.TrimPrefix(targetUsername, "@"))
	if target != "" {
		// Clean ", @target" or " @target" with optional following punctuation
		reWithLeadingComma := regexp.MustCompile("(?i)(,\\s*)@" + regexp.QuoteMeta(target) + "([:,\\.]?)")
		cleaned = reWithLeadingComma.ReplaceAllStringFunc(cleaned, func(match string) string {
			if strings.HasSuffix(match, ".") || strings.HasSuffix(match, "!") || strings.HasSuffix(match, "?") {
				return string(match[len(match)-1])
			}
			return ""
		})

		re := regexp.MustCompile("(?i)(^|\\s)@" + regexp.QuoteMeta(target) + "([:,\\.]?)(\\s|$)")
		cleaned = re.ReplaceAllStringFunc(cleaned, func(match string) string {
			trimmed := strings.TrimSpace(match)
			if strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?") {
				return string(trimmed[len(trimmed)-1]) + " "
			}
			return " "
		})
	}
	return g.SanitizeOutput(cleaned, blockedWords)
}

func (g *Guard) SanitizeOutput(text string, blockedWords []string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")

	for len(s) > 0 && (s[0] == '/' || s[0] == '.') {
		s = strings.TrimLeft(s, "/. \t\r\n")
	}

	s = reURL.ReplaceAllString(s, "[enlace]")

	for _, w := range blockedWords {
		word := strings.TrimSpace(w)
		if word == "" {
			continue
		}
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(word))
		s = re.ReplaceAllString(s, "***")
	}

	s = strings.Join(strings.Fields(s), " ")

	runes := []rune(s)
	if len(runes) <= g.maxOutputRunes {
		return s
	}

	sub := runes[:g.maxOutputRunes]
	minCut := g.maxOutputRunes / 2
	cutIdx := -1

	for i := g.maxOutputRunes - 1; i >= minCut; i-- {
		r := sub[i]
		if r == '!' || r == '?' || r == '.' || r == ',' {
			cutIdx = i + 1
			break
		}
		if unicode.IsSpace(r) {
			cutIdx = i
			break
		}
	}

	if cutIdx >= minCut {
		return strings.TrimSpace(string(sub[:cutIdx]))
	}

	return string(sub)
}
