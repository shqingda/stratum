package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/shqingda/talos/llm"
)

// Flow is one way to sign in with a browser or a device code.
type Flow interface {
	Login(ctx context.Context, in Interaction) (Credential, error)
	Refresh(ctx context.Context, c Credential) (Credential, error)
}

// Method describes a sign-in a provider offers besides an API key.
type Method struct {
	ID    string // the Credential.Method it issues
	Label string // "Sign in with ChatGPT"
	// Subscription means requests use the user's plan, not metered tokens.
	Subscription bool
	Flow         Flow
}

// Methods are the sign-ins Talos knows, by id.
var Methods = map[string]*Method{
	"chatgpt":    {ID: "chatgpt", Label: "Sign in with ChatGPT", Subscription: true, Flow: &ChatGPT{}},
	"codex":      {ID: "codex", Label: "Sign in with ChatGPT (Codex)", Subscription: true, Flow: &Codex{}},
	"xai":        {ID: "xai", Label: "Sign in with SuperGrok or X Premium", Subscription: true, Flow: &XAI{}},
	"openrouter": {ID: "openrouter", Label: "Sign in with OpenRouter", Flow: &OpenRouter{}},
	"copilot":    {ID: "copilot", Label: "Sign in with GitHub Copilot", Subscription: true, Flow: &Copilot{}},
}

// API is a wire protocol.
type API string

const (
	APIAnthropic API = "anthropic" // Anthropic Messages
	APIResponses API = "responses" // OpenAI Responses
	APICodex     API = "codex"     // OpenAI Responses on the ChatGPT backend
	APIChat      API = "chat"      // Chat Completions
	APIGemini    API = "gemini"    // Gemini generateContent
)

// Provider is a model provider, described as data: adding one that speaks a
// known protocol is adding an entry.
type Provider struct {
	ID, Name string
	API      API
	BaseURL  string
	// Regions are other endpoints by name, such as mainland China's.
	Regions map[string]string
	// KeyURL is where the user makes an API key; empty when the provider
	// takes none.
	KeyURL string
	// KeyEnv is the environment variable that conventionally holds the key.
	KeyEnv string
	// Logins are the Methods the provider offers besides a key.
	Logins []string
	Compat llm.ChatCompat
	// Catalog is the provider's id on models.dev, when it differs from ID.
	Catalog string
}

// Providers lists the providers in the order a sign-in screen shows them.
var Providers = []Provider{
	{ID: "openai", Name: "OpenAI", API: APIResponses, BaseURL: "https://api.openai.com/v1",
		KeyURL: "https://platform.openai.com/api-keys", KeyEnv: "OPENAI_API_KEY", Logins: []string{"chatgpt"}},
	{ID: "codex", Name: "ChatGPT (Codex)", API: APICodex, BaseURL: "https://chatgpt.com/backend-api", Logins: []string{"codex"}},
	{ID: "copilot", Name: "GitHub Copilot", API: APIChat, BaseURL: "https://api.githubcopilot.com",
		Logins: []string{"copilot"}, Catalog: "github-copilot"},
	{ID: "anthropic", Name: "Anthropic", API: APIAnthropic, BaseURL: "https://api.anthropic.com",
		KeyURL: "https://console.anthropic.com/settings/keys", KeyEnv: "ANTHROPIC_API_KEY"},
	{ID: "google", Name: "Google Gemini", API: APIGemini, BaseURL: "https://generativelanguage.googleapis.com",
		KeyURL: "https://aistudio.google.com/apikey", KeyEnv: "GEMINI_API_KEY"},
	{ID: "xai", Name: "xAI", API: APIResponses, BaseURL: "https://api.x.ai/v1",
		KeyURL: "https://console.x.ai", KeyEnv: "XAI_API_KEY", Logins: []string{"xai"}},
	{ID: "openrouter", Name: "OpenRouter", API: APIChat, BaseURL: "https://openrouter.ai/api/v1",
		KeyURL: "https://openrouter.ai/keys", KeyEnv: "OPENROUTER_API_KEY", Logins: []string{"openrouter"},
		Compat: llm.ChatCompat{Thinking: "openrouter"}},
	{ID: "groq", Name: "Groq", API: APIChat, BaseURL: "https://api.groq.com/openai/v1",
		KeyURL: "https://console.groq.com/keys", KeyEnv: "GROQ_API_KEY"},
	{ID: "deepseek", Name: "DeepSeek", API: APIChat, BaseURL: "https://api.deepseek.com",
		KeyURL: "https://platform.deepseek.com/api_keys", KeyEnv: "DEEPSEEK_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}},
	{ID: "moonshot", Name: "Kimi (Moonshot)", API: APIChat, BaseURL: "https://api.moonshot.ai/v1",
		Regions: map[string]string{"cn": "https://api.moonshot.cn/v1"},
		KeyURL:  "https://platform.moonshot.ai/console/api-keys", KeyEnv: "MOONSHOT_API_KEY",
		Compat: llm.ChatCompat{Thinking: "none"}, Catalog: "moonshotai"},
	{ID: "zai", Name: "Z.ai (GLM)", API: APIChat, BaseURL: "https://api.z.ai/api/paas/v4",
		Regions: map[string]string{"cn": "https://open.bigmodel.cn/api/paas/v4"},
		KeyURL:  "https://z.ai/manage-apikey/apikey-list", KeyEnv: "ZAI_API_KEY",
		Compat: llm.ChatCompat{Thinking: "zai"}},
	{ID: "qwen", Name: "Qwen (Alibaba Cloud)", API: APIChat, BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		Regions: map[string]string{"cn": "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		KeyURL:  "https://modelstudio.console.alibabacloud.com/?tab=playground#/api-key", KeyEnv: "DASHSCOPE_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "alibaba"},
	{ID: "minimax", Name: "MiniMax", API: APIAnthropic, BaseURL: "https://api.minimax.io/anthropic",
		Regions: map[string]string{"cn": "https://api.minimaxi.com/anthropic"},
		KeyURL:  "https://platform.minimax.io/user-center/basic-information/interface-key", KeyEnv: "MINIMAX_API_KEY"},
	{ID: "custom", Name: "Custom endpoint", API: APIChat},
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

// Subscription tells whether a credential spends a plan rather than metered
// tokens.
func Subscription(c Credential) bool {
	m := Methods[c.Method]
	return c.Kind == KindOAuth && m != nil && m.Subscription
}

// keyHeaders authenticates with an API key, or an access token used like
// one, the way the protocol expects.
func keyHeaders(api API) func(Credential) http.Header {
	return func(c Credential) http.Header {
		secret := cmpOr(c.Key, c.Access)
		h := http.Header{}
		switch api {
		case APIAnthropic:
			h.Set("x-api-key", secret)
		case APIGemini:
			h.Set("x-goog-api-key", secret)
		default:
			h.Set("Authorization", "Bearer "+secret)
		}
		return h
	}
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
	api, base := p.API, cmpOr(c.BaseURL, p.BaseURL)
	if p.ID == "custom" {
		if base == "" {
			return nil, fmt.Errorf("auth: %s has no endpoint address", account)
		}
		api = API(cmpOr(c.API, string(APIChat)))
	}
	method := c.Method
	if method == "" && c.Kind == KindOAuth && len(p.Logins) == 1 {
		method = p.Logins[0] // credentials from before Method was recorded
	}
	s := &Source{Store: store, Account: account}
	if m := Methods[method]; m != nil {
		s.Refresh = m.Flow.Refresh
	}
	headers := keyHeaders(api)
	if method == "copilot" {
		headers = CopilotHeaders
	}
	switch api {
	case APIAnthropic:
		return &llm.Anthropic{BaseURL: base, Auth: s.Auth(headers)}, nil
	case APIResponses:
		return &llm.OpenAI{BaseURL: base, Auth: s.Auth(headers)}, nil
	case APICodex:
		return &llm.OpenAI{BaseURL: base, Codex: true, Auth: s.Auth(CodexHeaders)}, nil
	case APIGemini:
		return &llm.Gemini{BaseURL: base, Auth: s.Auth(headers)}, nil
	case APIChat:
		return &llm.Chat{BaseURL: base, Compat: p.Compat, Auth: s.Auth(headers)}, nil
	}
	return nil, fmt.Errorf("auth: %s: unknown protocol %q", account, api)
}

// CatalogNames maps models.dev's provider ids to Talos's, for
// llm.LoadCatalog.
func CatalogNames() map[string]string {
	m := map[string]string{}
	for _, p := range Providers {
		if p.API == APICodex || p.ID == "custom" {
			continue
		}
		m[cmpOr(p.Catalog, p.ID)] = p.ID
	}
	return m
}
