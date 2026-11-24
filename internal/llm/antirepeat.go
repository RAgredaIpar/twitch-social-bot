package llm

import (
	"strings"
	"unicode"

	"github.com/twitch-social-bot/internal/domain"
)

var stopWordsESEN = map[string]struct{}{
	"a": {}, "e": {}, "i": {}, "o": {}, "u": {}, "y": {},
	"que": {}, "con": {}, "los": {}, "las": {}, "del": {}, "por": {}, "para": {}, "una": {}, "uno": {},
	"pero": {}, "como": {}, "más": {}, "mas": {}, "eso": {}, "esa": {}, "ese": {}, "esto": {}, "esta": {},
	"este": {}, "son": {}, "eres": {}, "soy": {}, "está": {}, "hay": {}, "muy": {}, "sin": {},
	"tus": {}, "mis": {}, "sus": {}, "nos": {}, "les": {}, "algo": {}, "nada": {}, "todo": {}, "toda": {},
	"ya": {}, "si": {}, "sí": {}, "no": {}, "ni": {}, "me": {}, "te": {}, "se": {}, "lo": {}, "la": {}, "el": {},
	"the": {}, "and": {}, "you": {}, "for": {}, "that": {}, "this": {}, "with": {}, "not": {}, "are": {},
	"was": {}, "but": {}, "your": {}, "have": {}, "just": {}, "from": {}, "they": {}, "what": {}, "all": {},
}

// DetectRepeatedTerms inspects recent bot replies (up to last 5) and returns
// overused edge fillers (appearing at start/end of >= 2 replies, including
// 1-char trailing chat tags like "p" or "f") and repeated content words
// (appearing across >= 3 replies), capped at 4 terms.
func DetectRepeatedTerms(turns []domain.ConversationTurn) []string {
	if len(turns) < 2 {
		return nil
	}
	if len(turns) > 5 {
		turns = turns[len(turns)-5:]
	}

	edgeCounts := make(map[string]int)
	bodyCounts := make(map[string]int)
	var order []string
	seenOrder := make(map[string]struct{})
	recordCandidate := func(tok string) {
		if _, ok := seenOrder[tok]; !ok {
			seenOrder[tok] = struct{}{}
			order = append(order, tok)
		}
	}

	for _, turn := range turns {
		tokens := tokenizeNormalizedWords(turn.BotReply)
		if len(tokens) == 0 {
			continue
		}

		first := tokens[0]
		last := tokens[len(tokens)-1]
		if len([]rune(first)) >= 2 {
			if _, stop := stopWordsESEN[first]; !stop {
				edgeCounts[first]++
				recordCandidate(first)
			}
		}
		if len(tokens) > 1 && last != first && len([]rune(last)) >= 1 {
			if _, stop := stopWordsESEN[last]; !stop {
				edgeCounts[last]++
				recordCandidate(last)
			}
		}

		seenInTurn := make(map[string]struct{}, len(tokens))
		for _, tok := range tokens {
			if len([]rune(tok)) < 3 {
				continue
			}
			if _, stop := stopWordsESEN[tok]; stop {
				continue
			}
			seenInTurn[tok] = struct{}{}
		}
		for tok := range seenInTurn {
			bodyCounts[tok]++
			recordCandidate(tok)
		}
	}

	var flagged []string
	flaggedSet := make(map[string]struct{})
	for _, tok := range order {
		if edgeCounts[tok] >= 2 || bodyCounts[tok] >= 3 {
			if _, exists := flaggedSet[tok]; !exists {
				flaggedSet[tok] = struct{}{}
				flagged = append(flagged, tok)
				if len(flagged) == 4 {
					break
				}
			}
		}
	}
	return flagged
}

func tokenizeNormalizedWords(text string) []string {
	lower := strings.ToLower(text)
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		norm := collapseTrailingRepeats(cur)
		out = append(out, string(norm))
		cur = cur[:0]
	}
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// collapseTrailingRepeats turns "xddd" -> "xd", "brooo" -> "bro", "jajaja" -> "jaja".
func collapseTrailingRepeats(runes []rune) []rune {
	for len(runes) >= 3 && runes[len(runes)-1] == runes[len(runes)-2] {
		runes = runes[:len(runes)-1]
	}
	s := string(runes)
	if len(s) >= 6 && strings.HasPrefix(s, "jaja") && strings.Trim(s, "ja") == "" {
		return []rune("jaja")
	}
	if len(s) >= 6 && strings.HasPrefix(s, "haha") && strings.Trim(s, "ha") == "" {
		return []rune("haha")
	}
	return runes
}
