package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
)

func fakeJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func accessToken(account string) string {
	return fakeJWT(map[string]any{codexClaim: map[string]any{"chatgpt_account_id": account}})
}

// fakeOpenAI plays auth.openai.com: the token endpoint and the device flow.
type fakeOpenAI struct {
	t        *testing.T
	forms    []url.Values
	polls    atomic.Int32
	pending  int32 // polls answered "pending" before the code
	verifier string
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth/token":
		r.ParseForm()
		f.forms = append(f.forms, r.PostForm)
		if r.PostForm.Get("grant_type") == "refresh_token" && r.PostForm.Get("refresh_token") != "r1" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken("acct-1"),
			"refresh_token": "r2",
			"id_token":      fakeJWT(map[string]any{"email": "a@b.c"}),
			"expires_in":    3600,
		})
	case "/api/accounts/deviceauth/usercode":
		io.WriteString(w, `{"device_auth_id":"dev1","user_code":"ABCD-1234","interval":"0.01"}`)
	case "/api/accounts/deviceauth/token":
		if f.polls.Add(1) <= f.pending {
			w.WriteHeader(403)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"authorization_code": "devcode", "code_verifier": "devver"})
	default:
		w.WriteHeader(404)
	}
}

func TestCodexBrowserLogin(t *testing.T) {
	f := &fakeOpenAI{t: t}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Codex{AuthBase: srv.URL, Port: freePort(t)}

	var challenge string
	cred, err := c.Login(context.Background(), Interaction{OpenURL: func(u string) error {
		q := mustURL(t, u).Query()
		if q.Get("client_id") != codexClientID || q.Get("originator") != "talos" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("authorize url %s", u)
		}
		challenge = q.Get("code_challenge")
		// The browser lands on the loopback redirect, after a stray hit.
		go func() {
			http.Get(q.Get("redirect_uri") + "?code=x&state=wrong")
			http.Get(q.Get("redirect_uri") + "?code=thecode&state=" + q.Get("state"))
		}()
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccountID != "acct-1" || cred.Email != "a@b.c" || cred.Refresh != "r2" || cred.Kind != KindOAuth {
		t.Errorf("cred %+v", cred)
	}
	if time.Until(cred.Expires) < 50*time.Minute {
		t.Errorf("expires %v", cred.Expires)
	}
	form := f.forms[0]
	if form.Get("code") != "thecode" || form.Get("grant_type") != "authorization_code" || !strings.HasSuffix(form.Get("redirect_uri"), "/auth/callback") {
		t.Errorf("form %v", form)
	}
	sum := pkceChallenge(form.Get("code_verifier"))
	if sum != challenge {
		t.Error("verifier does not match the challenge")
	}
}

func TestCodexPasteWhenPortTaken(t *testing.T) {
	f := &fakeOpenAI{t: t}
	srv := httptest.NewServer(f)
	defer srv.Close()
	busy := httptest.NewServer(http.NotFoundHandler()) // holds the port
	defer busy.Close()
	port := mustURL(t, busy.URL).Port()
	c := &Codex{AuthBase: srv.URL, Port: atoi(port)}

	var state string
	cred, err := c.Login(context.Background(), Interaction{
		OpenURL: func(u string) error { state = mustURL(t, u).Query().Get("state"); return nil },
		Paste: func(context.Context) (string, error) {
			return "http://localhost:" + port + "/auth/callback?code=pasted&state=" + state, nil
		},
	})
	if err != nil || cred.AccountID != "acct-1" || f.forms[0].Get("code") != "pasted" {
		t.Fatalf("%+v %v", cred, err)
	}
	if _, err := codeFromRedirect("http://x/?code=c&state=other", state); err == nil {
		t.Error("took a code from another sign-in")
	}
}

func TestCodexDeviceLogin(t *testing.T) {
	f := &fakeOpenAI{t: t, pending: 2}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var shown string
	cred, err := (&Codex{AuthBase: srv.URL}).LoginDevice(context.Background(), Interaction{
		DeviceCode: func(code, uri string) { shown = code + " " + uri },
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown != "ABCD-1234 "+srv.URL+"/codex/device" || f.polls.Load() != 3 || cred.AccountID != "acct-1" {
		t.Errorf("shown %q polls %d cred %+v", shown, f.polls.Load(), cred)
	}
	if form := f.forms[0]; form.Get("code_verifier") != "devver" || !strings.HasSuffix(form.Get("redirect_uri"), "/deviceauth/callback") {
		t.Errorf("form %v", form)
	}
}

func TestSourceRefreshes(t *testing.T) {
	f := &fakeOpenAI{t: t}
	srv := httptest.NewServer(f)
	defer srv.Close()
	store := &Memory{}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	store.Set("chatgpt", Credential{Kind: KindOAuth, Access: "old", Refresh: "r1", Expires: now.Add(10 * time.Minute), AccountID: "acct-1", Email: "a@b.c"})
	s := &Source{Store: store, Account: "chatgpt", Refresh: (&Codex{AuthBase: srv.URL}).Refresh, Now: func() time.Time { return now }}

	c, err := s.Credential(context.Background())
	if err != nil || c.Access != "old" || len(f.forms) != 0 {
		t.Fatalf("refreshed too early: %+v %v", c, err)
	}
	now = now.Add(6 * time.Minute) // inside the margin
	h, err := s.Auth(CodexHeaders)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("Authorization") == "Bearer old" || h.Get("chatgpt-account-id") != "acct-1" || len(f.forms) != 1 {
		t.Errorf("headers %v forms %d", h, len(f.forms))
	}
	saved, _ := store.Get("chatgpt")
	if saved.Refresh != "r2" || saved.Email != "a@b.c" {
		t.Errorf("saved %+v", saved)
	}
	// The saved refresh token (r2) is now the one the fake rejects.
	now = now.Add(30 * 24 * time.Hour)
	if _, err := s.Credential(context.Background()); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Errorf("err %v", err)
	}
}

func TestStores(t *testing.T) {
	m := &Memory{}
	if _, err := m.Get("x"); err != ErrNotFound {
		t.Fatal(err)
	}
	m.Set("openai", Credential{Kind: KindAPIKey, Key: "k"})
	m.Set("anthropic/work", Credential{Kind: KindAPIKey, Key: "k2"})
	if l, _ := m.List(); strings.Join(l, ",") != "anthropic/work,openai" {
		t.Error(l)
	}
	m.Delete("openai")
	if l, _ := m.List(); len(l) != 1 {
		t.Error(l)
	}
}

func TestOpenCodex(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header
		json.NewDecoder(r.Body).Decode(&got)
		got["_path"] = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")
	}))
	defer api.Close()
	store := &Memory{}
	store.Set("codex", Credential{Kind: KindOAuth, Access: "tok", AccountID: "acct-1", Expires: time.Now().Add(time.Hour), BaseURL: api.URL})
	p, err := Open(store, "codex")
	if err != nil {
		t.Fatal(err)
	}
	p.(*llm.OpenAI).BaseURL = api.URL
	r, err := llm.Collect(p.Stream(context.Background(), llm.Request{Model: "gpt-5.5", SessionID: "s1", MaxTokens: 100, Messages: []llm.Message{llm.Text(llm.User, "hi")}}), nil)
	if err != nil || r.Message.PlainText() != "hi" {
		t.Fatalf("%+v %v", r, err)
	}
	if got["_path"] != "/codex/responses" || got["instructions"] == nil || got["prompt_cache_key"] != "s1" || got["max_output_tokens"] != float64(100) || got["store"] != false {
		t.Errorf("body %v", got)
	}
	if hdr.Get("Authorization") != "Bearer tok" || hdr.Get("chatgpt-account-id") != "acct-1" || hdr.Get("originator") != "talos" || hdr.Get("session_id") != "s1" {
		t.Errorf("headers %v", hdr)
	}
	if _, err := Open(store, "custom/x"); err == nil {
		t.Error("opened a missing account")
	}
}

func TestJWTClaims(t *testing.T) {
	if jwtClaims("nope") != nil || jwtClaims("a.!!!.c") != nil {
		t.Error("bad tokens decoded")
	}
	if c := jwtClaims(accessToken("x")); c[codexClaim].(map[string]any)["chatgpt_account_id"] != "x" {
		t.Error(c)
	}
}

func freePort(t *testing.T) int {
	cb, err := listenCallback(0, "/", "")
	if err != nil {
		t.Fatal(err)
	}
	p := cb.port()
	cb.close()
	return p
}

func mustURL(t *testing.T, s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
