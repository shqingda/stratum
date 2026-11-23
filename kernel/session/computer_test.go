package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/tools"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// notesDesk is a desktop that shows only Notes, and does nothing.
type notesDesk struct{ computer.Desktop }

var notesApp = computer.App{Name: "Notes", ID: "com.apple.Notes"}

func (notesDesk) Missing(context.Context) []string { return nil }
func (notesDesk) Displays(context.Context) ([]computer.Display, error) {
	return []computer.Display{{ID: 1, Name: "Display", W: 800, H: 600, Main: true}}, nil
}
func (notesDesk) Screenshot(context.Context, computer.Display, int, int) ([]byte, error) {
	return []byte("jpeg"), nil
}
func (notesDesk) Click(context.Context, float64, float64, string, int) error { return nil }
func (notesDesk) FindApp(_ context.Context, name string) (computer.App, error) {
	if strings.EqualFold(name, "notes") {
		return notesApp, nil
	}
	return computer.App{}, errors.New("no such app")
}
func (notesDesk) AppAt(context.Context, float64, float64) (computer.Target, error) {
	return computer.Target{App: notesApp}, nil
}

// Granting apps asks even in full access and past a rule kept from when
// computer use asked for each action; what it does in them then runs
// unasked.
func TestComputerAppsAskInEveryMode(t *testing.T) {
	defer computer.Use(notesDesk{})()
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("a", "computer", map[string]any{"action": "request_access", "apps": []string{"notes"}, "reason": "写备忘录"})}},
		{Calls: []llm.ToolCall{llm.Call("b", "computer", map[string]any{"action": "click", "x": 10, "y": 10})}},
		{Text: "好"},
	}}
	m, s := changesManager(t, script)
	defer computer.Forget(s.ID)
	m.Tools = append(m.Tools, tools.Computer)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	full := "full"
	if _, err := m.Update(ctx, proto.SessionUpdate{SessionID: s.ID, Mode: &full}); err != nil {
		t.Fatal(err)
	}
	set, _ := m.Settings(ctx)
	set.Rules = append(set.Rules, proto.Rule{Tool: "computer"})
	if _, err := m.SetSettings(ctx, set); err != nil {
		t.Fatal(err)
	}
	var asked []proto.Approval
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventApprovalRequest:
			var a proto.Approval
			json.Unmarshal(e.Data, &a)
			asked = append(asked, a)
			go m.Answer(proto.ApprovalAnswer{SessionID: s.ID, ApprovalID: a.ID, Allow: true})
		case proto.EventTurnEnd:
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "做"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	if len(asked) != 1 || asked[0].Kind != "computer" || asked[0].Rule != nil ||
		len(asked[0].Apps) != 1 || asked[0].Apps[0] != (proto.AppAccess{Name: "Notes", Tier: "full"}) {
		t.Fatalf("asked %+v", asked)
	}
	var results []llm.ToolResult
	last := script.Requests[len(script.Requests)-1]
	for _, msg := range last.Messages {
		for _, part := range msg.Parts {
			if part.ToolResult != nil {
				results = append(results, *part.ToolResult)
			}
		}
	}
	if len(results) != 2 || results[0].IsError || !strings.Contains(results[0].Content, "Notes: tier full") ||
		results[1].IsError || !strings.HasPrefix(results[1].Content, "Click at 10, 10.") {
		t.Fatalf("results %+v", results)
	}
	if g := computer.Granted(s.ID); len(g) != 1 {
		t.Errorf("granted %+v", g)
	}
}
