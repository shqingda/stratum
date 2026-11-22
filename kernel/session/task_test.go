package session

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestTask(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("t1", "task", map[string]any{"description": "找 Add", "prompt": "Where is Add defined?"})}},
		{Calls: []llm.ToolCall{llm.Call("s1", "list_dir", map[string]any{"path": "."})}},
		{Text: "Add is in add.go."},
		{Text: "在 add.go。"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	steps := make(chan proto.TaskStep, 4)
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventTaskStep:
			var st proto.TaskStep
			json.Unmarshal(e.Data, &st)
			steps <- st
		case proto.EventTurnEnd:
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "Add 在哪"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	sub := script.Requests[1]
	if !strings.Contains(sub.System, "sub-agent") || len(sub.Messages) != 1 || sub.Messages[0].PlainText() != "Where is Add defined?" {
		t.Fatalf("sub-agent request %+v", sub)
	}
	if slices.ContainsFunc(sub.Tools, func(x llm.Tool) bool { return x.Name == "task" || x.Name == "ask_user" }) {
		t.Fatal("the sub-agent can start sub-agents or ask")
	}
	last := script.Requests[3].Messages
	if r := last[len(last)-1].Parts[0].ToolResult; r == nil || r.Content != "Add is in add.go." {
		t.Fatalf("report %+v", last[len(last)-1])
	}
	if st := <-steps; st.CallID != "t1" || st.Call.Name != "list_dir" {
		t.Fatalf("step %+v", st)
	}
	h, _ := m.history(ctx, s.ID)
	if len(h) != 4 {
		t.Fatalf("the sub-agent's messages were recorded: %d messages", len(h))
	}
}
