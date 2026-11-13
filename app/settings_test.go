package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestSettings(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Accounts = []proto.Account{
		{ID: "codex", Provider: "codex", Name: "ChatGPT (Codex)", Email: "me@example.test", Subscription: true, Model: "gpt-6-luna"},
		{ID: "deepseek", Provider: "deepseek", Name: "DeepSeek", Model: "deepseek-chat"},
	}
	a.out = make(chan []proto.Command, 16)
	sent := func(method string) json.RawMessage {
		t.Helper()
		for {
			select {
			case cmds := <-a.out:
				for _, cmd := range cmds {
					if cmd.Method == method {
						return cmd.Params
					}
				}
			default:
				t.Fatalf("no %s sent", method)
			}
		}
	}
	tt := ui.NewTester(a.View, 1120, 800)
	tt.Frame()
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	sent(proto.MethodSettings)
	a.C.Settings = &proto.Settings{Mode: "auto"}
	tt.Frame()
	if !tt.HasText("账号与模型") || !tt.HasText("DeepSeek") || tt.HasText("定时任务") {
		t.Fatalf("not the settings page: %q", tt.Texts())
	}
	snapshot(t, tt, "settings")

	if err := tt.Click("完全"); err != nil {
		t.Fatal(err)
	}
	var s proto.Settings
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if s.Mode != "full" || a.C.Settings.Mode != "full" {
		t.Fatalf("saved %+v", s)
	}
	if err := tt.Click("DeepSeek 的操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("退出登录"); err != nil {
		t.Fatal(err)
	}
	var ref proto.AccountRef
	json.Unmarshal(sent(proto.MethodLogout), &ref)
	if ref.Account != "deepseek" {
		t.Fatalf("signed out of %+v", ref)
	}
	if err := tt.Click("添加账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Sign in with ChatGPT") {
		t.Fatal("添加账号 did not show the sign-ins")
	}
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "settings-add-dark")
	if err := tt.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("定时任务") {
		t.Fatal("closing settings did not go back")
	}
}

func TestCmdNumbers(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if tt.HasText("⌘1") {
		t.Fatal("numbers without Cmd held")
	}
	tt.HoldModifiers(ui.Cmd)
	tt.Frame()
	if !tt.HasText("⌘1") || !tt.HasText("⌘5") || tt.HasText("2 分") {
		t.Fatalf("with Cmd held: %q", tt.Texts())
	}
	snapshot(t, tt, "cmd-numbers")
	tt.HoldModifiers(0)
	tt.Frame()
	if tt.HasText("⌘1") {
		t.Fatal("numbers stay after Cmd is let go")
	}
}
