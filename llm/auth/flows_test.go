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

	"talos/llm"
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
