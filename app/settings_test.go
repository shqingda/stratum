package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/i18n"
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
	if !tt.HasText("语言") || !tt.HasText("在程序坞图标上显示数字") || tt.HasText("定时任务") {
		t.Fatalf("not the general page: %q", tt.Texts())
	}
	snapshot(t, tt, "settings")

	clickSwitch(t, tt, "出错")
	var s proto.Settings
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if len(s.Quiet) != 1 || s.Quiet[0] != "fail" {
		t.Fatalf("saved %+v", s)
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if s.Theme != "dark" || len(s.Quiet) != 1 {
		t.Fatalf("saved %+v", s)
	}
	if err := tt.Click("语言"); err == nil {
		tt.Frame()
	}
	if err := tt.Click("跟随系统"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("English"); err != nil {
		t.Fatal(err)
	}
	if a.locale.Chosen() != "en" {
		t.Fatalf("language %q", a.locale.Chosen())
	}

	a.settingsQuery = "沙盒"
	tt.Frame()
	if tt.HasText("快捷键") || !tt.HasText("权限") {
		t.Fatalf("search: %q", tt.Texts())
	}
	a.settingsQuery = ""
	tt.Frame()
	if err := tt.Click("权限"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "settings-permissions")
	if err := tt.Click("完全访问"); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if s.Mode != "full" || a.C.Settings.Mode != "full" {
		t.Fatalf("saved %+v", s)
	}
	if err := tt.Click("账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "settings-accounts")
	if err := tt.Click("DeepSeek 选项"); err != nil {
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
	if err := tt.Click("添加账号…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Sign in with ChatGPT") {
		t.Fatal("添加账号 did not show the sign-ins")
	}
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "settings-add-dark")
	if err := tt.Click("快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("本会话都允许") {
		t.Fatalf("shortcuts: %q", tt.Texts())
	}
	snapshot(t, tt, "settings-shortcuts-dark")
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

// clickSwitch clicks the switch at the end of the settings row labeled s:
// a row is as wide as the page's column, 540 points, and its label starts
// 12 points into it.
func clickSwitch(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("no row %q", s)
	}
	tt.ClickAt(r.X-12+540-10-15, r.Y+r.H/2)
	tt.Frame()
}

func TestSettingsEnglish(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "en" })
	defer i18n.SetLocaleFunc(chinese)
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Settings = &proto.Settings{Mode: "auto"}
	a.settingsOpen, a.settingsPage = true, "models"
	tt := ui.NewTester(a.View, 1120, 800)
	tt.SetDark(true)
	tt.Frame()
	for _, s := range []string{"Models", "Default model", "Background tasks", "Same as default", "General"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q in %q", s, tt.Texts())
		}
	}
	snapshot(t, tt, "settings-en-dark")
	a.settingsQuery = "沙盒"
	tt.Frame()
	if !tt.HasText("Permissions") || tt.HasText("Keyboard") {
		t.Fatalf("a Chinese search in English: %q", tt.Texts())
	}
}
