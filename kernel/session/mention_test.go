package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

func TestAtWords(t *testing.T) {
	got := atWords("mail a@b.com see @session:abc and @notes:note://today")
	if strings.Join(got, ",") != "session:abc,notes:note://today" {
		t.Fatalf("%v", got)
	}
}

func TestExpandSessionMention(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "it is 4040"}, {Text: "done"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	other, err := m.Create(ctx, proto.SessionCreate{Cwd: s.Cwd, Title: "port notes", Account: "script", Model: "script", Mode: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{}, 8)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: other.ID, Text: "the port is 4040"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the other session did not finish")
	}
	ask := "what port? @session:" + other.ID
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: ask}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the mention turn did not finish")
	}
	if len(script.Requests) != 2 {
		t.Fatalf("%d requests", len(script.Requests))
	}
	got := script.Requests[1].Messages[len(script.Requests[1].Messages)-1].PlainText()
	for _, want := range []string{ask, `title="port notes"`, "user: the port is 4040", "assistant: it is 4040", "</session>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Count(got, "<session ") != 1 {
		t.Fatalf("attached %d times: %q", strings.Count(got, "<session "), got)
	}
	// The current session is not attached, and a missing one is left as text.
	x, err := m.Store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain := "see @session:" + s.ID + " and @session:missing"
	if got := m.expandMentions(ctx, x, plain); got != plain {
		t.Fatalf("expanded %q", got)
	}
}

func TestTailRunes(t *testing.T) {
	s := strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10)
	got := tailRunes(s, 12)
	if got != "…\n"+strings.Repeat("b", 10) {
		t.Fatalf("%q", got)
	}
	if tailRunes("short", 12) != "short" {
		t.Fatal(tailRunes("short", 12))
	}
}
