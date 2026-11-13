package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestOnboarding(t *testing.T) {
	a := working()
	a.C.Entries, a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, nil, false
	a.C.Accounts = []proto.Account{}
	a.out = make(chan []proto.Command, 8)
	sent := func() proto.Login {
		t.Helper()
		for {
			select {
			case cmds := <-a.out:
				for _, cmd := range cmds {
					if cmd.Method == proto.MethodLogin {
						var q proto.Login
						json.Unmarshal(cmd.Params, &q)
						return q
					}
				}
			default:
				t.Fatal("no auth.login sent")
			}
		}
	}
	tt := ui.NewTester(a.View, 760, 700)
	tt.SetDark(true)
	tt.Frame()
	if !tt.HasText("连接一个模型") || tt.HasText("新会话") {
		t.Fatalf("not the sign-in page: %q", tt.Texts())
	}
	snapshot(t, tt, "login")

	if err := tt.Click("Sign in with ChatGPT"); err != nil {
		t.Fatal(err)
	}
	if q := sent(); q.Provider != "codex" || q.Method != "codex" || q.Device {
		t.Fatalf("sent %+v", q)
	}
	a.C.SignIn.LoginProgress = proto.LoginProgress{URL: "https://auth.example.test"}
	tt.Frame()
	if err := tt.Click("浏览器打不开？用设备码"); err != nil {
		t.Fatal(err)
	}
	if q := sent(); !q.Device {
		t.Fatalf("sent %+v", q)
	}
	a.C.SignIn.LoginProgress = proto.LoginProgress{Code: "K7QM-4X2P", CodeURL: "https://auth.openai.com/codex/device"}
	tt.Frame()
	if !tt.HasText("复制代码") || !tt.HasText("Q") {
		t.Fatalf("no code: %q", tt.Texts())
	}
	snapshot(t, tt, "login-code")

	if err := tt.Click("API key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.keyOpen {
		t.Fatal("API key did not open its form")
	}
	a.key = "sk-test"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if q := sent(); q.Provider != "openai" || q.Key != "sk-test" || a.key != "" {
		t.Fatalf("sent %+v, key left %q", q, a.key)
	}
	tt.Frame()
	snapshot(t, tt, "login-key")

	a.C.Accounts = []proto.Account{{ID: "anthropic"}}
	tt.Frame()
	if tt.HasText("连接一个模型") {
		t.Fatal("still the sign-in page with an account")
	}
}
