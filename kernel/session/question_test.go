package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestAskUser(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("q", "ask_user", map[string]any{"question": "用哪种方案？", "options": []string{"A", "B"}})}},
		{Text: "好，按 B。"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	asked := make(chan proto.Approval, 1)
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventApprovalRequest:
			var a proto.Approval
			json.Unmarshal(e.Data, &a)
			asked <- a
		case proto.EventTurnEnd:
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "改一下"}, ""); err != nil {
		t.Fatal(err)
	}
	var a proto.Approval
	select {
	case a = <-asked:
	case <-ctx.Done():
		t.Fatal("no question")
	}
	if a.Kind != "question" || a.Question != "用哪种方案？" || len(a.Options) != 2 {
		t.Fatalf("question %+v", a)
	}
	if err := m.Answer(proto.ApprovalAnswer{SessionID: s.ID, ApprovalID: a.ID, Allow: true, Text: "B"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	for _, msg := range script.Requests[1].Messages {
		for _, part := range msg.Parts {
			if r := part.ToolResult; r != nil && r.CallID == "q" {
				if r.Content != "The user answered: B" {
					t.Fatalf("result %+v", r)
				}
				return
			}
		}
	}
	t.Fatal("no answer reached the model")
}
