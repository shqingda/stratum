package session

import (
	"context"
	"encoding/json"
	"iter"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// held is a provider whose first request waits until the test lets it
// go, so that prompts can be sent while a turn runs.
type held struct {
	*llm.Script
	once sync.Once
	go_  chan struct{}
}

func (h *held) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Event, error] {
	h.once.Do(func() {
		select {
		case <-h.go_:
		case <-ctx.Done():
		}
	})
	return h.Script.Stream(ctx, req)
}

// queueTest is a session whose first turn holds, and what its events
// say: the turns that ended and the queues streamed.
type queueTest struct {
	m      *Manager
	s      proto.Session
	h      *held
	ended  chan proto.TurnEnd
	queues chan proto.Queue
}

func newQueueTest(t *testing.T, turns ...llm.Turn) *queueTest {
	script := &llm.Script{Turns: turns}
	m, s := changesManager(t, script)
	q := &queueTest{m: m, s: s, h: &held{Script: script, go_: make(chan struct{})},
		ended: make(chan proto.TurnEnd, 8), queues: make(chan proto.Queue, 16)}
	m.Open = func(string) (llm.Provider, error) { return q.h, nil }
	t.Cleanup(m.Subscribe(func(e proto.Event) {
		switch e.Type {
		case proto.EventTurnEnd:
			var end proto.TurnEnd
			json.Unmarshal(e.Data, &end)
			q.ended <- end
		case proto.EventQueue:
			var qu proto.Queue
			json.Unmarshal(e.Data, &qu)
			q.queues <- qu
		}
	}))
	return q
}

func (q *queueTest) prompt(t *testing.T, text string, steer bool) proto.PromptResult {
	t.Helper()
	res, err := q.m.Prompt(context.Background(), proto.Prompt{SessionID: q.s.ID, Text: text, Steer: steer}, "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (q *queueTest) end(t *testing.T) proto.TurnEnd {
	t.Helper()
	select {
	case e := <-q.ended:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no turn ended")
	}
	return proto.TurnEnd{}
}

// queue waits for a queue event that satisfies ok.
func (q *queueTest) queue(t *testing.T, ok func(proto.Queue) bool) proto.Queue {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case qu := <-q.queues:
			if ok(qu) {
				return qu
			}
		case <-deadline:
			t.Fatal("no such queue")
		}
	}
}

// said is the text of the user's messages a request carried.
func said(req llm.Request) []string {
	var out []string
	for _, m := range req.Messages {
		if m.Role == llm.User {
			for _, p := range m.Parts {
				if p.Text != "" {
					out = append(out, p.Text)
				}
			}
		}
	}
	return out
}

// A prompt queued behind a turn that calls no tool starts the next turn
// once it ends; it used to wait for a round of tool calls that never came,
// and was lost.
func TestQueuedStartsNextTurn(t *testing.T) {
	q := newQueueTest(t, llm.Turn{Text: "一"}, llm.Turn{Text: "二"})
	q.prompt(t, "a", false)
	if res := q.prompt(t, "b", false); !res.Queued || res.ID == "" {
		t.Fatalf("result %+v", res)
	}
	close(q.h.go_)
	q.end(t)
	q.end(t)
	reqs := q.h.Requests
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	if got := said(reqs[1]); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("second turn said %q", got)
	}
}

// A steering prompt joins the running turn after its tool calls and
// starts no turn of its own.
func TestSteerJoinsTurn(t *testing.T) {
	q := newQueueTest(t,
		llm.Turn{Calls: []llm.ToolCall{llm.Call("l", "list_dir", map[string]any{"path": "."})}},
		llm.Turn{Text: "好"})
	q.prompt(t, "a", false)
	q.prompt(t, "顺便看看 b", true)
	close(q.h.go_)
	q.end(t)
	select {
	case <-q.ended:
		t.Fatal("a second turn ran")
	case <-time.After(100 * time.Millisecond):
	}
	reqs := q.h.Requests
	if len(reqs) != 2 || !slices.Contains(said(reqs[1]), "顺便看看 b") {
		t.Fatalf("requests %d, second said %q", len(reqs), said(reqs[len(reqs)-1]))
	}
}

// What waits when the user stops goes back to them, not into a turn.
func TestStopReturnsQueue(t *testing.T) {
	q := newQueueTest(t, llm.Turn{Text: "一"}, llm.Turn{Text: "二"})
	q.prompt(t, "a", false)
	q.prompt(t, "b", false)
	q.m.Cancel(q.s.ID)
	if !q.end(t).Stopped {
		t.Fatal("not stopped")
	}
	back := q.queue(t, func(qu proto.Queue) bool { return len(qu.Returned) > 0 })
	if len(back.Returned) != 1 || back.Returned[0].Text != "b" {
		t.Fatalf("returned %+v", back.Returned)
	}
	select {
	case <-q.ended:
		t.Fatal("b ran")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEditQueue(t *testing.T) {
	q := newQueueTest(t, llm.Turn{Text: "一"}, llm.Turn{Text: "二"})
	q.prompt(t, "a", false)
	b := q.prompt(t, "b", false)
	c := q.prompt(t, "c", false)
	if err := q.m.EditQueue(proto.QueueEdit{SessionID: q.s.ID, ID: b.ID, Action: "remove"}); err != nil {
		t.Fatal(err)
	}
	if err := q.m.EditQueue(proto.QueueEdit{SessionID: q.s.ID, ID: c.ID, Action: "steer"}); err != nil {
		t.Fatal(err)
	}
	got := q.queue(t, func(qu proto.Queue) bool { return len(qu.Items) == 1 && qu.Items[0].Steer })
	if got.Items[0].Text != "c" {
		t.Fatalf("queue %+v", got.Items)
	}
	if err := q.m.EditQueue(proto.QueueEdit{SessionID: q.s.ID, ID: b.ID, Action: "remove"}); err == nil {
		t.Fatal("removed b twice")
	}
	close(q.h.go_)
	q.end(t)
	q.end(t) // c came too late to join a tool round, so it starts the next
	if got := said(q.h.Requests[1]); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("second turn said %q", got)
	}
}

func TestDraft(t *testing.T) {
	m, s := changesManager(t, &llm.Script{})
	ctx := context.Background()
	before, _ := m.Get(ctx, s.ID) // Create's event may have moved updated_at on
	if err := m.SetDraft(ctx, proto.DraftSet{SessionID: s.ID, Text: "写了一半"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.Get(ctx, s.ID)
	if snap.Draft != "写了一半" || !snap.Session.Updated.Equal(before.Session.Updated) {
		t.Fatalf("draft %q, updated %v → %v", snap.Draft, before.Session.Updated, snap.Session.Updated)
	}
	m.SetDraft(ctx, proto.DraftSet{SessionID: s.ID})
	if snap, _ := m.Get(ctx, s.ID); snap.Draft != "" {
		t.Fatalf("draft %q after clearing", snap.Draft)
	}
	m.SetDraft(ctx, proto.DraftSet{SessionID: s.ID, Text: "x"})
	if err := m.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Store.Setting(ctx, "draft:"+s.ID); v != nil {
		t.Fatal("the draft outlived its session")
	}
}

// A command the user types runs in the session's folder, holds the
// session as a turn does, and the model reads what it printed.
func TestShell(t *testing.T) {
	q := newQueueTest(t, llm.Turn{Text: "看到了"})
	close(q.h.go_)
	shells := make(chan proto.Shell, 1)
	t.Cleanup(q.m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventShell {
			var sh proto.Shell
			json.Unmarshal(e.Data, &sh)
			shells <- sh
		}
	}))
	ctx := context.Background()
	if err := q.m.Shell(ctx, proto.ShellRun{SessionID: q.s.ID, Command: "sleep 0.3; echo hi"}); err != nil {
		t.Fatal(err)
	}
	if err := q.m.Shell(ctx, proto.ShellRun{SessionID: q.s.ID, Command: "echo again"}); err == nil {
		t.Fatal("a second command ran beside the first")
	}
	if res := q.prompt(t, "看看输出", false); !res.Queued {
		t.Fatal("a prompt did not wait for the command")
	}
	select {
	case sh := <-shells:
		if sh.Output != "hi\n" || sh.Stopped {
			t.Fatalf("shell %+v", sh)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command did not end")
	}
	q.end(t)
	got := said(q.h.Requests[0])
	if len(got) != 2 || got[0] != "I ran this command myself in the workspace:\n\n$ sleep 0.3; echo hi\nhi\n" || got[1] != "看看输出" {
		t.Fatalf("the model read %q", got)
	}
}
