package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// routed is a manager whose "script" account is main and whose "xai"
// account is backup.
func routed(t *testing.T, main, backup *llm.Script, set proto.Settings) (*Manager, proto.Session, chan proto.Event) {
	m, s := changesManager(t, main)
	m.Open = func(id string) (llm.Provider, error) {
		if id == "xai" {
			return backup, nil
		}
		return main, nil
	}
	m.Accounts = func() []string { return []string{"script", "xai"} }
	if _, err := m.SetSettings(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	events := make(chan proto.Event, 64)
	t.Cleanup(m.Subscribe(func(e proto.Event) { events <- e }))
	return m, s, events
}

func waitFor(t *testing.T, events chan proto.Event, typ string) proto.Event {
	t.Helper()
	for {
		select {
		case e := <-events:
			if e.Type == typ {
				return e
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s", typ)
		}
	}
}

func TestFallback(t *testing.T) {
	main := &llm.Script{Turns: []llm.Turn{{Err: &llm.Error{Status: 429, Message: "usage limit reached"}}}}
	backup := &llm.Script{Turns: []llm.Turn{{Text: "换了账号也能答"}}}
	m, s, events := routed(t, main, backup, proto.Settings{Fallback: true})
	if _, err := m.Prompt(context.Background(), proto.Prompt{SessionID: s.ID, Text: "hi"}, ""); err != nil {
		t.Fatal(err)
	}
	var f proto.Fallback
	json.Unmarshal(waitFor(t, events, proto.EventFallback).Data, &f)
	if f.From != "script/script" || f.To != "xai/"+llm.DefaultModel("xai") || !strings.Contains(f.Error, "usage limit") {
		t.Fatalf("%+v", f)
	}
	var end proto.TurnEnd
	json.Unmarshal(waitFor(t, events, proto.EventTurnEnd).Data, &end)
	if end.Error != "" || backup.Requests[0].Model != llm.DefaultModel("xai") {
		t.Fatalf("%+v %q", end, backup.Requests[0].Model)
	}
}

func TestNoFallbackWhenOff(t *testing.T) {
	main := &llm.Script{Turns: []llm.Turn{{Err: &llm.Error{Status: 402, Message: "quota"}}}}
	backup := &llm.Script{}
	m, s, events := routed(t, main, backup, proto.Settings{})
	m.Prompt(context.Background(), proto.Prompt{SessionID: s.ID, Text: "hi"}, "")
	var end proto.TurnEnd
	json.Unmarshal(waitFor(t, events, proto.EventTurnEnd).Data, &end)
	if end.Error == "" || len(backup.Requests) != 0 {
		t.Fatalf("%+v %d", end, len(backup.Requests))
	}
}

func TestRoles(t *testing.T) {
	main := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("t1", "task", map[string]any{"description": "看看", "prompt": "look"})}},
		{Text: "好了"},
	}}
	backup := &llm.Script{Turns: []llm.Turn{{Text: "修复 Add 测试"}, {Text: "report"}}}
	m, s, events := routed(t, main, backup, proto.Settings{
		Sub:   &proto.ModelRef{Account: "xai", Model: "grok-sub", Effort: "low"},
		Light: &proto.ModelRef{Account: "xai", Model: "grok-light"},
	})
	s, err := m.Create(context.Background(), proto.SessionCreate{Cwd: s.Cwd, Account: "script", Model: "script", Mode: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Prompt(context.Background(), proto.Prompt{SessionID: s.ID, Text: "Add 的测试挂了"}, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, events, proto.EventTurnEnd)
	m.wg.Wait()
	models := map[string]bool{}
	for _, r := range backup.Requests {
		models[r.Model] = true
	}
	if !models["grok-sub"] || !models["grok-light"] {
		t.Fatalf("backup got %v", models)
	}
	x, _ := m.Store.GetSession(context.Background(), s.ID)
	if x.Title != "修复 Add 测试" && x.Title != "report" {
		t.Fatalf("title %q", x.Title)
	}
}
