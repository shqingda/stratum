package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
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
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	sent(proto.MethodSettings)
	a.C.Settings = &proto.Settings{Mode: "auto"}
	tt.Frame()
	if !tt.HasText("语言") || !tt.HasText("在程序坞图标上显示数字") || !tt.HasText("Stratum devel") {
		t.Fatalf("not the general page: %q", tt.Texts())
	}
	snapshot(t, tt, "settings")

	clickSwitch(t, tt, "出错")
	var s proto.Settings
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if len(s.Quiet) != 1 || s.Quiet[0] != "fail" {
		t.Fatalf("saved %+v", s)
	}
	if err := tt.Click("立即发送"); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if !s.EnterSteers || len(s.Quiet) != 1 {
		t.Fatalf("saved %+v", s)
	}
	tt.Frame()
	if !tt.HasText("⌘回车 加入队列。") {
		t.Errorf("the note does not follow the setting: %q", tt.Texts())
	}
	if err := tt.Click("从不"); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(sent(proto.MethodSetSettings), &s)
	if s.NotifyWhen != "never" || !s.EnterSteers {
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
	if err := tt.Click("账号与模型"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "settings-accounts")
	if !tt.HasText("Sign in with ChatGPT") {
		t.Fatal("accounts page did not list the sign-ins directly")
	}
	if err := tt.Click("退出登录 DeepSeek"); err != nil {
		t.Fatal(err)
	}
	var ref proto.AccountRef
	json.Unmarshal(sent(proto.MethodLogout), &ref)
	if ref.Account != "deepseek" {
		t.Fatalf("signed out of %+v", ref)
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
	if tt.HasText("搜索设置") || !tt.HasText("修复 Add 测试") {
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
	tt.ClickAt(r.X-15+572-1-12-18, r.Y+r.H/2)
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

// TestAppearanceAndAbout sets the theme, the text size and the detail on
// their own page, and shows the build.
func TestAppearanceAndAbout(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Settings = &proto.Settings{}
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1120, 800)
	a.openSettings()
	a.C.Settings = &proto.Settings{}
	tt.Frame()
	if err := tt.Click("外观"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"深色", "大", "全部"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	var s proto.Settings
	for done := false; !done; {
		select {
		case cmds := <-a.out:
			for _, cmd := range cmds {
				if cmd.Method == proto.MethodSetSettings {
					json.Unmarshal(cmd.Params, &s)
				}
			}
		default:
			done = true
		}
	}
	if s.Theme != "dark" || s.TextSize != "large" || s.Detail != "all" {
		t.Fatalf("saved %+v", s)
	}
	if z := textZoom(a.C.Settings.TextSize); z != 1.15 {
		t.Errorf("zoom %v for %q", z, a.C.Settings.TextSize)
	}
	if !tt.HasText("Stratum devel") {
		t.Fatalf("about footer: %q", tt.Texts())
	}
}

func TestRoleModelEffort(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Settings = &proto.Settings{
		Sub: &proto.ModelRef{Account: "chatgpt", Model: "gpt-6-luna", Effort: "high"},
	}
	a.settingsOpen, a.settingsPage = true, "models"
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1120, 800)
	tt.Frame()
	if !tt.HasText("gpt-6-luna · 高") {
		t.Fatalf("missing role effort label in %q", tt.Texts())
	}
	if err := tt.Click("gpt-6-luna · 高"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("低"); err != nil {
		t.Fatal(err)
	}
	var saved proto.Settings
	for done := false; !done; {
		select {
		case cmds := <-a.out:
			for _, cmd := range cmds {
				if cmd.Method == proto.MethodSetSettings {
					_ = json.Unmarshal(cmd.Params, &saved)
				}
			}
		default:
			done = true
		}
	}
	if saved.Sub == nil || saved.Sub.Effort != "low" {
		t.Fatalf("saved Sub = %+v", saved.Sub)
	}
}

func TestThemeStudioPersistence(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.C.Settings = &proto.Settings{}
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1120, 800)
	a.openSettings()
	a.settingsPage = "appearance"
	tt.SetDark(true)
	tt.Frame()

	for _, label := range []string{locales.ColorStyleVivid(), locales.PresetMalachite()} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("Click(%q): %v", label, err)
		}
		tt.Frame()
	}
	pbLabel, ok1 := tt.Find(locales.PureBlack())
	pbNote, ok2 := tt.Find(locales.PureBlackNote())
	if !ok1 || !ok2 {
		t.Fatalf("missing PureBlack row: label=%v note=%v", ok1, ok2)
	}
	tt.ClickAt(pbLabel.X-15+572-1-12-18, (pbLabel.Y+pbNote.Y+pbNote.H)/2)
	tt.Frame()

	var saved proto.Settings
	for done := false; !done; {
		select {
		case cmds := <-a.out:
			for _, cmd := range cmds {
				if cmd.Method == proto.MethodSetSettings {
					_ = json.Unmarshal(cmd.Params, &saved)
				}
			}
		default:
			done = true
		}
	}
	if saved.ColorStyle != "vivid" || saved.SeedColor != "#2E7D63" || !saved.PureBlack {
		t.Fatalf("saved theme studio settings = %+v, want vivid / #2E7D63 / PureBlack=true", saved)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if a.p == nil || a.p.Surface != (ui.RGB(0, 0, 0)) {
		t.Fatalf("expected PureBlack Surface=#000000, got %+v", a.p.Surface)
	}
}
