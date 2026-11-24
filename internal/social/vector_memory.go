package social

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

var memoryStopwords = map[string]bool{
	"que": true, "con": true, "por": true, "para": true, "una": true, "uno": true,
	"los": true, "las": true, "del": true, "pero": true, "como": true, "esta": true,
	"este": true, "eso": true, "hoy": true, "ayer": true, "muy": true, "mas": true,
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"you": true, "are": true, "was": true, "have": true, "has": true, "not": true,
}

// CosineSimilarity computes a normalized hybrid word + character n-gram cosine similarity in [0, 1].
func CosineSimilarity(a, b string) float64 {
	va := embedText(a)
	vb := embedText(b)
	return dotProduct(va, vb)
}

// SelectRelevantFacts returns up to topK semantically relevant facts for query
// plus the recentK most recent facts, preserving chronological order without duplicates.
func SelectRelevantFacts(facts []string, query string, topK, recentK int) []string {
	n := len(facts)
	if n == 0 {
		return nil
	}
	if topK < 0 {
		topK = 0
	}
	if recentK < 0 {
		recentK = 0
	}
	if n <= topK+recentK {
		out := make([]string, n)
		copy(out, facts)
		return out
	}

	recentStart := n - recentK
	if recentStart < 0 {
		recentStart = 0
	}

	selected := make([]bool, n)
	for i := recentStart; i < n; i++ {
		selected[i] = true
	}

	if topK > 0 && recentStart > 0 && strings.TrimSpace(query) != "" {
		qVec := embedText(query)
		if len(qVec) > 0 {
			type scored struct {
				idx   int
				score float64
			}
			candidates := make([]scored, 0, recentStart)
			for i := 0; i < recentStart; i++ {
				sim := dotProduct(qVec, embedText(facts[i]))
				if sim > 0.01 {
					candidates = append(candidates, scored{idx: i, score: sim})
				}
			}
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].score != candidates[j].score {
					return candidates[i].score > candidates[j].score
				}
				return candidates[i].idx > candidates[j].idx
			})
			for i := 0; i < len(candidates) && i < topK; i++ {
				selected[candidates[i].idx] = true
			}
		}
	}

	out := make([]string, 0, topK+recentK)
	for i, keep := range selected {
		if keep {
			out = append(out, facts[i])
		}
	}
	return out
}

// SelectContextualFacts selects relevant + recent facts via SelectRelevantFacts and
// cools down any fact that the bot ALREADY referenced in lastBotReply when the viewer's
// current query is unrelated to that fact (preventing back-to-back shoehorning).
func SelectContextualFacts(facts []string, query, lastBotReply string, topK, recentK int) []string {
	base := SelectRelevantFacts(facts, query, topK, recentK)
	if len(base) == 0 || strings.TrimSpace(lastBotReply) == "" {
		return base
	}

	replyKeywords := significantKeywords(lastBotReply)
	out := make([]string, 0, len(base))
	for _, f := range base {
		if strings.TrimSpace(query) != "" && CosineSimilarity(query, f) >= 0.12 {
			out = append(out, f)
			continue
		}
		if CosineSimilarity(lastBotReply, f) >= 0.18 || sharesSignificantKeyword(f, replyKeywords) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func significantKeywords(text string) map[string]struct{} {
	words := strings.Fields(normalizeText(text))
	res := make(map[string]struct{}, len(words))
	for _, w := range words {
		if len([]rune(w)) >= 4 && !memoryStopwords[w] {
			res[w] = struct{}{}
		}
	}
	return res
}

func sharesSignificantKeyword(fact string, kw map[string]struct{}) bool {
	if len(kw) == 0 {
		return false
	}
	for _, w := range strings.Fields(normalizeText(fact)) {
		if len([]rune(w)) >= 4 && !memoryStopwords[w] {
			if _, ok := kw[w]; ok {
				return true
			}
		}
	}
	return false
}

func embedText(text string) map[string]float64 {
	words := strings.Fields(normalizeText(text))
	if len(words) == 0 {
		return nil
	}

	vec := make(map[string]float64, len(words)*6)
	for _, w := range words {
		runes := []rune(w)
		if len(runes) < 2 {
			continue
		}
		if len(runes) >= 3 && !memoryStopwords[w] {
			vec["w:"+w] += 2.0
		}
		if len(runes) >= 4 && !memoryStopwords[w] {
			vec["s:"+string(runes[:4])] += 1.5
		}
		padded := append([]rune{'_'}, runes...)
		padded = append(padded, '_')
		for i := 0; i+3 <= len(padded); i++ {
			vec["t:"+string(padded[i:i+3])] += 1.0
		}
	}

	var normSq float64
	for _, val := range vec {
		normSq += val * val
	}
	if normSq == 0 {
		return nil
	}
	invNorm := 1.0 / math.Sqrt(normSq)
	for k, val := range vec {
		vec[k] = val * invNorm
	}
	return vec
}

func dotProduct(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	var sum float64
	for k, va := range a {
		if vb, ok := b[k]; ok {
			sum += va * vb
		}
	}
	return sum
}

func normalizeText(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		case 'ñ':
			r = 'n'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			sb.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(sb.String())
}
