package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/social"
)

// Ensure Client implements domain.LLMClient.
var _ domain.LLMClient = (*Client)(nil)

type ClientConfig struct {
	Provider         string
	BaseURL          string
	APIKey           string
	Model            string
	Timeout          time.Duration
	MaxTokens        int
	Temperature      float64
	FrequencyPenalty float64
	PresencePenalty  float64
	FailureThreshold int
	BreakerCooldown  time.Duration
	HTTPClient       *http.Client
	AllowNSFW        bool
	Locale           *i18n.Manager
}

type Client struct {
	cfg        ClientConfig
	clock      domain.Clock
	breaker    *CircuitBreaker
	httpClient *http.Client
}

func NewClient(cfg ClientConfig, clock domain.Clock) *Client {
	provider := DetectProvider(cfg.Provider, cfg.BaseURL)
	if cfg.Provider == "" {
		cfg.Provider = provider.ID
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = provider.DefaultBaseURL
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.deepseek.com"
		}
	}
	if cfg.Model == "" {
		cfg.Model = provider.FallbackModel
		if cfg.Model == "" {
			cfg.Model = "deepseek-chat"
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 450
	}
	if cfg.Temperature <= 0 {
		cfg.Temperature = 0.85
	}
	if cfg.FrequencyPenalty <= 0 {
		cfg.FrequencyPenalty = 0.45
	}
	if cfg.PresencePenalty <= 0 {
		cfg.PresencePenalty = 0.35
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.BreakerCooldown <= 0 {
		cfg.BreakerCooldown = 20 * time.Second
	}
	if clock == nil {
		clock = domain.NewRealClock()
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	breaker := NewCircuitBreaker(clock, cfg.FailureThreshold, cfg.BreakerCooldown)

	return &Client{
		cfg:        cfg,
		clock:      clock,
		breaker:    breaker,
		httpClient: httpClient,
	}
}

func (c *Client) Breaker() *CircuitBreaker {
	return c.breaker
}

// BuildSystemPrompt returns a deterministic, static prompt for context caching.
func BuildSystemPrompt(spec *domain.PersonaSpec) string {
	return BuildSystemPromptWithLocale(spec, nil)
}

// BuildSystemPromptWithLocale returns a deterministic static prompt localized via loc (SFW by default).
func BuildSystemPromptWithLocale(spec *domain.PersonaSpec, loc *i18n.Manager) string {
	return BuildSystemPromptWithOptions(spec, loc, false)
}

// BuildSystemPromptWithOptions builds the static system prompt with configurable NSFW mode.
func BuildSystemPromptWithOptions(spec *domain.PersonaSpec, loc *i18n.Manager, allowNSFW bool) string {
	if spec == nil {
		return ""
	}
	if loc == nil {
		loc, _ = i18n.NewManager("", "es")
	}

	var b strings.Builder
	if soul := strings.TrimSpace(loc.Soul()); soul != "" {
		b.WriteString(soul + "\n\n")
	}
	b.WriteString(loc.Get("llm_role_header") + "\n")
	b.WriteString(fmt.Sprintf("%s: %s\n", loc.Get("llm_bot_name_label"), spec.BotName))
	if strings.TrimSpace(spec.BasePersona) != "" {
		b.WriteString(fmt.Sprintf("%s: %s\n", loc.Get("llm_base_persona_label"), spec.BasePersona))
	}
	if len(spec.Bio) > 0 {
		b.WriteString(loc.Get("llm_bio_label") + ":\n")
		for _, line := range spec.Bio {
			b.WriteString("- " + line + "\n")
		}
	}
	if len(spec.Adjectives) > 0 {
		b.WriteString(fmt.Sprintf("%s: %s\n", loc.Get("llm_adjectives_label"), strings.Join(spec.Adjectives, ", ")))
	}
	if len(spec.Topics) > 0 {
		b.WriteString(fmt.Sprintf("%s: %s\n", loc.Get("llm_topics_label"), strings.Join(spec.Topics, ", ")))
	}
	b.WriteString(loc.Get("llm_language_directive") + "\n\n")

	if len(spec.Lore) > 0 {
		b.WriteString(loc.Get("llm_lore_header") + "\n")
		for _, item := range spec.Lore {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}

	if len(spec.Style) > 0 {
		b.WriteString(loc.Get("llm_style_header") + "\n")
		for _, item := range spec.Style {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}

	if len(spec.MessageExamples) > 0 {
		b.WriteString(loc.Get("llm_examples_header") + "\n")
		for _, ex := range spec.MessageExamples {
			b.WriteString(fmt.Sprintf("Viewer: %s\n%s: %s\n\n", ex.User, spec.BotName, ex.Reply))
		}
	}

	if len(spec.Tiers) > 0 {
		b.WriteString(loc.Get("llm_tiers_header") + "\n")
		sortedTiers := make([]domain.TierSpec, len(spec.Tiers))
		copy(sortedTiers, spec.Tiers)
		sort.SliceStable(sortedTiers, func(i, j int) bool {
			return sortedTiers[i].Min < sortedTiers[j].Min
		})
		for _, t := range sortedTiers {
			b.WriteString(fmt.Sprintf("- [%d, %d] %s (%s): %s\n", t.Min, t.Max, t.Name, t.ID, t.Behavior))
		}
		b.WriteString("\n")
	}

	if len(spec.Slots) > 0 {
		b.WriteString(loc.Get("llm_slots_header") + "\n")
		sortedSlots := make([]domain.SlotSpec, len(spec.Slots))
		copy(sortedSlots, spec.Slots)
		sort.SliceStable(sortedSlots, func(i, j int) bool {
			return sortedSlots[i].ID < sortedSlots[j].ID
		})
		for _, s := range sortedSlots {
			b.WriteString(fmt.Sprintf("- %s: %s (max_holders: %d)\n", s.ID, s.Title, s.MaxHolders))
		}
		b.WriteString("\n")
	}

	b.WriteString(loc.Get("llm_rules_header") + "\n")
	b.WriteString(loc.Get("llm_rule_1") + "\n")
	b.WriteString(loc.Get("llm_rule_2") + "\n")
	b.WriteString(loc.Get("llm_rule_3") + "\n")
	b.WriteString(loc.Get("llm_rule_opinion") + "\n")
	b.WriteString(loc.Get("llm_rule_no_ai") + "\n")
	if !allowNSFW {
		b.WriteString(loc.Get("llm_rule_sfw") + "\n")
	}
	b.WriteString(loc.Get("llm_rule_4") + "\n")
	b.WriteString(`{"reply":"...","affinity_delta":0,"new_title":"","new_fact":"","slot_claim":""}` + "\n")

	return b.String()
}

// BuildUserPrompt formats the dynamic sections of the prompt.
func BuildUserPrompt(input domain.PromptInput) string {
	return BuildUserPromptWithLocale(input, nil)
}

// BuildUserPromptWithLocale formats the dynamic prompt sections localized via loc.
func BuildUserPromptWithLocale(input domain.PromptInput, loc *i18n.Manager) string {
	if loc == nil {
		loc, _ = i18n.NewManager("", "es")
	}
	var b strings.Builder

	channelName := strings.TrimSpace(input.BroadcasterName)
	if channelName == "" && input.ViewerRole == "broadcaster" {
		channelName = strings.TrimSpace(input.Profile.DisplayName)
		if channelName == "" {
			channelName = strings.TrimSpace(input.Profile.Username)
		}
	}

	if channelName != "" || input.StreamGame != "" || input.StreamTitle != "" {
		b.WriteString(loc.Get("llm_stream_context_header") + "\n")
		if channelName != "" {
			b.WriteString(fmt.Sprintf("- Streamer: %s\n", channelName))
		}
		if input.StreamGame != "" {
			b.WriteString(fmt.Sprintf("- Game/Category: %s\n", input.StreamGame))
		}
		if input.StreamTitle != "" {
			b.WriteString(fmt.Sprintf("- Title: %s\n", input.StreamTitle))
		}
		b.WriteString("\n")
	}

	b.WriteString(loc.Get("llm_user_channel_state") + "\n")
	if input.CurrentMood != "" {
		b.WriteString(fmt.Sprintf("- Current Mood: %s\n", loc.Get("llm_mood_"+input.CurrentMood)))
	}
	var slotLines []string
	if len(input.ActiveSlots) > 0 {
		var slotIDs []string
		for slotID := range input.ActiveSlots {
			slotIDs = append(slotIDs, slotID)
		}
		sort.Strings(slotIDs)
		for _, slotID := range slotIDs {
			holders := input.ActiveSlots[slotID]
			for _, h := range holders {
				slotLines = append(slotLines, fmt.Sprintf("%s: @%s (%s: %d)", slotID, h.DisplayName, loc.Get("llm_user_affinity_label"), h.Affinity))
			}
		}
	}
	if len(slotLines) == 0 {
		b.WriteString(loc.Get("llm_user_no_active_slots") + "\n\n")
	} else {
		for _, line := range slotLines {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(loc.Get("llm_user_room_context") + "\n")
	if len(input.RecentRoom) == 0 {
		b.WriteString(loc.Get("llm_user_no_room_msgs") + "\n\n")
	} else {
		for _, msg := range input.RecentRoom {
			b.WriteString(fmt.Sprintf("@%s: %s\n", msg.DisplayName, msg.Text))
		}
		b.WriteString("\n")
	}

	b.WriteString(loc.Get("llm_user_viewer_profile") + "\n")
	b.WriteString(fmt.Sprintf("- Viewer: %s (@%s)\n", input.Profile.DisplayName, input.Profile.Username))
	if input.ViewerRole != "" {
		if input.ViewerRole == "broadcaster" && channelName != "" {
			b.WriteString(fmt.Sprintf("- Role: %s\n", loc.Format("llm_role_broadcaster_named", channelName, channelName)))
		} else {
			b.WriteString(fmt.Sprintf("- Role: %s\n", loc.Get("llm_role_"+input.ViewerRole)))
		}
	}
	if input.ViewerRole == "broadcaster" && input.Profile.Affinity >= 0 {
		b.WriteString(loc.Get("llm_broadcaster_familiarity") + "\n")
	}
	b.WriteString(fmt.Sprintf("- Affinity: %d\n", input.Profile.Affinity))
	b.WriteString(fmt.Sprintf("- Tier: %s (%s)\n", input.CurrentTier.Name, input.CurrentTier.ID))
	b.WriteString(fmt.Sprintf("- Behavior: %s\n", input.CurrentTier.Behavior))
	if input.Profile.EarnedTitle != "" {
		b.WriteString(fmt.Sprintf("- Earned Title: %s\n", input.Profile.EarnedTitle))
	} else {
		b.WriteString("- Earned Title: none\n")
	}
	if len(input.Profile.Facts) > 0 {
		relevantFacts := social.SelectRelevantFacts(input.Profile.Facts, input.ViewerMessage, 5, 3)
		b.WriteString(fmt.Sprintf("- Facts: %s\n", strings.Join(relevantFacts, "; ")))
	} else {
		b.WriteString(fmt.Sprintf("- Facts: %s\n", loc.Get("llm_user_no_facts")))
	}
	if len(input.Profile.RecentTurns) > 0 {
		b.WriteString(loc.Get("llm_user_recent_turns") + "\n")
		for _, turn := range input.Profile.RecentTurns {
			b.WriteString(fmt.Sprintf("  * Viewer: %s\n    Bot: %s\n", turn.UserMessage, turn.BotReply))
		}
	} else {
		b.WriteString(loc.Get("llm_user_recent_turns") + " " + loc.Get("llm_user_no_turns") + "\n")
	}
	b.WriteString("\n")

	pacingKey, _ := pickTurnPacingKey(input.ViewerMessage)
	b.WriteString(loc.Get(pacingKey) + "\n")
	b.WriteString(loc.Get("llm_anti_repeat") + "\n")
	if repeated := DetectRepeatedTerms(input.Profile.RecentTurns); len(repeated) > 0 {
		quoted := make([]string, len(repeated))
		for i, term := range repeated {
			quoted[i] = fmt.Sprintf("%q", term)
		}
		b.WriteString(loc.Format("llm_dynamic_antirepeat", strings.Join(quoted, ", ")) + "\n")
	}
	if len(input.RecentRoom) > 0 {
		b.WriteString(loc.Get("llm_cross_memory") + "\n")
	}
	if pacingKey == "llm_pacing_medium" && rand.IntN(100) < 10 {
		b.WriteString(loc.Get("llm_follow_up_hint") + "\n")
	}
	b.WriteString("\n")

	b.WriteString(loc.Get("llm_user_untrusted_msg") + "\n")
	b.WriteString(input.ViewerMessage)
	b.WriteString("\n")

	return b.String()
}

func isMinimalMessage(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return true
	}
	return len([]rune(trimmed)) <= 12 || len(strings.Fields(trimmed)) <= 2
}

func pickTurnPacingKey(viewerMsg string) (string, bool) {
	if isMinimalMessage(viewerMsg) {
		return "llm_pacing_minimal", true
	}
	n := rand.IntN(100)
	switch {
	case n < 50:
		return "llm_pacing_short", false
	case n < 92:
		return "llm_pacing_medium", false
	default:
		return "llm_pacing_long", false
	}
}

// ParseLLMDecision parses the raw LLM response with fallbacks.
func ParseLLMDecision(raw string) (domain.LLMDecision, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return domain.LLMDecision{}, errors.New("empty LLM response")
	}

	cleaned := trimmed
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1]
			}
			cleaned = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}

	firstBrace := strings.Index(cleaned, "{")
	lastBrace := strings.LastIndex(cleaned, "}")
	if firstBrace != -1 && lastBrace != -1 && firstBrace < lastBrace {
		jsonCandidate := cleaned[firstBrace : lastBrace+1]
		var dec domain.LLMDecision
		if err := json.Unmarshal([]byte(jsonCandidate), &dec); err == nil {
			if strings.TrimSpace(dec.Reply) != "" {
				return dec, nil
			}
			// If JSON unmarshaling succeeded but Reply is empty, fallback to trimmed raw
			return domain.LLMDecision{
				Reply:         trimmed,
				AffinityDelta: 0,
			}, nil
		}
	}

	// Truncated or malformed JSON recovery: if the output starts like JSON with a "reply" field
	if strings.Contains(cleaned, `"reply"`) {
		replyKey := `"reply"`
		idx := strings.Index(cleaned, replyKey)
		if idx != -1 {
			rem := cleaned[idx+len(replyKey):]
			colonIdx := strings.Index(rem, ":")
			if colonIdx != -1 {
				valPart := strings.TrimSpace(rem[colonIdx+1:])
				if strings.HasPrefix(valPart, `"`) {
					valPart = valPart[1:]
					var sb strings.Builder
					escaped := false
					foundClosing := false
					for _, r := range valPart {
						if escaped {
							switch r {
							case 'n':
								sb.WriteRune('\n')
							case 'r':
								sb.WriteRune('\r')
							case 't':
								sb.WriteRune('\t')
							case '"':
								sb.WriteRune('"')
							case '\\':
								sb.WriteRune('\\')
							default:
								sb.WriteRune(r)
							}
							escaped = false
						} else if r == '\\' {
							escaped = true
						} else if r == '"' {
							foundClosing = true
							break
						} else {
							sb.WriteRune(r)
						}
					}
					extractedReply := strings.TrimSpace(sb.String())
					if foundClosing && extractedReply != "" {
						return domain.LLMDecision{
							Reply:         extractedReply,
							AffinityDelta: 0,
						}, nil
					}
				}
			}
		}
	}

	// Fallback to plain text
	if cleaned == "" {
		cleaned = trimmed
	}
	if cleaned == "" {
		return domain.LLMDecision{}, errors.New("empty LLM response")
	}

	return domain.LLMDecision{
		Reply:         cleaned,
		AffinityDelta: 0,
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model            string         `json:"model"`
	Messages         []chatMessage  `json:"messages"`
	MaxTokens        int            `json:"max_tokens,omitempty"`
	Temperature      float64        `json:"temperature,omitempty"`
	FrequencyPenalty float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64        `json:"presence_penalty,omitempty"`
	ResponseFormat   responseFormat `json:"response_format"`
}

type chatChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

type anthropicRequest struct {
	Model       string        `json:"model"`
	System      string        `json:"system,omitempty"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature,omitempty"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func formatEndpoint(baseURL, protocol string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if protocol == "anthropic" {
		return baseURL + "/messages"
	}
	return baseURL + "/chat/completions"
}

func (c *Client) Generate(ctx context.Context, input domain.PromptInput) (domain.LLMDecision, error) {
	if !c.breaker.Allow() {
		return domain.LLMDecision{}, ErrCircuitOpen
	}

	systemPrompt := BuildSystemPromptWithOptions(input.Persona, c.cfg.Locale, c.cfg.AllowNSFW)
	userPrompt := BuildUserPromptWithLocale(input, c.cfg.Locale)
	provider := DetectProvider(c.cfg.Provider, c.cfg.BaseURL)

	var bodyBytes []byte
	var err error
	if provider.Protocol == "anthropic" {
		reqPayload := anthropicRequest{
			Model:       c.cfg.Model,
			System:      systemPrompt,
			Messages:    []chatMessage{{Role: "user", Content: userPrompt}},
			MaxTokens:   c.cfg.MaxTokens,
			Temperature: c.cfg.Temperature,
		}
		bodyBytes, err = json.Marshal(reqPayload)
	} else {
		reqPayload := chatRequest{
			Model: c.cfg.Model,
			Messages: []chatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
			MaxTokens:      c.cfg.MaxTokens,
			Temperature:    c.cfg.Temperature,
			ResponseFormat: responseFormat{Type: "json_object"},
		}
		if provider.ID != "gemini" {
			reqPayload.FrequencyPenalty = c.cfg.FrequencyPenalty
			reqPayload.PresencePenalty = c.cfg.PresencePenalty
		}
		bodyBytes, err = json.Marshal(reqPayload)
	}
	if err != nil {
		return domain.LLMDecision{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := formatEndpoint(c.cfg.BaseURL, provider.Protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return domain.LLMDecision{}, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if provider.Protocol == "anthropic" {
		if c.cfg.APIKey != "" {
			req.Header.Set("x-api-key", c.cfg.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.breaker.RecordFailure()
		return domain.LLMDecision{}, fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.breaker.RecordFailure()
		return domain.LLMDecision{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		c.breaker.RecordFailure()
		return domain.LLMDecision{}, fmt.Errorf("llm api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return domain.LLMDecision{}, fmt.Errorf("llm api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var rawContent string
	if provider.Protocol == "anthropic" {
		var antResp anthropicResponse
		if err := json.Unmarshal(respBody, &antResp); err != nil {
			return domain.LLMDecision{}, fmt.Errorf("failed to unmarshal anthropic response: %w", err)
		}
		for _, block := range antResp.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				rawContent = block.Text
				break
			}
		}
		if rawContent == "" {
			return domain.LLMDecision{}, errors.New("anthropic returned no text content")
		}
	} else {
		var chatResp chatResponse
		if err := json.Unmarshal(respBody, &chatResp); err != nil {
			return domain.LLMDecision{}, fmt.Errorf("failed to unmarshal llm response: %w", err)
		}
		if len(chatResp.Choices) == 0 {
			return domain.LLMDecision{}, errors.New("llm returned no choices")
		}
		rawContent = chatResp.Choices[0].Message.Content
	}

	decision, err := ParseLLMDecision(rawContent)
	if err != nil {
		return domain.LLMDecision{}, fmt.Errorf("failed to parse decision: %w", err)
	}

	c.breaker.RecordSuccess()
	return decision, nil
}
