package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestRewindConversation(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "one"}, {Text: "two"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == s.ID {
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
	evs, err := m.Store.Events(ctx, s.ID, 0, 0, proto.EventTurnStart)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%d events, %v", len(evs), err)
	}
	out, err := m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: evs[1].Seq, Action: "conversation"})
	if err != nil || out.Prompt != "second" {
		t.Fatalf("%+v %v", out, err)
	}
	h, err := m.history(ctx, s.ID)
	if err != nil || len(h) != 2 || h[0].PlainText() != "first" || h[1].PlainText() != "one" {
		t.Fatalf("%v %v", h, err)
	}
	snap, err := m.Get(ctx, s.ID)
	if err != nil || snap.Draft != "second" {
		t.Fatalf("draft %q %v", snap.Draft, err)
	}
	if _, err := m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: evs[1].Seq, Action: "nope"}); err == nil {
		t.Fatal("accepted a bad action")
	}
}

func TestRewindCodeAndBoth(t *testing.T) {
	m, s := changesManager(t, &llm.Script{})
	q := recordTestChange(t, m, s)
	ctx := context.Background()
	out, err := m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: q.TurnSeq, Action: "code"})
	if err != nil || len(out.Paths) != 1 || out.Prompt != "" {
		t.Fatalf("%+v %v", out, err)
	}
	b, err := os.ReadFile(q.Path)
	if err != nil || string(b) != "before" {
		t.Fatalf("%q %v", b, err)
	}
	evs, err := m.Store.Events(ctx, s.ID, 0, 0, proto.EventTurnStart)
	if err != nil || len(evs) != 1 {
		t.Fatalf("code restore dropped the prompt: %d %v", len(evs), err)
	}

	// Put the edit back, then restore files and the conversation together.
	if _, err := m.changes.Redo(ctx, s.ID, q.TurnSeq, ""); err != nil {
		t.Fatal(err)
	}
	out, err = m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: q.TurnSeq, Action: "both"})
	if err != nil || out.Prompt != "recorded turn" {
		t.Fatalf("%+v %v", out, err)
	}
	b, err = os.ReadFile(q.Path)
	if err != nil || string(b) != "before" {
		t.Fatalf("both %q %v", b, err)
	}
	left, err := m.Store.Events(ctx, s.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		if e.Seq >= q.TurnSeq && e.Type != proto.EventChangesReverted {
			t.Fatalf("kept %s at %d", e.Type, e.Seq)
		}
	}
}

func TestRewindSummarize(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "one"},
		{Text: "two"},
		{Text: "SUMMARY: from turn 2"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == s.ID {
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
	evs, err := m.Store.Events(ctx, s.ID, 0, 0, proto.EventTurnStart)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%d events, %v", len(evs), err)
	}
	out, err := m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: evs[1].Seq, Action: "summarize"})
	if err != nil || out.Prompt != "" || out.Action != "summarize" {
		t.Fatalf("%+v %v", out, err)
	}
	// Request 2 should only have been sent turn 2's messages plus compactPrompt.
	if len(script.Requests) != 3 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	sumReq := script.Requests[2].Messages
	if len(sumReq) != 3 || sumReq[0].PlainText() != "second" || sumReq[1].PlainText() != "two" {
		t.Fatalf("summarize sent %+v", sumReq)
	}
	h, err := m.history(ctx, s.ID)
	if err != nil || len(h) != 4 || h[0].PlainText() != "first" || h[1].PlainText() != "one" || !strings.Contains(h[2].PlainText(), "SUMMARY: from turn 2") {
		t.Fatalf("history %+v %v", h, err)
	}
	snap, err := m.Get(ctx, s.ID)
	if err != nil || snap.Draft != "" {
		t.Fatalf("draft %q %v", snap.Draft, err)
	}
}

func TestRewindSummarizeBefore(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "one"},
		{Text: "two"},
		{Text: "three"},
		{Text: "SUMMARY: turn 1 done"},
	}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == s.ID {
			ended <- struct{}{}
		}
	})()
	for _, text := range []string{"first", "second", "third"} {
		if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: text}, ""); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ended:
		case <-ctx.Done():
			t.Fatal("the turn did not end")
		}
	}
	evs, err := m.Store.Events(ctx, s.ID, 0, 0, proto.EventTurnStart)
	if err != nil || len(evs) != 3 {
		t.Fatalf("%d events, %v", len(evs), err)
	}
	out, err := m.Rewind(ctx, proto.Rewind{SessionID: s.ID, TurnSeq: evs[1].Seq, Action: "summarize_before"})
	if err != nil || out.Prompt != "" || out.Action != "summarize_before" {
		t.Fatalf("%+v %v", out, err)
	}
	// Request 3 should only summarize turn 1.
	sumReq := script.Requests[3].Messages
	if len(sumReq) != 3 || sumReq[0].PlainText() != "first" || sumReq[1].PlainText() != "one" {
		t.Fatalf("summarize_before sent %+v", sumReq)
	}
	h, err := m.history(ctx, s.ID)
	if err != nil || len(h) != 6 || !strings.Contains(h[0].PlainText(), "SUMMARY: turn 1 done") || h[2].PlainText() != "second" || h[4].PlainText() != "third" {
		t.Fatalf("history %+v %v", h, err)
	}
}

func TestForkSession(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "one"}, {Text: "two"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 4)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "hello"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}

	forked, err := m.Fork(ctx, proto.SessionRef{SessionID: s.ID})
	if err != nil {
		t.Fatal(err)
	}
	if forked.ID == s.ID || forked.ParentID != s.ID || !strings.HasSuffix(forked.Title, "(fork)") {
		t.Fatalf("forked = %+v", forked)
	}
	h, err := m.history(ctx, forked.ID)
	if err != nil || len(h) != 2 || h[0].PlainText() != "hello" || h[1].PlainText() != "one" {
		t.Fatalf("forked history = %+v %v", h, err)
	}
}
