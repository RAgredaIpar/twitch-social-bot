package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ProviderSpec defines a supported AI provider, its brand color, endpoint, and protocol.
type ProviderSpec struct {
	ID             string
	Name           string
	ColorANSI      string
	DefaultBaseURL string
	FallbackModel  string
	KeyGuideURL    string
	Protocol       string // "openai" or "anthropic"
}

var providerCatalog = []ProviderSpec{
	{
		ID:             "deepseek",
		Name:           "DeepSeek",
		ColorANSI:      "\x1b[38;2;77;107;254m", // DeepSeek Blue (#4D6BFE)
		DefaultBaseURL: "https://api.deepseek.com",
		FallbackModel:  "deepseek-chat",
		KeyGuideURL:    "https://platform.deepseek.com/api_keys",
		Protocol:       "openai",
	},
	{
		ID:             "claude",
		Name:           "Claude (Anthropic)",
		ColorANSI:      "\x1b[38;2;217;119;87m", // Claude Terracotta Orange (#D97757)
		DefaultBaseURL: "https://api.anthropic.com/v1",
		FallbackModel:  "claude-3-5-haiku-latest",
		KeyGuideURL:    "https://console.anthropic.com/settings/keys",
		Protocol:       "anthropic",
	},
	{
		ID:             "openai",
		Name:           "ChatGPT (OpenAI)",
		ColorANSI:      "\x1b[38;2;16;163;127m", // OpenAI Emerald (#10A37F)
		DefaultBaseURL: "https://api.openai.com/v1",
		FallbackModel:  "gpt-4o-mini",
		KeyGuideURL:    "https://platform.openai.com/api-keys",
		Protocol:       "openai",
	},
	{
		ID:             "gemini",
		Name:           "Google Gemini",
		ColorANSI:      "\x1b[38;2;78;149;255m", // Gemini Blue (#4E95FF)
		DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		FallbackModel:  "gemini-2.5-flash",
		KeyGuideURL:    "https://aistudio.google.com/apikey",
		Protocol:       "openai",
	},
	{
		ID:             "groq",
		Name:           "Groq (LPU Fast)",
		ColorANSI:      "\x1b[38;2;245;80;54m", // Groq Orange-Red (#F55036)
		DefaultBaseURL: "https://api.groq.com/openai/v1",
		FallbackModel:  "llama-3.3-70b-versatile",
		KeyGuideURL:    "https://console.groq.com/keys",
		Protocol:       "openai",
	},
	{
		ID:             "xai",
		Name:           "Grok (xAI)",
		ColorANSI:      "\x1b[38;2;240;240;245m", // Grok Silver-White (#F0F0F5)
		DefaultBaseURL: "https://api.x.ai/v1",
		FallbackModel:  "grok-3-mini",
		KeyGuideURL:    "https://console.x.ai/",
		Protocol:       "openai",
	},
	{
		ID:             "custom",
		Name:           "Custom / Private API",
		ColorANSI:      "\x1b[38;2;192;132;252m", // Neon Purple (#C084FC)
		DefaultBaseURL: "",
		FallbackModel:  "default",
		KeyGuideURL:    "",
		Protocol:       "openai",
	},
}

// Providers returns a copy of all supported AI providers in display order.
func Providers() []ProviderSpec {
	out := make([]ProviderSpec, len(providerCatalog))
	copy(out, providerCatalog)
	return out
}

// GetProvider returns the ProviderSpec for id, defaulting to DeepSeek if unknown.
func GetProvider(id string) ProviderSpec {
	norm := strings.ToLower(strings.TrimSpace(id))
	for _, p := range providerCatalog {
		if p.ID == norm {
			return p
		}
	}
	return providerCatalog[0]
}

// DetectProvider resolves a ProviderSpec from an explicit provider ID or by inspecting baseURL.
func DetectProvider(providerID, baseURL string) ProviderSpec {
	norm := strings.ToLower(strings.TrimSpace(providerID))
	for _, p := range providerCatalog {
		if p.ID == norm {
			return p
		}
	}
	lowerURL := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(lowerURL, "anthropic.com"):
		return GetProvider("claude")
	case strings.Contains(lowerURL, "openai.com"):
		return GetProvider("openai")
	case strings.Contains(lowerURL, "googleapis.com"):
		return GetProvider("gemini")
	case strings.Contains(lowerURL, "groq.com"):
		return GetProvider("groq")
	case strings.Contains(lowerURL, "x.ai"):
		return GetProvider("xai")
	case lowerURL == "" || strings.Contains(lowerURL, "deepseek.com"):
		return GetProvider("deepseek")
	default:
		return GetProvider("custom")
	}
}

// DiscoverBestModel queries the provider's live GET /models endpoint to pick the newest
// suitable chat model automatically, falling back to the provider's FallbackModel if unreachable.
func DiscoverBestModel(ctx context.Context, httpClient *http.Client, providerID, baseURL, apiKey string) string {
	spec := DetectProvider(providerID, baseURL)
	if baseURL == "" {
		baseURL = spec.DefaultBaseURL
	}
	if baseURL == "" || strings.TrimSpace(apiKey) == "" {
		return spec.FallbackModel
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 4 * time.Second}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return spec.FallbackModel
	}

	if spec.Protocol == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return spec.FallbackModel
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return spec.FallbackModel
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Data) == 0 {
		return spec.FallbackModel
	}

	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		id := strings.TrimPrefix(strings.TrimSpace(m.ID), "models/")
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return spec.FallbackModel
	}

	return selectPreferredModel(spec, ids)
}

func selectPreferredModel(spec ProviderSpec, ids []string) string {
	switch spec.ID {
	case "deepseek":
		for _, id := range ids {
			if id == "deepseek-chat" {
				return id
			}
		}
	case "claude":
		for _, kw := range []string{"haiku", "sonnet", "claude"} {
			for _, id := range ids {
				if strings.Contains(strings.ToLower(id), kw) {
					return id
				}
			}
		}
	case "openai":
		for _, kw := range []string{"gpt-4.1-mini", "gpt-4o-mini", "gpt-4.1", "gpt-4o"} {
			for _, id := range ids {
				lower := strings.ToLower(id)
				if strings.Contains(lower, kw) && !strings.Contains(lower, "audio") && !strings.Contains(lower, "realtime") && !strings.Contains(lower, "transcribe") && !strings.Contains(lower, "tts") {
					return id
				}
			}
		}
	case "gemini":
		for _, kw := range []string{"gemini-2.5-flash", "gemini-2.0-flash", "flash", "gemini-2.5-pro"} {
			for _, id := range ids {
				lower := strings.ToLower(id)
				if strings.Contains(lower, kw) && !strings.Contains(lower, "tts") && !strings.Contains(lower, "image") && !strings.Contains(lower, "embedding") {
					return id
				}
			}
		}
	case "groq":
		for _, kw := range []string{"llama-3.3-70b", "llama-3.1-8b", "llama"} {
			for _, id := range ids {
				if strings.Contains(strings.ToLower(id), kw) && !strings.Contains(strings.ToLower(id), "guard") {
					return id
				}
			}
		}
	case "xai":
		for _, kw := range []string{"grok-3-mini", "grok-3", "grok"} {
			for _, id := range ids {
				if strings.Contains(strings.ToLower(id), kw) && !strings.Contains(strings.ToLower(id), "vision") && !strings.Contains(strings.ToLower(id), "image") {
					return id
				}
			}
		}
	}
	return ids[0]
}
