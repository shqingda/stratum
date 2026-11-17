package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
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
