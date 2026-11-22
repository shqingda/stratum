package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestCompaction(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: strings.Repeat("long answer ", 100)},
		{Text: "SUMMARY: fixed Add"},
		{Text: "second answer"},
	}}
	m, s := changesManager(t, script)
	m.CompactAt = 200
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 2)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()
	for _, text := range []string{"first", "second"} {
		if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: text}, ""); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ended:
		case <-ctx.Done():
			t.Fatal("the turn did not end")
		}
	}
	if len(script.Requests) != 3 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	ask := script.Requests[1].Messages
	if last := ask[len(ask)-1].PlainText(); last != compactPrompt {
		t.Fatalf("the summary request ends with %q", last)
	}
	next := script.Requests[2].Messages
	if len(next) != 3 || !strings.Contains(next[0].PlainText(), "SUMMARY: fixed Add") || next[2].PlainText() != "second" {
		t.Fatalf("after compaction the model got %d messages: %+v", len(next), next)
	}
	// The summary stays: a later turn starts from it too.
	h, err := m.history(ctx, s.ID)
	if err != nil || len(h) != 4 || !strings.Contains(h[0].PlainText(), "SUMMARY") {
		t.Fatalf("history %+v %v", h, err)
	}
}

func TestCompactNow(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "short answer"},
		{Text: "SUMMARY: asked once"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 2)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()
	for _, p := range []proto.Prompt{{SessionID: s.ID, Text: "first"}, {SessionID: s.ID, Text: "/compact", Compact: true}} {
		if _, err := m.Prompt(ctx, p, ""); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ended:
		case <-ctx.Done():
			t.Fatal("the turn did not end")
		}
	}
	// The second turn only summarized: no agent request follows it.
	if len(script.Requests) != 2 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	h, err := m.history(ctx, s.ID)
	if err != nil || len(h) == 0 || !strings.Contains(h[0].PlainText(), "SUMMARY: asked once") {
		t.Fatalf("history %+v %v", h, err)
	}
}
