package llm

import (
	"context"
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

// Model describes a model in the catalog. Prices are US dollars per million
// tokens.
type Model struct {
	ID        string
	Provider  string // provider id: "openai", "anthropic", ...
	Name      string
	Context   int
	MaxOutput int
	Reasoning bool
	Images    bool
	Price     Price
	Released  string // YYYY-MM-DD, where known
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

// builtin is the catalog before models.dev answers, and the models it does
// not list (the ChatGPT backend's). Taken from pi-ai's and models.dev's
// catalogs of November 2026.
var builtin = []Model{
	{ID: "gpt-6.1-sol", Provider: "codex", Name: "GPT-6.1 Sol", Context: 500000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
	{ID: "gpt-6-astra", Provider: "codex", Name: "GPT-6 Astra", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{10, 50, 1, 12.5}},
	{ID: "gpt-6-sol", Provider: "codex", Name: "GPT-6 Sol", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.2, 2.5}},
	{ID: "gpt-6-luna", Provider: "codex", Name: "GPT-6 Luna", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},

	{ID: "gpt-6.1-sol", Provider: "openai", Name: "GPT-6.1 Sol", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
	{ID: "gpt-6-luna", Provider: "openai", Name: "GPT-6 Luna", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},
	{ID: "claude-opus-5-5", Provider: "anthropic", Name: "Claude Opus 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{4, 20, 0.2, 5}},
	{ID: "claude-sonnet-5-5", Provider: "anthropic", Name: "Claude Sonnet 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}},
	{ID: "claude-haiku-5-5", Provider: "anthropic", Name: "Claude Haiku 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},
	{ID: "gemini-4-argon", Provider: "google", Name: "Gemini 4 Argon", Context: 1048576, MaxOutput: 32768, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 0}},
	{ID: "gemini-3.8-flash", Provider: "google", Name: "Gemini 3.8 Flash", Context: 1048576, MaxOutput: 65536, Reasoning: true, Images: true, Price: Price{0.75, 3.75, 0.075, 0}},
	{ID: "grok-4.7", Provider: "xai", Name: "Grok 4.7", Context: 500000, MaxOutput: 500000, Reasoning: true, Images: true, Price: Price{2, 6, 0.5, 0}},
	{ID: "grok-4.3", Provider: "xai", Name: "Grok 4.3", Context: 1000000, MaxOutput: 30000, Reasoning: true, Images: true, Price: Price{1.25, 2.5, 0.2, 0}},
	{ID: "deepseek-v4-pro", Provider: "deepseek", Name: "DeepSeek V4 Pro", Context: 1000000, MaxOutput: 384000, Reasoning: true, Price: Price{1.32, 3.96, 0.044, 0}},
	{ID: "deepseek-flash", Provider: "deepseek", Name: "DeepSeek V4.1 Flash", Context: 1000000, MaxOutput: 384000, Reasoning: true, Images: true, Price: Price{0.3, 1.2, 0.006, 0}},
}

// defaults are the models a provider's sessions start with.
var defaults = map[string]string{
	"codex":     "gpt-6.1-sol",
	"openai":    "gpt-6.1-sol",
	"anthropic": "claude-sonnet-5-5",
	"google":    "gemini-4-argon",
	"xai":       "grok-4.7",
	"deepseek":  "deepseek-v4-pro",
}

var (
	catalogMu sync.RWMutex
	catalog   = builtin
)

// Models lists the catalog's models of a provider, or all of them for "".
func Models(provider string) []Model {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	var out []Model
	for _, m := range catalog {
		if provider == "" || m.Provider == provider {
			out = append(out, m)
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
	if id := defaults[provider]; id != "" {
		return id
	}
	return ""
}

// ModelsDevURL is the catalog of models Talos refreshes from.
const ModelsDevURL = "https://models.dev/api.json"

// LoadCatalog fills the catalog from models.dev, through a copy cached at
// path that is refetched once older than maxAge. names maps models.dev's
// provider ids to Talos's. When the fetch fails the cached copy serves, and
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
	// The ChatGPT backend's models are not on models.dev.
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
	for dev, talos := range names {
		for _, m := range doc[dev].Models {
			if !m.ToolCall || (m.Type != "" && m.Type != "chat") || !slices.Contains(m.Modalities.Output, "text") {
				continue
			}
			out = append(out, Model{
				ID: m.ID, Provider: talos, Name: m.Name,
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
