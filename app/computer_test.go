package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

func TestComputerUseIsASettingAndAsks(t *testing.T) {
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
