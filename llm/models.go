package llm

// Model describes a model in the catalog. Prices are US dollars per million
// tokens.
type Model struct {
	ID        string
	Provider  string // auth provider id: "chatgpt", "anthropic", ...
	Name      string
	Context   int
	MaxOutput int
	Reasoning bool
	Images    bool
	Price     Price
	// Default marks the provider's model a new session starts with.
	Default bool
}

// Price is what a model costs per million tokens. A ChatGPT sign-in pays
// by plan, so its prices only estimate.
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Cost prices one reply's usage. Input counts cached tokens too.
func (m Model) Cost(u Usage) float64 {
	fresh := u.Input - u.CacheRead - u.CacheWrite
	return (float64(fresh)*m.Price.Input + float64(u.Output)*m.Price.Output +
		float64(u.CacheRead)*m.Price.CacheRead + float64(u.CacheWrite)*m.Price.CacheWrite) / 1e6
}

// Catalog is the built-in list of models, taken from pi-ai's catalog of
// November 2026. G4 refreshes it from models.dev.
var Catalog = []Model{
	{ID: "gpt-6.1-sol", Provider: "chatgpt", Name: "GPT-6.1 Sol", Context: 500000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}, Default: true},
	{ID: "gpt-6-astra", Provider: "chatgpt", Name: "GPT-6 Astra", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{10, 50, 1, 12.5}},
	{ID: "gpt-6-luna", Provider: "chatgpt", Name: "GPT-6 Luna", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},

	{ID: "claude-opus-5-5", Provider: "anthropic", Name: "Claude Opus 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{4, 20, 0.2, 5}},
	{ID: "claude-sonnet-5-5", Provider: "anthropic", Name: "Claude Sonnet 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}, Default: true},
	{ID: "claude-haiku-5-5", Provider: "anthropic", Name: "Claude Haiku 5.5", Context: 1000000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},

	{ID: "gpt-6.1-sol", Provider: "openai", Name: "GPT-6.1 Sol", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 2.5}, Default: true},
	{ID: "gpt-6-luna", Provider: "openai", Name: "GPT-6 Luna", Context: 272000, MaxOutput: 128000, Reasoning: true, Images: true, Price: Price{0.1, 0.5, 0.01, 0.125}},

	{ID: "gemini-4-argon", Provider: "google", Name: "Gemini 4 Argon", Context: 1048576, MaxOutput: 32768, Reasoning: true, Images: true, Price: Price{2, 10, 0.1, 0}, Default: true},
	{ID: "gemini-3.8-flash", Provider: "google", Name: "Gemini 3.8 Flash", Context: 1048576, MaxOutput: 65536, Reasoning: true, Images: true, Price: Price{0.75, 3.75, 0.075, 0}},

	{ID: "deepseek-v4-pro", Provider: "deepseek", Name: "DeepSeek V4 Pro", Context: 1000000, MaxOutput: 384000, Reasoning: true, Price: Price{1.32, 3.96, 0.044, 0}, Default: true},
	{ID: "deepseek-flash", Provider: "deepseek", Name: "DeepSeek V4.1 Flash", Context: 1000000, MaxOutput: 384000, Reasoning: true, Images: true, Price: Price{0.3, 1.2, 0.006, 0}},
}

// FindModel looks a model up by provider and id.
func FindModel(provider, id string) (Model, bool) {
	for _, m := range Catalog {
		if m.Provider == provider && m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// DefaultModel is the model a provider's sessions start with; "" when the
// catalog has none (OpenRouter, custom endpoints).
func DefaultModel(provider string) string {
	for _, m := range Catalog {
		if m.Provider == provider && m.Default {
			return m.ID
		}
	}
	return ""
}
