package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
)

func TestChatGPTLogin(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accounts/oauth/token" {
			w.WriteHeader(404)
			return
		}
		r.ParseForm()
		form = r.PostForm
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "refresh_token": "rt", "expires_in": 3600,
			"id_token": fakeJWT(map[string]any{"email": "a@b.c"}),
			"scope":    "openid " + chatgptDirectScope,
		})
	}))
	defer srv.Close()
	c := &ChatGPT{AuthBase: srv.URL, Port: freePort(t), HostID: func() (string, error) { return "00000000-0000-4000-8000-000000000000", nil }}
	cred, err := c.Login(context.Background(), Interaction{OpenURL: func(u string) error {
		q := mustURL(t, u).Query()
		if q.Get("client_id") != "dynamic_agent_client" || q.Get("agent_name_hint") != "Talos" ||
			q.Get("ext_agent_host_id") != "urn:uuid:00000000-0000-4000-8000-000000000000" || q.Get("resource") != chatgptResource {
			t.Errorf("authorize url %s", u)
		}
		go http.Get(q.Get("redirect_uri") + "?code=c1&client_id=issued-1&state=" + q.Get("state"))
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cred.ClientID != "issued-1" || cred.Method != "chatgpt" || cred.Email != "a@b.c" || !Subscription(cred) {
		t.Errorf("cred %+v", cred)
	}
	if form.Get("client_id") != "issued-1" || form.Get("code") != "c1" || form.Get("resource") != chatgptResource {
		t.Errorf("form %v", form)
	}
	if _, err := c.Refresh(context.Background(), Credential{Refresh: "rt"}); err == nil {
		t.Error("refreshed without a client id")
	}
	if _, err := chatgptCredential(tokenResponse{AccessToken: "a", Scope: "openid"}, "x"); err == nil {
		t.Error("took a grant without API access")
	}
}

func TestXAIDeviceLogin(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch {
		case r.URL.Path == "/oauth2/device/code":
			if r.PostForm.Get("referrer") != "talos" {
				t.Errorf("form %v", r.PostForm)
			}
			io.WriteString(w, `{"device_code":"dc","user_code":"XY-12","verification_uri":"https://x.ai/device","expires_in":60,"interval":0.01}`)
		case r.PostForm.Get("grant_type") == "refresh_token":
			io.WriteString(w, `{"access_token":"a2","expires_in":60}`) // the refresh token is kept
		case polls.Add(1) == 1:
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"authorization_pending"}`)
		default:
			io.WriteString(w, `{"access_token":"a1","refresh_token":"r1","expires_in":60}`)
		}
	}))
	defer srv.Close()
	x := &XAI{AuthBase: srv.URL}
	var shown string
	start := time.Now()
	cred, err := x.Login(context.Background(), Interaction{DeviceCode: func(code, uri string) { shown = code + " " + uri }})
	if err != nil {
		t.Fatal(err)
	}
	if shown != "XY-12 https://x.ai/device" || cred.Access != "a1" || cred.Method != "xai" || polls.Load() != 2 {
		t.Errorf("shown %q cred %+v polls %d", shown, cred, polls.Load())
	}
	if time.Since(start) > 10*time.Second {
		t.Error("slow polling")
	}
	fresh, err := x.Refresh(context.Background(), cred)
	if err != nil || fresh.Access != "a2" || fresh.Refresh != "r1" {
		t.Errorf("%+v %v", fresh, err)
	}
}

func TestOpenRouterLogin(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"key":"sk-or-1"}`)
	}))
	defer srv.Close()
	cred, err := (&OpenRouter{Base: srv.URL}).Login(context.Background(), Interaction{OpenURL: func(u string) error {
		q := mustURL(t, u).Query()
		cb := q.Get("callback_url")
		if !strings.Contains(cb, "/oauth/callback/") {
			t.Errorf("callback %s", cb)
		}
		go func() {
			http.Get(strings.Replace(cb, "/oauth/callback/", "/oauth/callback/x", 1) + "?code=stray")
			http.Get(cb + "?code=c1")
		}()
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Kind != KindAPIKey || cred.Key != "sk-or-1" || body["code"] != "c1" || body["code_verifier"] == "" || Subscription(cred) {
		t.Errorf("cred %+v body %v", cred, body)
	}
}

// TestOpen checks every provider reaches its endpoint with the right
// protocol and headers.
func TestOpen(t *testing.T) {
	type seen struct {
		path string
		h    http.Header
		body map[string]any
	}
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.URL.Path, r.Header, nil}
		json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(400) // the reply does not matter here
	}))
	defer srv.Close()

	wantPath := map[API]string{APIAnthropic: "/v1/messages", APIResponses: "/responses", APICodex: "/codex/responses", APIChat: "/chat/completions", APIGemini: "/v1beta/models/m:streamGenerateContent"}
	for _, p := range Providers {
		store := &Memory{}
		store.Set(p.ID, Credential{Kind: KindAPIKey, Key: "k", Access: "k", BaseURL: srv.URL})
		prov, err := Open(store, p.ID)
		if err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		llm.Collect(prov.Stream(context.Background(), llm.Request{Model: "m", Effort: "low", MaxTokens: 10, Messages: []llm.Message{llm.Text(llm.User, "hi")}}), nil)
		if got.path != wantPath[p.API] {
			t.Errorf("%s: path %s", p.ID, got.path)
		}
		auth := got.h.Get("Authorization") + got.h.Get("x-api-key") + got.h.Get("x-goog-api-key")
		if !strings.Contains(auth, "k") {
			t.Errorf("%s: no key in %v", p.ID, got.h)
		}
	}

	// Compat flags reach the body.
	store := &Memory{}
	store.Set("deepseek", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL})
	prov, _ := Open(store, "deepseek")
	llm.Collect(prov.Stream(context.Background(), llm.Request{Model: "m", Effort: "high", Messages: []llm.Message{
		llm.Text(llm.User, "hi"),
		{Role: llm.Assistant, Parts: []llm.Part{{Thinking: &llm.Thinking{Text: "plan", Provider: "chat"}}, {ToolCall: &llm.ToolCall{ID: "c", Name: "f"}}}},
	}}), nil)
	if th, _ := got.body["thinking"].(map[string]any); th["type"] != "enabled" {
		t.Errorf("thinking %v", got.body)
	}
	if m := got.body["messages"].([]any)[1].(map[string]any); m["reasoning_content"] != "plan" {
		t.Errorf("reasoning not replayed: %v", m)
	}

	// A custom endpoint may speak Anthropic.
	store.Set("custom/mm", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL, API: "anthropic"})
	prov, _ = Open(store, "custom/mm")
	llm.Collect(prov.Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{llm.Text(llm.User, "hi")}}), nil)
	if got.path != "/v1/messages" {
		t.Error(got.path)
	}
	if _, err := Open(store, "nope"); err == nil {
		t.Error("opened an unknown provider")
	}
}

func TestMultiProviderDispatch(t *testing.T) {
	type hit struct {
		path string
		h    http.Header
		body map[string]any
	}
	var last hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = hit{path: r.URL.Path, h: r.Header.Clone()}
		json.NewDecoder(r.Body).Decode(&last.body)
		w.WriteHeader(400)
	}))
	defer srv.Close()

	store := &Memory{}
	for _, id := range []string{"openrouter", "fireworks", "opencode", "opencode-go"} {
		store.Set(id, Credential{Kind: KindAPIKey, Key: "k-"+id, BaseURL: srv.URL})
	}

	// OpenRouter: Anthropic model vs Chat Completions model.
	orProv, err := Open(store, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(orProv.Stream(context.Background(), llm.Request{
		Model: "anthropic/claude-sonnet-5.5", SessionID: "s-or", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if last.path != "/v1/messages" || last.h.Get("x-api-key") != "k-openrouter" || last.h.Get("x-session-id") != "s-or" {
		t.Errorf("openrouter anthropic: path=%s hdr=%v", last.path, last.h)
	}
	llm.Collect(orProv.Stream(context.Background(), llm.Request{
		Model: "openai/gpt-6.1-sol", SessionID: "s-or2", Effort: "high", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if last.path != "/chat/completions" || last.h.Get("Authorization") != "Bearer k-openrouter" || last.h.Get("x-session-id") != "s-or2" {
		t.Errorf("openrouter chat: path=%s hdr=%v", last.path, last.h)
	}

	// Fireworks: Anthropic model (NoToolCache) vs Chat Completions model.
	fwProv, err := Open(store, "fireworks")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(fwProv.Stream(context.Background(), llm.Request{
		Model: "accounts/fireworks/models/deepseek-v4p1-flash",
		Tools: []llm.Tool{{Name: "t", Description: "d"}},
		Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if last.path != "/v1/messages" || last.h.Get("x-api-key") != "k-fireworks" {
		t.Errorf("fireworks anthropic: path=%s hdr=%v", last.path, last.h)
	}
	if tools, _ := last.body["tools"].([]any); len(tools) != 1 || tools[0].(map[string]any)["cache_control"] != nil {
		t.Errorf("fireworks should disable tool cache_control: %v", last.body["tools"])
	}
	llm.Collect(fwProv.Stream(context.Background(), llm.Request{
		Model: "accounts/fireworks/models/glm-5p3", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if last.path != "/chat/completions" || last.h.Get("Authorization") != "Bearer k-fireworks" {
		t.Errorf("fireworks chat: path=%s hdr=%v", last.path, last.h)
	}

	// OpenCode Zen: all 4 wire APIs + x-opencode-session.
	ocProv, err := Open(store, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model, path, authHdr string
	}{
		{"claude-fable-5", "/v1/messages", "x-api-key"},
		{"gemini-3-flash", "/v1beta/models/gemini-3-flash:streamGenerateContent", "x-goog-api-key"},
		{"beam-free", "/chat/completions", "Authorization"},
		{"gpt-5", "/responses", "Authorization"},
	} {
		llm.Collect(ocProv.Stream(context.Background(), llm.Request{
			Model: tc.model, SessionID: "oc-sess", Messages: []llm.Message{llm.Text(llm.User, "hi")},
		}), nil)
		if last.path != tc.path || last.h.Get(tc.authHdr) == "" || last.h.Get("x-opencode-session") != "oc-sess" {
			t.Errorf("opencode %s: path=%s hdr=%v", tc.model, last.path, last.h)
		}
	}

	// OpenCode Go: Anthropic + Gemini + Chat + x-opencode-session.
	ocgProv, err := Open(store, "opencode-go")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model, path string
	}{
		{"claude-haiku-5-5", "/v1/messages"},
		{"gemini-4-argon", "/v1beta/models/gemini-4-argon:streamGenerateContent"},
		{"beam-free", "/chat/completions"},
	} {
		llm.Collect(ocgProv.Stream(context.Background(), llm.Request{
			Model: tc.model, SessionID: "ocg-sess", Messages: []llm.Message{llm.Text(llm.User, "hi")},
		}), nil)
		if last.path != tc.path || last.h.Get("x-opencode-session") != "ocg-sess" {
			t.Errorf("opencode-go %s: path=%s hdr=%v", tc.model, last.path, last.h)
		}
	}
}

func TestProviderCompatAndHeaders(t *testing.T) {
	var lastH http.Header
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastH = r.Header.Clone()
		lastBody = nil
		json.NewDecoder(r.Body).Decode(&lastBody)
		w.WriteHeader(400)
	}))
	defer srv.Close()

	store := &Memory{}
	store.Set("nvidia", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL})
	nv, err := Open(store, "nvidia")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(nv.Stream(context.Background(), llm.Request{
		Model: "deepseek-ai/deepseek-v4.1-flash", Effort: "high", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if lastH.Get("NVCF-POLL-SECONDS") != "3600" {
		t.Errorf("nvidia missing poll header: %v", lastH)
	}

	store.Set("zai", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL})
	zaiProv, err := Open(store, "zai")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(zaiProv.Stream(context.Background(), llm.Request{
		Model:    "glm-5.3",
		Effort:   "high",
		Tools:    []llm.Tool{{Name: "t", Description: "d"}},
		Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if lastBody["tool_stream"] != true {
		t.Errorf("zai missing tool_stream: %v", lastBody)
	}
	if th, _ := lastBody["thinking"].(map[string]any); th["type"] != "enabled" || th["clear_thinking"] != false {
		t.Errorf("zai thinking: %v", lastBody["thinking"])
	}

	store.Set("baseten", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL})
	bt, err := Open(store, "baseten")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(bt.Stream(context.Background(), llm.Request{
		Model: "zai-org/GLM-5.2", Effort: "high", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if cta, _ := lastBody["chat_template_args"].(map[string]any); cta["enable_thinking"] != true {
		t.Errorf("baseten chat_template_args: %v", lastBody)
	}

	// Legacy "moonshot" alias resolves to moonshotai.
	p, ok := Lookup("moonshot")
	if !ok || p.ID != "moonshotai" {
		t.Fatalf("Lookup(moonshot) = %+v, %v", p, ok)
	}
	store.Set("moonshot", Credential{Kind: KindAPIKey, Key: "k", BaseURL: srv.URL})
	ms, err := Open(store, "moonshot")
	if err != nil {
		t.Fatal(err)
	}
	llm.Collect(ms.Stream(context.Background(), llm.Request{
		Model: "kimi-k3.1-pro", Effort: "high", Messages: []llm.Message{llm.Text(llm.User, "hi")},
	}), nil)
	if lastH.Get("Authorization") != "Bearer k" {
		t.Errorf("moonshot alias auth header: %v", lastH)
	}
}
