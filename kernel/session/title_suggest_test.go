package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestSessionRetitle(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "fixed the failing test"},
		{Text: "Fix Add unit test"},
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

	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "fix Add test"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("main turn did not end")
	}

	updated, err := m.Retitle(ctx, s.ID)
	if err != nil {
		t.Fatalf("Retitle: %v", err)
	}
	if updated.Title != "Fix Add unit test" {
		t.Fatalf("updated.Title = %q, want %q", updated.Title, "Fix Add unit test")
	}
	stored, err := m.Store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Fix Add unit test" {
		t.Fatalf("stored.Title = %q, want %q", stored.Title, "Fix Add unit test")
	}
}

func TestPromptSuggestionOnTurnEnd(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "I updated add.go"},
		{Text: "run go test"},
	}}
	m, s := changesManager(t, script)
	m.SuggestInTests = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gotSug := make(chan string, 1)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventSuggestion {
			var sug proto.Suggestion
			if json.Unmarshal(e.Data, &sug) == nil {
				gotSug <- sug.Text
			}
		}
	})()

	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: "fix add.go"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-gotSug:
		if got != "run go test" {
			t.Fatalf("suggestion = %q, want %q", got, "run go test")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for EventSuggestion")
	}
	m.wg.Wait()
}

func TestRewindConversationAllowedWhileOtherSessionRuns(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "first answer"}}}
	m, s1 := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd && e.SessionID == s1.ID {
			ended <- struct{}{}
		}
	})()

	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s1.ID, Text: "first question"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("s1 turn did not end")
	}

	// Simulate another session (e.g., notes waiting for approval) with an active run.
	m.mu.Lock()
	if m.runs == nil {
		m.runs = map[string]*run{}
	}
	m.runs["other-session"] = &run{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.runs, "other-session")
		m.mu.Unlock()
	}()

	evs, err := m.Store.Events(ctx, s1.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var turnSeq int64
	for _, ev := range evs {
		if ev.Type == proto.EventTurnStart {
			turnSeq = ev.Seq
			break
		}
	}
	if turnSeq == 0 {
		t.Fatal("missing EventTurnStart")
	}
	if _, err := m.Rewind(ctx, proto.Rewind{SessionID: s1.ID, TurnSeq: turnSeq, Action: "conversation"}); err != nil {
		t.Fatalf("conversation rewind blocked by another session: %v", err)
	}
}
