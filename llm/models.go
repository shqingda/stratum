package llm

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed models.json
var modelsJSON []byte

// ModelCompat holds per-model wire compatibility flags from the catalog.
type ModelCompat struct {
	MaxTokensField                              string         `json:"maxTokensField"`
	ThinkingFormat                              string         `json:"thinkingFormat"`
	SupportsReasoningEffort                     *bool          `json:"supportsReasoningEffort"`
	RequiresReasoningContentOnAssistantMessages bool           `json:"requiresReasoningContentOnAssistantMessages"`
	ZaiToolStream                               bool           `json:"zaiToolStream"`
	ChatTemplateArgs                            map[string]any `json:"chatTemplateArgs"`
	SupportsCacheControlOnTools                 *bool          `json:"supportsCacheControlOnTools"`
	AllowEmptySignature                         bool           `json:"allowEmptySignature"`
	ForceAdaptiveThinking                       *bool          `json:"forceAdaptiveThinking"`
	SendSessionAffinityHeaders                  bool           `json:"sendSessionAffinityHeaders"`
	SessionAffinityFormat                       string         `json:"sessionAffinityFormat"`
}

// Model describes a model in the catalog. Prices are US dollars per million
// tokens.
type Model struct {
	ID               string
	Provider         string // provider id: "openai", "anthropic", ...
	Name             string
	API              string // wire protocol: "anthropic", "responses", "chat", "gemini"
	BaseURL          string
	Headers          map[string]string
	Compat           *ModelCompat
	ThinkingLevelMap map[string]*string
	Context          int
	MaxOutput        int
	Reasoning        bool
	Images           bool
	Price            Price
	Released         string // YYYY-MM-DD, where known
}

// Price is what a model costs per million tokens. A subscription pays by
// plan, so its prices only estimate.
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Cost prices usage. Input counts cached tokens too.
func (m Model) Cost(u Usage) float64 {
	fresh := u.Input - u.CacheRead - u.CacheWrite
	return (float64(fresh)*m.Price.Input + float64(u.Output)*m.Price.Output +
		float64(u.CacheRead)*m.Price.CacheRead + float64(u.CacheWrite)*m.Price.CacheWrite) / 1e6
}

var effortLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// ResolveEffort clamps effort to the levels levelMap permits and returns the
// wire value for it. An empty effort or map passes through unchanged.
func ResolveEffort(effort string, levelMap map[string]*string) string {
	if effort == "" || len(levelMap) == 0 {
		return effort
	}
	supported := func(lv string) bool {
		v, ok := levelMap[lv]
		if ok && v == nil {
			return false
		}
		if (lv == "xhigh" || lv == "max") && !ok {
			return false
		}
		return true
	}
	chosen := ""
	if supported(effort) {
		chosen = effort
	} else if idx := slices.Index(effortLevels, effort); idx >= 0 {
		for i := idx + 1; i < len(effortLevels); i++ {
			if supported(effortLevels[i]) {
				chosen = effortLevels[i]
				break
			}
		}
		if chosen == "" {
			for i := idx - 1; i >= 0; i-- {
				if supported(effortLevels[i]) {
					chosen = effortLevels[i]
					break
				}
			}
		}
	} else {
		for _, lv := range effortLevels {
			if supported(lv) {
				chosen = lv
				break
			}
		}
	}
	if chosen == "" {
		return ""
	}
	if v, ok := levelMap[chosen]; ok && v != nil {
		return *v
	}
	return chosen
}

func canonicalProvider(provider string) string {
	if provider == "moonshot" {
		return "moonshotai"
	}
	return provider
}

func mapCatalogAPI(api string) string {
	switch api {
	case "anthropic-messages":
		return "anthropic"
	case "google-generative-ai":
		return "gemini"
	case "openai-completions":
		return "chat"
	case "openai-responses":
		return "responses"
	default:
		return api
	}
}

// builtin is the embedded catalog before models.dev answers, plus providers
// models.dev does not list (such as the ChatGPT backend's).
var builtin []Model

// defaults are the models a provider's sessions start with.
var defaults = map[string]string{
	"codex":                         "gpt-6.1-sol",
	"copilot":                       "gpt-6.1-sol",
	"openai":                        "gpt-6.1-sol",
	"anthropic":                     "claude-sonnet-5-5",
	"google":                        "gemini-4-argon",
	"xai":                           "grok-4.7",
	"openrouter":                    "anthropic/claude-sonnet-5.5",
	"opencode":                      "claude-sonnet-5-5",
	"opencode-go":                   "deepseek-v4-pro",
	"vercel-ai-gateway":             "anthropic/claude-sonnet-5.5",
	"fireworks":                     "accounts/fireworks/models/deepseek-v4p1-flash",
	"groq":                          "openai/gpt-oss-120b",
	"cerebras":                      "qwen-3.8-27b",
	"together":                      "deepseek-ai/DeepSeek-V4-Pro-0813",
	"baseten":                       "deepseek-ai/DeepSeek-V4-Pro-0813",
	"huggingface":                   "deepseek-ai/DeepSeek-V4-Pro",
	"nvidia":                        "deepseek-ai/deepseek-v4.1-flash",
	"deepseek":                      "deepseek-v4-pro",
	"kimi-coding":                   "kimi-for-coding",
	"kimi-coding-global":            "kimi-for-coding",
	"moonshotai":                    "kimi-k3.1-pro",
	"moonshotai-cn":                 "kimi-k3.1-pro",
	"zai":                           "glm-5.3",
	"zai-api":                       "glm-5.3",
	"zai-coding-cn":                 "glm-5.3",
	"zhipuai":                       "glm-5.3",
	"qwen":                          "qwen3.8-max",
	"qwen-token-plan":               "qwen3.8-max",
	"qwen-token-plan-cn":            "qwen3.8-max",
	"qwen-token-plan-individual":    "qwen3.8-max",
	"qwen-token-plan-individual-cn": "qwen3.8-max",
	"minimax":                       "MiniMax-M3",
	"minimax-cn":                    "MiniMax-M3",
	"xiaomi":                        "mimo-v2.6-pro",
	"xiaomi-token-plan-cn":          "mimo-v2.6-pro",
	"xiaomi-token-plan-sgp":         "mimo-v2.6-pro",
	"xiaomi-token-plan-ams":         "mimo-v2.6-pro",
	"ant-ling":                      "Ring-2.6-1T",
}

var (
	catalogMu sync.RWMutex
	catalog   []Model
)

func init() {
	base := []Model{
		{ID: "gpt-6.1-sol", Provider: "codex", Name: "GPT-6.1 Sol", API: "responses", Context: 500000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
		{ID: "gpt-6-astra", Provider: "codex", Name: "GPT-6 Astra", API: "responses", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{10, 50, 1, 12.5}},
		{ID: "gpt-6-sol", Provider: "codex", Name: "GPT-6 Sol", API: "responses", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.2, 2.5}},
		{ID: "gpt-6-luna", Provider: "codex", Name: "GPT-6 Luna", API: "responses", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},

		{ID: "gpt-6.1-sol", Provider: "copilot", Name: "GPT-6.1 Sol", API: "chat", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
		{ID: "claude-sonnet-5-5", Provider: "copilot", Name: "Claude Sonnet 5.5", API: "chat", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
		{ID: "gemini-4-argon", Provider: "copilot", Name: "Gemini 4 Argon", API: "chat", Context: 1048576, MaxOutput: 32768, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 0}},

		{ID: "qwen3.8-max", Provider: "qwen", Name: "Qwen3.8 Max", API: "chat", Context: 1000000, MaxOutput: 65536, Reasoning: true, Images: true, Price: Price{1.2, 6, 0, 0}},
		{ID: "qwen3.8-flash", Provider: "qwen", Name: "Qwen3.8 Flash", API: "chat", Context: 1000000, MaxOutput: 65536, Reasoning: true, Images: true, Price: Price{0.15, 1.5, 0, 0}},
		{ID: "qwen3.7-plus", Provider: "qwen", Name: "Qwen3.7 Plus", API: "chat", Context: 1000000, MaxOutput: 65536, Reasoning: true, Images: true, Price: Price{0.4, 1.2, 0, 0}},
	}

	var raw []struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		API       string   `json:"api"`
		BaseURL   string   `json:"baseUrl"`
		Provider  string   `json:"provider"`
		Reasoning bool     `json:"reasoning"`
		Input     []string `json:"input"`
		Cost      struct {
			Input      float64 `json:"input"`
			Output     float64 `json:"output"`
			CacheRead  float64 `json:"cacheRead"`
			CacheWrite float64 `json:"cacheWrite"`
		} `json:"cost"`
		ContextWindow    int                `json:"contextWindow"`
		MaxTokens        int                `json:"maxTokens"`
		Headers          map[string]string  `json:"headers"`
		Compat           *ModelCompat       `json:"compat"`
		ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
	}
	if err := json.Unmarshal(modelsJSON, &raw); err != nil {
		panic(fmt.Sprintf("llm: embedded models.json: %v", err))
	}
	for _, r := range raw {
		base = append(base, Model{
			ID:               r.ID,
			Provider:         r.Provider,
			Name:             r.Name,
			API:              mapCatalogAPI(r.API),
			BaseURL:          r.BaseURL,
			Headers:          r.Headers,
			Compat:           r.Compat,
			ThinkingLevelMap: r.ThinkingLevelMap,
			Context:          r.ContextWindow,
			MaxOutput:        r.MaxTokens,
			Reasoning:        r.Reasoning,
			Images:           slices.Contains(r.Input, "image"),
			Price:            Price{r.Cost.Input, r.Cost.Output, r.Cost.CacheRead, r.Cost.CacheWrite},
		})
	}
	builtin = base
	catalog = builtin
}

// Models lists the catalog's models of a provider, or all of them for "".
func Models(provider string) []Model {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	filter := func(p string) []Model {
		var out []Model
		for _, m := range catalog {
			if p == "" || m.Provider == p {
				out = append(out, m)
			}
		}
		return out
	}
	out := filter(provider)
	if len(out) == 0 && provider != "" {
		if canon := canonicalProvider(provider); canon != provider {
			out = filter(canon)
		}
	}
	return out
}

// FindModel looks a model up by provider and id.
func FindModel(provider, id string) (Model, bool) {
	for _, m := range Models(provider) {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// DefaultModel is the model a provider's sessions start with; "" when there
// is none to suggest and the user picks.
func DefaultModel(provider string) string {
	if id := defaults[canonicalProvider(provider)]; id != "" {
		return id
	}
	return ""
}

// ModelsDevURL is the catalog of models Stratum refreshes from.
const ModelsDevURL = "https://models.dev/api.json"

// LoadCatalog fills the catalog from models.dev, through a copy cached at
// path that is refetched once older than maxAge. names maps models.dev's
// provider ids to Stratum's. When the fetch fails the cached copy serves, and
// without one the built-in list stays.
func LoadCatalog(ctx context.Context, path string, maxAge time.Duration, names map[string]string) error {
	data, err := os.ReadFile(path)
	if fi, statErr := os.Stat(path); err != nil || statErr != nil || time.Since(fi.ModTime()) > maxAge {
		fresh, fetchErr := fetchCatalog(ctx)
		if fetchErr == nil {
			data, err = fresh, nil
			os.MkdirAll(filepath.Dir(path), 0o700)
			if tmp := path + ".tmp"; os.WriteFile(tmp, fresh, 0o600) == nil {
				os.Rename(tmp, path)
			}
		} else if err != nil {
			return fetchErr
		}
	}
	models, err := parseModelsDev(data, names)
	if err != nil {
		return err
	}
	builtinByKey := make(map[[2]string]Model, len(builtin))
	for _, m := range builtin {
		builtinByKey[[2]string{m.Provider, m.ID}] = m
	}
	for i := range models {
		if bm, ok := builtinByKey[[2]string{models[i].Provider, models[i].ID}]; ok {
			models[i].API = bm.API
			models[i].BaseURL = bm.BaseURL
			models[i].Headers = bm.Headers
			models[i].Compat = bm.Compat
			models[i].ThinkingLevelMap = bm.ThinkingLevelMap
		}
	}
	// Providers not listed on models.dev keep their built-in entries.
	listed := map[string]bool{}
	for _, m := range models {
		listed[m.Provider] = true
	}
	for _, m := range builtin {
		if !listed[m.Provider] {
			models = append(models, m)
		}
	}
	catalogMu.Lock()
	catalog = models
	catalogMu.Unlock()
	return nil
}

func fetchCatalog(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelsDevURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// parseModelsDev keeps the chat models that can call tools, of the
// providers in names, newest first.
func parseModelsDev(data []byte, names map[string]string) ([]Model, error) {
	var doc map[string]struct {
		Models map[string]struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			Reasoning  bool   `json:"reasoning"`
			ToolCall   bool   `json:"tool_call"`
			Released   string `json:"release_date"`
			Modalities struct {
				Input  []string `json:"input"`
				Output []string `json:"output"`
			} `json:"modalities"`
			Limit struct {
				Context int `json:"context"`
				Output  int `json:"output"`
			} `json:"limit"`
			Cost struct {
				Input      float64 `json:"input"`
				Output     float64 `json:"output"`
				CacheRead  float64 `json:"cache_read"`
				CacheWrite float64 `json:"cache_write"`
			} `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("models.dev: %w", err)
	}
	var out []Model
	for dev, ours := range names {
		for _, m := range doc[dev].Models {
			if !m.ToolCall || (m.Type != "" && m.Type != "chat") || !slices.Contains(m.Modalities.Output, "text") {
				continue
			}
			out = append(out, Model{
				ID: m.ID, Provider: ours, Name: m.Name,
				Context: m.Limit.Context, MaxOutput: m.Limit.Output,
				Reasoning: m.Reasoning, Images: slices.Contains(m.Modalities.Input, "image"),
				Price:    Price{m.Cost.Input, m.Cost.Output, m.Cost.CacheRead, m.Cost.CacheWrite},
				Released: m.Released,
			})
		}
	}
	slices.SortFunc(out, func(a, b Model) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		if c := strings.Compare(b.Released, a.Released); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}
