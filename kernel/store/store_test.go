package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shqingda/talos/llm"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "talos.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, Session{ID: "s1", Cwd: "/w", Mode: "ask"}); err != nil {
		t.Fatal(err)
	}
	s.CreateSession(ctx, Session{ID: "s2", Cwd: "/w", Mode: "auto"})

	// Concurrent appends get distinct, gapless seqs.
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if _, err := s.Append(ctx, "s1", "note", map[string]int{"i": i}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	s.Append(ctx, "s1", MessageEvent, llm.Text(llm.User, "hi"))
	s.Append(ctx, "s1", MessageEvent, llm.Message{Role: llm.Assistant, Parts: []llm.Part{{ToolCall: &llm.ToolCall{ID: "c", Name: "f", Args: []byte(`{}`)}}}})
	if rev, _ := s.Revision(ctx, "s1"); rev != 22 {
		t.Errorf("revision %d", rev)
	}
	evs, _ := s.Events(ctx, "s1", 18, 2)
	if len(evs) != 2 || evs[0].Seq != 19 || evs[1].Seq != 20 {
		t.Errorf("%+v", evs)
	}
	msgs, err := s.Messages(ctx, "s1")
	if err != nil || len(msgs) != 2 || msgs[0].PlainText() != "hi" || msgs[1].Parts[0].ToolCall.Name != "f" {
		t.Fatalf("%+v %v", msgs, err)
	}

	// The last appended-to session lists first, and settings persist.
	list, _ := s.Sessions(ctx)
	if len(list) != 2 || list[0].ID != "s1" {
		t.Errorf("%+v", list)
	}
	x, _ := s.GetSession(ctx, "s2")
	x.Title, x.Model = "Fix tests", "m"
	s.UpdateSession(ctx, x)
	s.Close()

	s, _ = Open(path) // survives reopening
	defer s.Close()
	if x, _ := s.GetSession(ctx, "s2"); x.Title != "Fix tests" || x.Model != "m" || x.Mode != "auto" {
		t.Errorf("%+v", x)
	}
	if _, err := s.GetSession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Error(err)
	}
	s.DeleteSession(ctx, "s1")
	if evs, _ := s.Events(ctx, "s1", 0, 0); len(evs) != 0 {
		t.Error("events outlived their session")
	}

	s.PutReceipt(ctx, Receipt{ID: "r1", Method: "prompt", Status: "accepted"})
	s.PutReceipt(ctx, Receipt{ID: "r1", Method: "prompt", Status: "completed"})
	if r, _ := s.GetReceipt(ctx, "r1"); r.Status != "completed" {
		t.Error(r)
	}
}
