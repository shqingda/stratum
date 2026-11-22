package auth

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"strings"

	"github.com/shqingda/stratum/llm"
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
	"kimi":       {ID: "kimi", Label: "Sign in with Kimi Code", Subscription: true, Flow: &Kimi{}},
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
	// APIs lists all wire protocols when a gateway serves more than one.
	APIs          []API
	BaseURL       string
	Headers       map[string]string
	SessionHeader string
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
	// Catalog is the provider's id on models.dev, when it differs from ID,
	// or "-" to rely solely on the embedded catalog.
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

	// Gateways serving multiple models / wire APIs.
	{ID: "openrouter", Name: "OpenRouter", API: APIChat, APIs: []API{APIChat, APIAnthropic}, BaseURL: "https://openrouter.ai/api/v1",
		KeyURL: "https://openrouter.ai/keys", KeyEnv: "OPENROUTER_API_KEY", Logins: []string{"openrouter"},
		Compat: llm.ChatCompat{Thinking: "openrouter", SessionAffinityFormat: "openrouter"}},
	{ID: "opencode", Name: "OpenCode Zen", API: APIAnthropic, APIs: []API{APIAnthropic, APIGemini, APIChat, APIResponses},
		BaseURL: "https://opencode.ai/inference/anthropic", SessionHeader: "x-opencode-session",
		KeyURL: "https://opencode.ai/auth", KeyEnv: "OPENCODE_API_KEY", Catalog: "-"},
	{ID: "opencode-go", Name: "OpenCode Go", API: APIAnthropic, APIs: []API{APIAnthropic, APIGemini, APIChat, APIResponses},
		BaseURL: "https://opencode.ai/inference/go/anthropic", SessionHeader: "x-opencode-session",
		KeyURL: "https://opencode.ai/auth", KeyEnv: "OPENCODE_API_KEY", Catalog: "-"},
	{ID: "vercel-ai-gateway", Name: "Vercel AI Gateway", API: APIAnthropic, BaseURL: "https://ai-gateway.vercel.sh",
		KeyURL: "https://vercel.com/ai-gateway", KeyEnv: "AI_GATEWAY_API_KEY", Catalog: "-"},
	{ID: "fireworks", Name: "Fireworks", API: APIChat, APIs: []API{APIAnthropic, APIChat}, BaseURL: "https://api.fireworks.ai/inference/v1",
		KeyURL: "https://fireworks.ai/account/api-keys", KeyEnv: "FIREWORKS_API_KEY", Catalog: "-"},

	// Fast inference and open-weight model clouds.
	{ID: "groq", Name: "Groq", API: APIChat, BaseURL: "https://api.groq.com/openai/v1",
		KeyURL: "https://console.groq.com/keys", KeyEnv: "GROQ_API_KEY"},
	{ID: "cerebras", Name: "Cerebras", API: APIChat, BaseURL: "https://api.cerebras.ai/v1",
		KeyURL: "https://cloud.cerebras.ai/", KeyEnv: "CEREBRAS_API_KEY", Catalog: "-"},
	{ID: "together", Name: "Together", API: APIChat, BaseURL: "https://api.together.ai/v1",
		KeyURL: "https://api.together.xyz/settings/api-keys", KeyEnv: "TOGETHER_API_KEY",
		Compat: llm.ChatCompat{Thinking: "together"}, Catalog: "-"},
	{ID: "baseten", Name: "Baseten", API: APIChat, BaseURL: "https://inference.baseten.co/v1",
		KeyURL: "https://app.baseten.co/settings/api_keys", KeyEnv: "BASETEN_API_KEY", Catalog: "-"},
	{ID: "huggingface", Name: "Hugging Face", API: APIChat, BaseURL: "https://router.huggingface.co/v1",
		KeyURL: "https://huggingface.co/settings/tokens", KeyEnv: "HF_TOKEN", Catalog: "-"},
	{ID: "nvidia", Name: "NVIDIA", API: APIChat, BaseURL: "https://integrate.api.nvidia.com/v1",
		Headers: map[string]string{"NVCF-POLL-SECONDS": "3600"},
		KeyURL:  "https://build.nvidia.com/explore/discover", KeyEnv: "NVIDIA_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true, NoReasoningEffort: true}, Catalog: "-"},

	// Global & China labs and subscription plans.
	{ID: "deepseek", Name: "DeepSeek", API: APIChat, BaseURL: "https://api.deepseek.com",
		KeyURL: "https://platform.deepseek.com/api_keys", KeyEnv: "DEEPSEEK_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}},
	{ID: "kimi-coding", Name: "Kimi For Coding", API: APIChat, BaseURL: "https://api.kimi.com/coding/v1",
		KeyURL: "https://www.kimi.com/code/console", KeyEnv: "KIMI_API_KEY", Logins: []string{"kimi"},
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}, Catalog: "-"},
	{ID: "kimi-coding-global", Name: "Kimi For Coding Global", API: APIChat, BaseURL: "https://api.kimi.ai/coding/v1",
		KeyURL: "https://www.kimi.com/code/console", KeyEnv: "KIMI_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}, Catalog: "-"},
	{ID: "moonshotai", Name: "Moonshot AI (Kimi)", API: APIChat, BaseURL: "https://api.moonshot.ai/v1",
		Regions: map[string]string{"cn": "https://api.moonshot.cn/v1"},
		KeyURL:  "https://platform.moonshot.ai/console/api-keys", KeyEnv: "MOONSHOT_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}, Catalog: "-"},
	{ID: "moonshotai-cn", Name: "Moonshot AI CN (Kimi)", API: APIChat, BaseURL: "https://api.moonshot.cn/v1",
		KeyURL: "https://platform.moonshot.cn/console/api-keys", KeyEnv: "MOONSHOT_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true}, Catalog: "-"},
	{ID: "zai", Name: "Z.AI (GLM Coding)", API: APIChat, BaseURL: "https://api.z.ai/api/coding/paas/v4",
		Regions: map[string]string{"cn": "https://open.bigmodel.cn/api/coding/paas/v4"},
		KeyURL:  "https://z.ai/manage-apikey/apikey-list", KeyEnv: "ZAI_API_KEY",
		Compat: llm.ChatCompat{Thinking: "zai", ZaiToolStream: true}, Catalog: "-"},
	{ID: "zai-api", Name: "Z.AI Pay-as-you-go", API: APIChat, BaseURL: "https://api.z.ai/api/paas/v4",
		KeyURL: "https://z.ai/manage-apikey/apikey-list", KeyEnv: "ZAI_API_KEY",
		Compat: llm.ChatCompat{Thinking: "zai", ZaiToolStream: true}, Catalog: "-"},
	{ID: "zai-coding-cn", Name: "Z.AI Coding CN (GLM)", API: APIChat, BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
		KeyURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys", KeyEnv: "ZAI_CODING_CN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "zai", ZaiToolStream: true}, Catalog: "-"},
	{ID: "zhipuai", Name: "Zhipu AI (GLM)", API: APIChat, BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		KeyURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys", KeyEnv: "ZHIPU_API_KEY",
		Compat: llm.ChatCompat{Thinking: "zai", ZaiToolStream: true}, Catalog: "-"},
	{ID: "qwen", Name: "Qwen (Alibaba Cloud)", API: APIChat, BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		Regions: map[string]string{"cn": "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		KeyURL:  "https://modelstudio.console.alibabacloud.com/?tab=playground#/api-key", KeyEnv: "DASHSCOPE_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "alibaba"},
	{ID: "qwen-token-plan", Name: "Qwen Token Plan", API: APIChat, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
		KeyURL: "https://modelstudio.console.alibabacloud.com/", KeyEnv: "QWEN_TOKEN_PLAN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "-"},
	{ID: "qwen-token-plan-cn", Name: "Qwen Token Plan CN", API: APIChat, BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		KeyURL: "https://bailian.console.aliyun.com/", KeyEnv: "QWEN_TOKEN_PLAN_CN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "-"},
	{ID: "qwen-token-plan-individual", Name: "Qwen Token Plan Individual", API: APIChat, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
		KeyURL: "https://modelstudio.console.alibabacloud.com/", KeyEnv: "QWEN_TOKEN_PLAN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "-"},
	{ID: "qwen-token-plan-individual-cn", Name: "Qwen Token Plan Individual CN", API: APIChat, BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		KeyURL: "https://bailian.console.aliyun.com/", KeyEnv: "QWEN_TOKEN_PLAN_CN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "qwen"}, Catalog: "-"},
	{ID: "minimax", Name: "MiniMax", API: APIAnthropic, BaseURL: "https://api.minimax.io/anthropic",
		Regions: map[string]string{"cn": "https://api.minimaxi.com/anthropic"},
		KeyURL:  "https://platform.minimax.io/user-center/basic-information/interface-key", KeyEnv: "MINIMAX_API_KEY", Catalog: "-"},
	{ID: "minimax-cn", Name: "MiniMax CN", API: APIAnthropic, BaseURL: "https://api.minimaxi.com/anthropic",
		KeyURL: "https://platform.minimaxi.com/user-center/basic-information/interface-key", KeyEnv: "MINIMAX_CN_API_KEY", Catalog: "-"},
	{ID: "xiaomi", Name: "Xiaomi MiMo", API: APIChat, BaseURL: "https://api.xiaomimimo.com/v1",
		KeyURL: "https://platform.xiaomimimo.com/", KeyEnv: "XIAOMI_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true, NoReasoningEffort: true}, Catalog: "-"},
	{ID: "xiaomi-token-plan-cn", Name: "Xiaomi Token Plan CN", API: APIChat, BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		KeyURL: "https://platform.xiaomimimo.com/", KeyEnv: "XIAOMI_TOKEN_PLAN_CN_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true, NoReasoningEffort: true}, Catalog: "-"},
	{ID: "xiaomi-token-plan-sgp", Name: "Xiaomi Token Plan SGP", API: APIChat, BaseURL: "https://token-plan-sgp.xiaomimimo.com/v1",
		KeyURL: "https://platform.xiaomimimo.com/", KeyEnv: "XIAOMI_TOKEN_PLAN_SGP_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true, NoReasoningEffort: true}, Catalog: "-"},
	{ID: "xiaomi-token-plan-ams", Name: "Xiaomi Token Plan AMS", API: APIChat, BaseURL: "https://token-plan-ams.xiaomimimo.com/v1",
		KeyURL: "https://platform.xiaomimimo.com/", KeyEnv: "XIAOMI_TOKEN_PLAN_AMS_API_KEY",
		Compat: llm.ChatCompat{Thinking: "deepseek", ReplayReasoning: true, NoReasoningEffort: true}, Catalog: "-"},
	{ID: "ant-ling", Name: "Ant Ling", API: APIChat, BaseURL: "https://api.ant-ling.com/v1",
		KeyURL: "https://ling.tbox.cn/", KeyEnv: "ANT_LING_API_KEY",
		Compat: llm.ChatCompat{Thinking: "ant-ling", NoReasoningEffort: true}, Catalog: "-"},
	{ID: "custom", Name: "Custom endpoint", API: APIChat},
}

// Lookup finds the provider of an account: "anthropic", or "anthropic/work"
// for a second account.
func Lookup(account string) (Provider, bool) {
	id, _, _ := strings.Cut(account, "/")
	if id == "moonshot" {
		id = "moonshotai"
	}
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
	hc := quotaHTTPClient(account)
	if len(p.APIs) > 1 && p.ID != "custom" {
		return &multiProvider{
			provider:     p,
			source:       s,
			method:       method,
			explicitBase: c.BaseURL,
			httpClient:   hc,
		}, nil
	}
	return openProtocol(api, base, p, method, s, hc)
}

func openProtocol(api API, base string, p Provider, method string, s *Source, hc *http.Client) (llm.Provider, error) {
	headers := keyHeaders(api)
	if method == "copilot" {
		headers = CopilotHeaders
	}
	switch api {
	case APIAnthropic:
		return &llm.Anthropic{BaseURL: base, HTTP: hc, Auth: s.Auth(headers), Provider: p.ID, Headers: p.Headers, SessionHeader: p.SessionHeader}, nil
	case APIResponses:
		return &llm.OpenAI{BaseURL: base, HTTP: hc, Auth: s.Auth(headers), Provider: p.ID, Headers: p.Headers, SessionHeader: p.SessionHeader}, nil
	case APICodex:
		return &llm.OpenAI{BaseURL: base, HTTP: hc, Codex: true, Auth: s.Auth(CodexHeaders), Provider: p.ID, Headers: p.Headers, SessionHeader: p.SessionHeader}, nil
	case APIGemini:
		return &llm.Gemini{BaseURL: base, HTTP: hc, Auth: s.Auth(headers), Provider: p.ID, Headers: p.Headers, SessionHeader: p.SessionHeader}, nil
	case APIChat:
		return &llm.Chat{BaseURL: base, HTTP: hc, Compat: p.Compat, Auth: s.Auth(headers), Provider: p.ID, Headers: p.Headers, SessionHeader: p.SessionHeader}, nil
	}
	return nil, fmt.Errorf("auth: %s: unknown protocol %q", s.Account, api)
}

// multiProvider dispatches each request to the wire API and base URL declared
// by the selected model in the catalog, falling back to the provider defaults.
type multiProvider struct {
	provider     Provider
	source       *Source
	method       string
	explicitBase string
	httpClient   *http.Client
}

func (m *multiProvider) SetHTTP(hc *http.Client) {
	m.httpClient = hc
}

func (m *multiProvider) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Event, error] {
	api := m.provider.API
	base := m.explicitBase
	if model, ok := llm.FindModel(m.provider.ID, req.Model); ok {
		if model.API != "" {
			api = API(model.API)
		}
		if base == "" && model.BaseURL != "" {
			base = model.BaseURL
		}
	}
	if base == "" {
		base = m.provider.BaseURL
	}
	prov, err := openProtocol(api, base, m.provider, m.method, m.source, m.httpClient)
	if err != nil {
		return func(yield func(llm.Event, error) bool) {
			yield(llm.Event{}, err)
		}
	}
	return prov.Stream(ctx, req)
}

type quotaTransport struct {
	account string
	base    http.RoundTripper
}

func (t *quotaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && resp != nil {
		Quotas.RecordResponseHeaders(t.account, resp.Header)
	}
	return resp, err
}

func quotaHTTPClient(account string) *http.Client {
	return &http.Client{Transport: &quotaTransport{account: account}}
}

// CatalogNames maps models.dev's provider ids to Talos's, for
// llm.LoadCatalog.
func CatalogNames() map[string]string {
	m := map[string]string{}
	for _, p := range Providers {
		if p.API == APICodex || p.ID == "custom" || p.Catalog == "-" {
			continue
		}
		m[cmpOr(p.Catalog, p.ID)] = p.ID
	}
	return m
}
