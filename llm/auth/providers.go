package auth

import (
	"fmt"
	"net/http"
	"strings"

	"talos/llm"
)

// Provider describes a model provider Talos can sign in to.
type Provider struct {
	ID   string // account id prefix: "anthropic", "chatgpt", ...
	Name string
	// OAuth providers sign in through the browser; the others take an API
	// key.
	OAuth bool
	// KeyURL is where the user creates an API key.
	KeyURL string
	// open builds the adapter from a credential source.
	open func(s *Source, c Credential) llm.Provider
}

func bearer(c Credential) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Key)
	return h
}

func header(name string) func(Credential) http.Header {
	return func(c Credential) http.Header {
		h := http.Header{}
		h.Set(name, c.Key)
		return h
	}
}

// Providers lists the providers in the order a sign-in screen shows them.
var Providers = []Provider{
	{ID: "chatgpt", Name: "ChatGPT", OAuth: true, open: func(s *Source, c Credential) llm.Provider {
		s.Refresh = (&Codex{}).Refresh
		return &llm.OpenAI{Codex: true, Auth: s.Auth(CodexHeaders)}
	}},
	{ID: "anthropic", Name: "Anthropic", KeyURL: "https://console.anthropic.com/settings/keys", open: func(s *Source, c Credential) llm.Provider {
		return &llm.Anthropic{BaseURL: c.BaseURL, Auth: s.Auth(header("x-api-key"))}
	}},
	{ID: "openai", Name: "OpenAI", KeyURL: "https://platform.openai.com/api-keys", open: func(s *Source, c Credential) llm.Provider {
		return &llm.OpenAI{BaseURL: c.BaseURL, Auth: s.Auth(bearer)}
	}},
	{ID: "google", Name: "Google Gemini", KeyURL: "https://aistudio.google.com/apikey", open: func(s *Source, c Credential) llm.Provider {
		return &llm.Gemini{BaseURL: c.BaseURL, Auth: s.Auth(header("x-goog-api-key"))}
	}},
	{ID: "openrouter", Name: "OpenRouter", KeyURL: "https://openrouter.ai/keys", open: func(s *Source, c Credential) llm.Provider {
		return &llm.Chat{BaseURL: or(c.BaseURL, "https://openrouter.ai/api/v1"), Auth: s.Auth(bearer)}
	}},
	{ID: "deepseek", Name: "DeepSeek", KeyURL: "https://platform.deepseek.com/api_keys", open: func(s *Source, c Credential) llm.Provider {
		return &llm.Chat{BaseURL: or(c.BaseURL, "https://api.deepseek.com"), Auth: s.Auth(bearer)}
	}},
	{ID: "custom", Name: "OpenAI-compatible endpoint", open: func(s *Source, c Credential) llm.Provider {
		return &llm.Chat{BaseURL: c.BaseURL, Auth: s.Auth(bearer)}
	}},
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Lookup finds the provider of an account: "anthropic", or "anthropic/work"
// for a second account.
func Lookup(account string) (Provider, bool) {
	id, _, _ := strings.Cut(account, "/")
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Open connects to a signed-in account.
func Open(store Store, account string) (llm.Provider, error) {
	p, ok := Lookup(account)
	if !ok {
		return nil, fmt.Errorf("auth: unknown provider of %q", account)
	}
	c, err := store.Get(account)
	if err != nil {
		return nil, err
	}
	if p.ID == "custom" && c.BaseURL == "" {
		return nil, fmt.Errorf("auth: %s has no endpoint address", account)
	}
	return p.open(&Source{Store: store, Account: account}, c), nil
}
