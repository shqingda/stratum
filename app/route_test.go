package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

func TestModelRoles(t *testing.T) {
	a := working()
	a.out = make(chan []proto.Command, 16)
	a.settingsOpen = true
	a.C.Accounts = []proto.Account{
		{ID: "codex", Provider: "codex", Name: "ChatGPT", Model: "gpt-6.1-sol", Subscription: true},
		{ID: "xai", Provider: "xai", Name: "xAI", Model: "grok-4.7", Subscription: true},
		{ID: "openrouter", Provider: "openrouter", Name: "OpenRouter", Model: "auto"},
	}
	a.C.Settings = &proto.Settings{Account: "codex", Model: "gpt-6.1-sol", Effort: "high",
		Light: &proto.ModelRef{Account: "xai", Model: "grok-4.3", Effort: "low"}}
	tt := ui.NewTester(a.View, 1200, 900)
	tt.Frame()
	for _, s := range []string{"模型分工", "子 agent", "跟主 agent 一样", "grok-4.3", "先 xAI，再 OpenRouter；换过会在对话里提示"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	snapshot(t, tt, "settings-roles")
	if err := tt.Click("换备用账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cmd := sent(a, proto.MethodSetSettings)
	var s proto.Settings
	if cmd == nil || json.Unmarshal(cmd.Params, &s) != nil || !s.Fallback || s.Light == nil || s.Light.Model != "grok-4.3" {
		t.Fatalf("%+v", s)
	}
}
