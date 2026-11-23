package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestSideAskReadOnlyAndEphemeral(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "main answer"},
		{Text: "side answer"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()

	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "main question"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("main turn did not end")
	}

	beforeMsgs, err := m.Store.Events(ctx, s.ID, 0, 0, store.MessageEvent)
	if err != nil {
		t.Fatal(err)
	}

	res, err := m.SideAsk(ctx, proto.SideAsk{SessionID: s.ID, Text: "quick side question"})
	if err != nil {
		t.Fatalf("SideAsk: %v", err)
	}
	if res.Answer != "side answer" {
		t.Fatalf("SideAsk Answer = %q, want %q", res.Answer, "side answer")
	}

	if len(script.Requests) != 2 {
		t.Fatalf("expected 2 LLM requests, got %d", len(script.Requests))
	}
	req := script.Requests[1]
	if len(req.Tools) != 0 {
		t.Fatalf("expected 0 tools for SideAsk, got %d", len(req.Tools))
	}
	if len(req.Messages) < 3 || req.Messages[len(req.Messages)-1].PlainText() != "quick side question" {
		t.Fatalf("expected history + side question, got %+v", req.Messages)
	}

	afterMsgs, err := m.Store.Events(ctx, s.ID, 0, 0, store.MessageEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterMsgs) != len(beforeMsgs) {
		t.Fatalf("SideAsk persisted message events: before=%d after=%d", len(beforeMsgs), len(afterMsgs))
	}
	for _, ev := range afterMsgs {
		s := string(ev.Data)
		if strings.Contains(s, "quick side question") || strings.Contains(s, "side answer") {
			t.Fatalf("SideAsk leaked into persisted message event: %s", s)
		}
	}
}
