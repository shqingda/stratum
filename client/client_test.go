package client

import (
	"encoding/json"
	"testing"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

// Unit tests answer the client's commands with frames shaped as the
// kernel's; kernel_test.go runs it against a real one.

const sid = "s1"

func resp(id string, result any) proto.Frame {
	b, _ := json.Marshal(result)
	return proto.Frame{Type: proto.FrameResponse, ID: id, Result: b}
}

func errResp(id, code, msg string) proto.Frame {
	return proto.Frame{Type: proto.FrameResponse, ID: id, Error: proto.Errorf(code, "%s", msg)}
}

func ev(seq int64, typ string, data any) proto.Frame {
	b, _ := json.Marshal(data)
	return proto.Frame{Type: proto.FrameEvent, Event: &proto.Event{SessionID: sid, Seq: seq, Type: typ, Data: b}}
}

func msg(role llm.Role, text string) llm.Message {
	return llm.Message{Role: role, Parts: []llm.Part{{Text: text}}}
}

func sess(id, cwd string) proto.Session { return proto.Session{ID: id, Cwd: cwd, Mode: "ask"} }

func one(t *testing.T, cmds []proto.Command, method string) proto.Command {
	t.Helper()
	for _, c := range cmds {
		if c.Method == method {
			return c
		}
	}
	t.Fatalf("no %s among %v", method, methods(cmds))
	return proto.Command{}
}

func methods(cmds []proto.Command) []string {
	var m []string
	for _, c := range cmds {
		m = append(m, c.Method)
	}
	return m
}

func params[T any](t *testing.T, cmd proto.Command) T {
	var v T
	if err := json.Unmarshal(cmd.Params, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// connected returns a client showing session s1, whose log holds log, with
// the events command still outstanding when log is nil.
func connected(t *testing.T, log []proto.Event) (*Client, proto.Command) {
	t.Helper()
	c := newClient("/w", "t")
	init := one(t, c.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version, Instance: "i1"}), proto.MethodInitialize)
	cmds := c.Receive(resp(init.ID, proto.Hello{Version: proto.Version}))
	one(t, cmds, proto.MethodAccounts)
	list := one(t, cmds, proto.MethodSessionList)
	get := one(t, c.Receive(resp(list.ID, []proto.Session{sess("s9", "/elsewhere"), sess(sid, "/w"), sess("s0", "/w")})), proto.MethodSessionGet)
	if p := params[proto.SessionRef](t, get); p.SessionID != sid || c.Phase != Syncing {
		t.Fatalf("bound %+v, phase %s", p, c.Phase)
	}
	events := one(t, c.Receive(resp(get.ID, proto.Snapshot{Session: sess(sid, "/w")})), proto.MethodEvents)
	if log == nil {
		return c, events
	}
	if c.Receive(resp(events.ID, proto.EventPage{Events: log})) != nil || c.Phase != Ready {
		t.Fatalf("phase %s after the log", c.Phase)
	}
	return c, events
}

func texts(c *Client) []string {
	var out []string
	for _, e := range c.Entries {
		out = append(out, string(e.Role)+":"+e.Text)
	}
	return out
}

func event(f proto.Frame) proto.Event { return *f.Event }

func TestIdsAreUniqueAcrossLaunches(t *testing.T) {
	a, b := New("/w"), New("/w")
	ca := a.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version})
	cb := b.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version})
	if ca[0].ID == cb[0].ID {
		t.Error(ca[0].ID)
	}
}

func TestProtocolMismatch(t *testing.T) {
	c := newClient("/w", "t")
	if cmds := c.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version + 1}); cmds != nil || c.Phase != Disconnected || c.Err == "" {
		t.Errorf("%v %s %q", cmds, c.Phase, c.Err)
	}
}

func TestNoSessionCreatesOne(t *testing.T) {
	c := newClient("/w", "t")
	init := one(t, c.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version}), proto.MethodInitialize)
	list := one(t, c.Receive(resp(init.ID, nil)), proto.MethodSessionList)
	create := one(t, c.Receive(resp(list.ID, []proto.Session{sess("s9", "/elsewhere")})), proto.MethodSessionCreate)
	if p := params[proto.SessionCreate](t, create); p.Cwd != "/w" {
		t.Errorf("%+v", p)
	}
	one(t, c.Receive(resp(create.ID, sess(sid, "/w"))), proto.MethodSessionGet)
}

func TestLogThenLive(t *testing.T) {
	c, _ := connected(t, []proto.Event{
		event(ev(1, proto.EventTurnStart, proto.TurnStart{Text: "hi"})),
		event(ev(2, proto.EventMessage, msg(llm.User, "hi"))),
		event(ev(3, proto.EventMessage, msg(llm.Assistant, "hello"))),
		event(ev(4, proto.EventTurnEnd, proto.TurnEnd{Usage: llm.Usage{Input: 10}})),
	})
	c.Receive(ev(5, proto.EventTurnStart, nil))
	c.Receive(ev(0, proto.EventTextDelta, proto.Delta{Text: "wor"}))
	c.Receive(ev(0, proto.EventTextDelta, proto.Delta{Text: "ld"}))
	if !c.Busy || c.Entries[len(c.Entries)-1].Text != "world" {
		t.Errorf("%v %v", c.Busy, texts(c))
	}
	c.Receive(ev(6, proto.EventMessage, msg(llm.Assistant, "world!")))
	c.Receive(ev(7, proto.EventTurnEnd, proto.TurnEnd{Usage: llm.Usage{Input: 5}}))
	want := []string{"user:hi", "assistant:hello", "assistant:world!"}
	if got := texts(c); len(got) != 3 || got[2] != want[2] || got[0] != want[0] || c.Busy || c.Usage.Input != 15 {
		t.Errorf("%v busy %v usage %+v", got, c.Busy, c.Usage)
	}
	if !c.Entries[2].Done || c.Entries[2].Seq != 6 {
		t.Errorf("%+v", c.Entries[2])
	}
}

func TestEventsDuringSyncAreBuffered(t *testing.T) {
	c, events := connected(t, nil)
	c.Receive(ev(2, proto.EventMessage, msg(llm.User, "hi")))       // also in the log
	c.Receive(ev(0, proto.EventTextDelta, proto.Delta{Text: "x"}))  // belongs to seq 3, in the log
	c.Receive(ev(3, proto.EventMessage, msg(llm.Assistant, "xy")))  // also in the log
	c.Receive(ev(0, proto.EventTextDelta, proto.Delta{Text: "ne"})) // newer
	other := ev(1, proto.EventMessage, msg(llm.User, "elsewhere"))
	other.Event.SessionID = "s0"
	c.Receive(other)
	c.Receive(resp(events.ID, proto.EventPage{Events: []proto.Event{
		event(ev(1, proto.EventTurnStart, nil)),
		event(ev(2, proto.EventMessage, msg(llm.User, "hi"))),
		event(ev(3, proto.EventMessage, msg(llm.Assistant, "xy"))),
	}}))
	if got := texts(c); len(got) != 3 || got[2] != "assistant:ne" || c.Phase != Ready {
		t.Errorf("%v %s", got, c.Phase)
	}
}

func TestLogIsPaged(t *testing.T) {
	c, events := connected(t, nil)
	more := one(t, c.Receive(resp(events.ID, proto.EventPage{More: true, Events: []proto.Event{event(ev(1, proto.EventMessage, msg(llm.User, "a")))}})), proto.MethodEvents)
	if p := params[proto.EventsQuery](t, more); p.After != 1 || c.Phase != Syncing {
		t.Errorf("%+v %s", p, c.Phase)
	}
	c.Receive(resp(more.ID, proto.EventPage{Events: []proto.Event{event(ev(2, proto.EventMessage, msg(llm.User, "b")))}}))
	if got := texts(c); len(got) != 2 || c.Phase != Ready {
		t.Errorf("%v %s", got, c.Phase)
	}
}

func TestBufferOverflowRereads(t *testing.T) {
	c, events := connected(t, nil)
	for i := range maxBuffered + 1 {
		c.Receive(ev(int64(i+1), proto.EventMessage, msg(llm.User, "x")))
	}
	again := one(t, c.Receive(resp(events.ID, proto.EventPage{Events: []proto.Event{}})), proto.MethodEvents)
	if p := params[proto.EventsQuery](t, again); p.After != 0 {
		t.Errorf("%+v", p)
	}
}

func TestDuplicatesIgnoredGapRereads(t *testing.T) {
	c, _ := connected(t, []proto.Event{event(ev(1, proto.EventMessage, msg(llm.User, "a")))})
	if c.Receive(ev(1, proto.EventMessage, msg(llm.User, "a"))) != nil || len(c.Entries) != 1 {
		t.Error("a duplicate was applied")
	}
	again := one(t, c.Receive(ev(3, proto.EventMessage, msg(llm.User, "c"))), proto.MethodEvents)
	if p := params[proto.EventsQuery](t, again); p.After != 1 || c.Phase != Syncing {
		t.Errorf("%+v %s", p, c.Phase)
	}
	c.Receive(resp(again.ID, proto.EventPage{Events: []proto.Event{
		event(ev(2, proto.EventMessage, msg(llm.User, "b"))),
		event(ev(3, proto.EventMessage, msg(llm.User, "c"))),
	}}))
	if got := texts(c); len(got) != 3 || got[1] != "user:b" || c.Phase != Ready {
		t.Errorf("%v", got)
	}
}

func TestToolsFromLogAndLive(t *testing.T) {
	call := llm.Call("c1", "read_file", map[string]string{"path": "a.go"})
	c, _ := connected(t, []proto.Event{
		event(ev(1, proto.EventMessage, llm.Message{Role: llm.Assistant, Parts: []llm.Part{{Text: "Reading."}, {ToolCall: &call}}})),
		event(ev(2, proto.EventMessage, llm.Message{Role: llm.User, Parts: []llm.Part{{ToolResult: &llm.ToolResult{CallID: "c1", Content: "1 package a"}}}})),
	})
	if got := texts(c); len(got) != 2 || got[1] != "tool:read_file" || c.Entries[1].Detail != "a.go" || !c.Entries[1].Done || c.Entries[1].Output != "1 package a" {
		t.Fatalf("%v %+v", got, c.Entries[1])
	}
	shell := llm.Call("c2", "shell", map[string]string{"command": "go test"})
	c.Receive(ev(3, proto.EventMessage, llm.Message{Role: llm.Assistant, Parts: []llm.Part{{ToolCall: &shell}}}))
	c.Receive(ev(0, proto.EventToolStart, proto.ToolEvent{Call: shell}))
	if e := c.Entries[2]; e.Done || e.Detail != "go test" || len(c.Entries) != 3 {
		t.Errorf("%v %+v", texts(c), e)
	}
	c.Receive(ev(0, proto.EventToolEnd, proto.ToolEvent{Call: shell, Result: &llm.ToolResult{CallID: "c2", Content: "FAIL", IsError: true}}))
	c.Receive(ev(4, proto.EventMessage, llm.Message{Role: llm.User, Parts: []llm.Part{{ToolResult: &llm.ToolResult{CallID: "c2", Content: "FAIL", IsError: true}}}}))
	if e := c.Entries[2]; !e.Done || !e.Failed || len(c.Entries) != 3 {
		t.Errorf("%v %+v", texts(c), e)
	}
}

func TestApprovals(t *testing.T) {
	c, _ := connected(t, []proto.Event{
		event(ev(1, proto.EventTurnStart, nil)),
		event(ev(2, proto.EventApprovalRequest, proto.Approval{ID: "a1", Tool: "edit_file"})),
		event(ev(3, proto.EventApprovalResolved, proto.ApprovalAnswer{ApprovalID: "a1", Allow: true})),
		event(ev(4, proto.EventApprovalRequest, proto.Approval{ID: "a2", Tool: "shell"})),
	})
	if len(c.Approvals) != 1 || c.Approvals[0].ID != "a2" || !c.Busy {
		t.Fatalf("%+v", c.Approvals)
	}
	cmds, err := c.Answer("a2", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if p := params[proto.ApprovalAnswer](t, cmds[0]); p.SessionID != sid || !p.Allow || !p.Always {
		t.Errorf("%+v", p)
	}
	if _, err := c.Answer("a2", true, false); err == nil {
		t.Error("answered twice")
	}
	c.Receive(errResp(cmds[0].ID, "not_found", "no approval a2 waiting"))
	if c.Answering["a2"] {
		t.Error("still answering after a failure")
	}
	cmds, _ = c.Answer("a2", false, false)
	c.Receive(resp(cmds[0].ID, nil))
	c.Receive(ev(5, proto.EventApprovalResolved, proto.ApprovalAnswer{ApprovalID: "a2"}))
	if len(c.Approvals) != 0 || c.Answering["a2"] {
		t.Errorf("%+v %v", c.Approvals, c.Answering)
	}
}

func TestQueuedPrompt(t *testing.T) {
	c, _ := connected(t, []proto.Event{event(ev(1, proto.EventTurnStart, nil))})
	cmds, err := c.Prompt("also this")
	if err != nil {
		t.Fatal(err)
	}
	c.Receive(resp(cmds[0].ID, proto.PromptResult{Queued: true}))
	if len(c.Queued) != 1 {
		t.Fatal(c.Queued)
	}
	c.Receive(ev(2, proto.EventMessage, msg(llm.User, "also this")))
	if len(c.Queued) != 0 {
		t.Error(c.Queued)
	}
	cmds, _ = c.Cancel()
	if p := params[proto.SessionRef](t, cmds[0]); p.SessionID != sid {
		t.Error(p)
	}
	c.Receive(ev(3, proto.EventTurnEnd, proto.TurnEnd{Stopped: true}))
	if _, err := c.Cancel(); err == nil || c.Entries[len(c.Entries)-1].Role != Notice {
		t.Errorf("%v %v", err, texts(c))
	}
}

func TestDisconnectThenReceipts(t *testing.T) {
	c, _ := connected(t, []proto.Event{})
	sent, _ := c.Prompt("hi")
	c.ListModels("codex") // a read: not unknown
	c.Disconnected(nil)
	if len(c.Unknown) != 1 || c.Unknown[0].ID != sent[0].ID || c.Phase != Disconnected {
		t.Fatalf("%+v", c.Unknown)
	}
	if _, err := c.Prompt("again"); err != ErrNotReady {
		t.Error(err)
	}
	init := one(t, c.Receive(proto.Frame{Type: proto.FrameReady, Version: proto.Version, Instance: "i2"}), proto.MethodInitialize)
	cmds := c.Receive(resp(init.ID, nil))
	receipt := one(t, cmds, proto.MethodReceipt)
	list := one(t, cmds, proto.MethodSessionList)
	c.Receive(resp(receipt.ID, proto.Receipt{ID: sent[0].ID, Status: "unknown"}))
	if c.Unknown[0].Outcome != "not received" {
		t.Error(c.Unknown[0])
	}
	// The session shown before comes back, even if not the newest.
	get := one(t, c.Receive(resp(list.ID, []proto.Session{sess("s2", "/w"), sess(sid, "/w")})), proto.MethodSessionGet)
	if p := params[proto.SessionRef](t, get); p.SessionID != sid || c.Instance != "i2" {
		t.Error(p)
	}
}

func TestDeletedSessionShowsAnother(t *testing.T) {
	c, _ := connected(t, []proto.Event{})
	cmds, _ := c.Delete(sid)
	list := one(t, c.Receive(resp(cmds[0].ID, nil)), proto.MethodSessionList)
	get := one(t, c.Receive(resp(list.ID, []proto.Session{sess("s0", "/w")})), proto.MethodSessionGet)
	if p := params[proto.SessionRef](t, get); p.SessionID != "s0" {
		t.Error(p)
	}
	// Gone before it could be read: the list is read again.
	list = one(t, c.Receive(errResp(get.ID, "not_found", "no session s0")), proto.MethodSessionList)
	one(t, c.Receive(resp(list.ID, []proto.Session{})), proto.MethodSessionCreate)
}

func TestSessionUpdates(t *testing.T) {
	c, _ := connected(t, []proto.Event{})
	mode := "loose"
	if _, err := c.Update(proto.SessionUpdate{Mode: &mode}); err == nil {
		t.Error("bad mode accepted")
	}
	mode = "auto"
	cmds, _ := c.Update(proto.SessionUpdate{Mode: &mode})
	if p := params[proto.SessionUpdate](t, cmds[0]); p.SessionID != sid || *p.Mode != "auto" || p.Model != nil {
		t.Errorf("%+v", p)
	}
	s := sess(sid, "/w")
	s.Mode, s.Title = "auto", "Renamed"
	c.Receive(ev(1, proto.EventSession, s))
	other := ev(1, proto.EventSession, proto.Session{ID: "s7", Title: "new elsewhere"})
	other.Event.SessionID = "s7"
	c.Receive(other)
	if c.Current.Mode != "auto" || c.Sessions[0].ID != "s7" {
		t.Errorf("%+v %+v", c.Current, c.Sessions)
	}
	for _, x := range c.Sessions {
		if x.ID == sid && x.Title != "Renamed" {
			t.Errorf("%+v", x)
		}
	}
}

func TestOnlyLatestModelsKept(t *testing.T) {
	c, _ := connected(t, []proto.Event{})
	a, _ := c.ListModels("codex")
	b, _ := c.ListModels("xai")
	c.Receive(resp(b[0].ID, []proto.Model{{ID: "grok"}}))
	c.Receive(resp(a[0].ID, []proto.Model{{ID: "gpt"}}))
	if len(c.Models) != 1 || c.Models[0].ID != "grok" {
		t.Error(c.Models)
	}
}

// A model list that fails while a new session syncs leaves the sync alone.
func TestModelsFailureDuringSync(t *testing.T) {
	c := newClient("/w", "t")
	c.Phase = Ready
	c.Current = &proto.Session{ID: "s1"}
	cmds, err := c.ListModels("chatgpt")
	if err != nil {
		t.Fatal(err)
	}
	c.Phase = Syncing
	c.Receive(proto.Frame{Type: proto.FrameResponse, ID: cmds[0].ID, Error: proto.Errorf("test", "offline")})
	if c.Phase != Syncing || c.Err != "" {
		t.Fatalf("phase %v, err %q", c.Phase, c.Err)
	}
}
