package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
)

func TestCopilotDeviceLogin(t *testing.T) {
	var polls, exchanges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/login/device/code":
			if r.PostForm.Get("client_id") != copilotClientID {
				t.Errorf("form %v", r.PostForm)
			}
			io.WriteString(w, `{"device_code":"dc","user_code":"AB-12","verification_uri":"https://github.com/login/device","expires_in":60,"interval":0.01}`)
		case "/login/oauth/access_token":
			if polls.Add(1) == 1 {
				io.WriteString(w, `{"error":"authorization_pending"}`) // GitHub says so with a 200
				return
			}
			io.WriteString(w, `{"access_token":"gho_1","token_type":"bearer"}`)
		case "/copilot_internal/v2/token":
			if r.Header.Get("Authorization") != "token gho_1" {
				w.WriteHeader(401)
				return
			}
			n := exchanges.Add(1)
			io.WriteString(w, `{"token":"cop_`+string(rune('0'+n))+`","expires_at":4102444800,"endpoints":{"api":"https://api.business.githubcopilot.com"}}`)
		case "/user":
			io.WriteString(w, `{"login":"octo"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	g := &Copilot{GitHub: srv.URL, API: srv.URL}
	var shown string
	start := time.Now()
	cred, err := g.Login(context.Background(), Interaction{DeviceCode: func(code, uri string) { shown = code + " " + uri }})
	if err != nil {
		t.Fatal(err)
	}
	if shown != "AB-12 https://github.com/login/device" || polls.Load() != 2 {
		t.Errorf("shown %q polls %d", shown, polls.Load())
	}
	if cred.Access != "cop_1" || cred.Refresh != "gho_1" || cred.Method != "copilot" || cred.Email != "octo" ||
		cred.BaseURL != "https://api.business.githubcopilot.com" || cred.Expires.Year() != 2100 {
		t.Errorf("cred %+v", cred)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("slow polling")
	}
	fresh, err := g.Refresh(context.Background(), cred)
	if err != nil || fresh.Access != "cop_2" || fresh.Refresh != "gho_1" || fresh.Email != "octo" {
		t.Errorf("refresh %+v %v", fresh, err)
	}
	if h := CopilotHeaders(fresh); h.Get("Authorization") != "Bearer cop_2" || h.Get("Copilot-Integration-Id") == "" {
		t.Errorf("headers %v", h)
	}
}

func TestCopilotNoPlan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	g := &Copilot{API: srv.URL}
	if _, err := g.Refresh(context.Background(), Credential{Refresh: "gho"}); err == nil || !strings.Contains(err.Error(), "no Copilot plan") {
		t.Errorf("err %v", err)
	}
}

func TestCopilotDeclined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/device/code" {
			io.WriteString(w, `{"device_code":"dc","user_code":"AB","verification_uri":"https://github.com/login/device","interval":0.01}`)
			return
		}
		io.WriteString(w, `{"error":"access_denied"}`)
	}))
	defer srv.Close()
	g := &Copilot{GitHub: srv.URL}
	_, err := g.Login(context.Background(), Interaction{DeviceCode: func(string, string) {}})
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("err %v", err)
	}
}

func TestOpenCopilot(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(400)
	}))
	defer srv.Close()
	store := &Memory{}
	store.Set("copilot", Credential{Kind: KindOAuth, Method: "copilot", Access: "cop", Refresh: "gho",
		Expires: time.Now().Add(time.Hour), BaseURL: srv.URL})
	p, err := Open(store, "copilot")
	if err != nil {
		t.Fatal(err)
	}
	for range p.Stream(context.Background(), llm.Request{Model: "gpt-5", Messages: []llm.Message{llm.Text(llm.User, "hi")}}) {
	}
	if got.Get("Authorization") != "Bearer cop" || got.Get("Copilot-Integration-Id") != "vscode-chat" {
		t.Errorf("headers %v", got)
	}
}
