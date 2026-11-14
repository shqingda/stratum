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
	a.settingsOpen, a.settingsPage = true, "models"
	a.C.Accounts = []proto.Account{
		{ID: "codex", Provider: "codex", Name: "ChatGPT", Model: "gpt-6.1-sol", Subscription: true},
		{ID: "xai", Provider: "xai", Name: "xAI", Model: "grok-4.7", Subscription: true},
		{ID: "openrouter", Provider: "openrouter", Name: "OpenRouter", Model: "auto"},
	}
	a.C.Settings = &proto.Settings{Account: "codex", Model: "gpt-6.1-sol", Effort: "high",
		Light: &proto.ModelRef{Account: "xai", Model: "grok-4.3", Effort: "low"}}
	tt := ui.NewTester(a.View, 1200, 900)
	tt.Frame()
	for _, s := range []string{"后台任务", "子任务", "与默认模型相同", "grok-4.3", "按 xAI、OpenRouter 的顺序切换。"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown", s)
		}
	}
	snapshot(t, tt, "settings-roles")
	clickSwitch(t, tt, "切换到其他账号")
	cmd := sent(a, proto.MethodSetSettings)
	var s proto.Settings
	if cmd == nil || json.Unmarshal(cmd.Params, &s) != nil || !s.Fallback || s.Light == nil || s.Light.Model != "grok-4.3" {
		t.Fatalf("%+v", s)
	}
}
