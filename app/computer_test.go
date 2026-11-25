package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

func TestComputerUseIsHiddenUntilTurnedOn(t *testing.T) {
	t.Setenv("STRATUM_COMPUTER_USE", "")
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	tt := ui.NewTester(a.View, 1120, 800)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	a.C.Settings = &proto.Settings{Mode: "auto", ComputerUse: true}
	tt.Frame()
	if !tt.HasText("按回车") {
		t.Fatalf("not on the general page: %q", tt.Texts())
	}
	if tt.HasText("电脑操控") || tt.HasText("让代理查看屏幕并使用鼠标和键盘") {
		t.Error("General shows computer use without STRATUM_COMPUTER_USE")
	}
}

func TestComputerUseIsASettingAndAsks(t *testing.T) {
	t.Setenv("STRATUM_COMPUTER_USE", "1")
	a := working()
	a.C.Busy, a.C.Approvals = false, nil
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1120, 800)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	a.C.Settings = &proto.Settings{Mode: "auto"}
	tt.Frame()
	for _, want := range []string{"电脑操控", "让代理查看屏幕并使用鼠标和键盘"} {
		if !tt.HasText(want) {
			t.Fatalf("the general page lacks %q: %q", want, tt.Texts())
		}
	}
	clickSwitch(t, tt, "让代理查看屏幕并使用鼠标和键盘")
	var saved proto.Settings
	for found := false; !found; {
		select {
		case cmds := <-a.out:
			for _, cmd := range cmds {
				if cmd.Method == proto.MethodSetSettings {
					json.Unmarshal(cmd.Params, &saved)
					found = true
				}
			}
		default:
			t.Fatal("the switch saved nothing")
		}
	}
	if !saved.ComputerUse {
		t.Fatalf("saved %+v", saved)
	}

	b := working()
	args, _ := json.Marshal(map[string]any{"action": "click", "x": 640, "y": 400})
	b.C.Approvals = []proto.Approval{{ID: "c", Tool: "computer", Kind: "computer", Args: args, Reason: "It sees your screen and uses your mouse and keyboard.", Rule: &proto.Rule{Tool: "computer"}}}
	tb := ui.NewTester(b.View, 1440, 880)
	tb.Frame()
	for _, want := range []string{"电脑操控", "要查看屏幕，并使用鼠标和键盘"} {
		if !tb.HasText(want) {
			t.Errorf("the approval lacks %q: %q", want, tb.Texts())
		}
	}
	if tb.HasText("$") {
		t.Error("the approval shows a shell prompt for a click")
	}
}

// Asked for apps, the card lists each with what the agent may do there,
// says esc stops it, and offers no rule: each app is granted on its own.
func TestComputerAppsCard(t *testing.T) {
	a := working()
	a.C.Queued = nil
	a.out = make(chan []proto.Command, 16)
	args, _ := json.Marshal(map[string]any{"action": "request_access", "apps": []string{"Notes", "Chromium", "Terminal"}, "reason": "把会议纪要写进备忘录"})
	a.C.Approvals = []proto.Approval{{ID: "apps", Tool: "computer", Kind: "computer", Args: args,
		Reason: "It asks to see and use these apps until Stratum quits.",
		Apps:   []proto.AppAccess{{Name: "Notes", Tier: "full"}, {Name: "Chromium", Tier: "read"}, {Name: "Terminal", Tier: "click"}}}}
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	for _, want := range []string{
		"电脑操控", "把会议纪要写进备忘录",
		"Notes", "完全控制", "能点按、输入和按键",
		"Chromium", "只能查看", "能在截图中看到，不能点按或输入",
		"Terminal", "只能点按", "能点按和滚动，不能输入或按键",
		"要在 Stratum 退出前查看和使用这些应用", "代理使用电脑时，按 esc 可以随时停止",
	} {
		if !tt.HasText(want) {
			t.Errorf("the card lacks %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("始终允许") || tt.HasText("request_access") {
		t.Errorf("the card offers a rule or shows the arguments: %q", tt.Texts())
	}
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	var ans proto.ApprovalAnswer
	task8Command(t, a, proto.MethodApprovalRespond, &ans)
	if !ans.Allow || ans.Always || ans.Forever || ans.ApprovalID != "apps" {
		t.Errorf("answer = %+v", ans)
	}
}

func TestControlIndicator(t *testing.T) {
	var did []string
	ci := &ControlIndicator{
		show:      func() { did = append(did, "show") },
		hide:      func() { did = append(did, "hide") },
		bindEsc:   func(func()) error { did = append(did, "bind"); return nil },
		unbindEsc: func() { did = append(did, "unbind") },
	}
	stopped := make(chan bool, 1)
	ci.Hide() // not shown: nothing to do
	release := ci.Hold()
	release() // not shown: esc is not taken
	ci.Show(func() { stopped <- true })
	r1 := ci.Hold()
	r2 := ci.Hold()
	r1()
	r1() // twice is once
	r2()
	ci.pressed()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("esc did not stop the turn")
	}
	ci.Hide()
	ci.Hide()
	ci.pressed() // after it hid, esc stops nothing
	want := "show bind unbind bind hide unbind"
	if got := strings.Join(did, " "); got != want {
		t.Errorf("did %q, want %q", got, want)
	}
	select {
	case <-stopped:
		t.Error("esc stopped a turn after the pill hid")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestControlPill(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.controlPill, pillW, pillH)
	tt.Frame()
	for _, want := range []string{"Stratum 正在使用你的电脑", "esc", "停止"} {
		if !tt.HasText(want) {
			t.Errorf("the pill lacks %q: %q", want, tt.Texts())
		}
	}
}
