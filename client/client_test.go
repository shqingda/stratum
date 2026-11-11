package client

import (
	"encoding/json"
	"fmt"
	"testing"

	"talos/proto"
)

// A scripted kernel: tests answer the commands a client sends with frames
// shaped as the real kernel's (see testdata and e2e_test.go).

const sid = "s1"

func resp(id string, result any) proto.Frame {
	b, _ := json.Marshal(result)
	return proto.Frame{Type: proto.TypeResponse, ID: id, Result: b}
}

func errResp(id, msg string) proto.Frame {
	return proto.Frame{Type: proto.TypeResponse, ID: id, Error: msg}
}

func ev(rev, seq uint64, typ string, data map[string]any) proto.Frame {
	return proto.Frame{Type: proto.TypeEvent, Scope: "desk", Revision: rev,
		Event: &proto.Event{SessionID: sid, Seq: seq, Type: typ, Data: data}}
}

func msg(role, text string) map[string]any {
	return map[string]any{"message": map[string]any{"role": role, "text": text}}
}

func session(id, title string) proto.Session { return proto.Session{ID: id, Title: title, Cwd: "/w"} }

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

// connected returns a client bound to session s1 at revision rev and event
// cursor cur, with the snapshot answered and history still outstanding.
func connected(t *testing.T, rev, cur uint64, snap func(*proto.Snapshot)) (*Client, proto.Command) {
	t.Helper()
	c := newClient("desk", "t")
	hello := one(t, c.Receive(proto.Frame{Type: proto.TypeReady, ProtocolVer: 2}), "hello")
	cmds := c.Receive(resp(hello.ID, map[string]any{}))
	one(t, cmds, "workspace.current")
	list := one(t, cmds, "session.list")
	resume := one(t, c.Receive(resp(list.ID, []proto.Session{session(sid, "first"), session("s0", "older")})), "session.resume")
	if resume.Params["sessionId"] != sid || c.Phase != Syncing {
		t.Fatalf("resume %+v, phase %s", resume, c.Phase)
	}
	current := one(t, c.Receive(resp(resume.ID, session(sid, "first"))), "session.current")
	s := proto.Snapshot{Session: &proto.Session{ID: sid, Title: "first"}, Revision: rev, Cursor: cur, RunStatus: "idle"}
	if snap != nil {
		snap(&s)
	}
	history := one(t, c.Receive(resp(current.ID, s)), "messages.read")
	if c.Phase != Ready {
		t.Fatalf("phase %s after snapshot", c.Phase)
	}
	return c, history
}

func texts(c *Client) []string {
	var out []string
	for _, e := range c.Entries {
		out = append(out, string(e.Role)+":"+e.Text)
	}
	return out
}

func TestIdsAreUniqueAcrossLaunches(t *testing.T) {
	a := New("desk").Receive(proto.Frame{Type: proto.TypeReady})
	b := New("desk").Receive(proto.Frame{Type: proto.TypeReady})
	if a[0].ID == b[0].ID {
		t.Errorf("two launches both used id %s", a[0].ID)
	}
}

func TestHistoryThenLive(t *testing.T) {
	c, history := connected(t, 5, 4, nil)
	if history.Params["before"] != uint64(5) {
		t.Errorf("history before %v", history.Params["before"])
	}
	// A live event arrives before the history answer.
	c.Receive(ev(6, 5, "message", msg("user", "second")))
	c.Receive(resp(history.ID, proto.MessagePage{Items: []proto.StoredMessage{
		stored(2, "user", "first"), stored(4, "assistant", "reply"),
	}, HasOlder: true}))
	want := []string{"user:first", "assistant:reply", "user:second"}
	if fmt.Sprint(texts(c)) != fmt.Sprint(want) || !c.HasOlder {
		t.Errorf("transcript %q, older %v", texts(c), c.HasOlder)
	}
}

func stored(seq uint64, role, text string) proto.StoredMessage {
	var m proto.StoredMessage
	m.Seq, m.Message.Role, m.Message.Text = seq, role, text
	return m
}

func TestFramesDuringSyncAreBufferedAndFiltered(t *testing.T) {
	c := newClient("desk", "t")
	hello := one(t, c.Receive(proto.Frame{Type: proto.TypeReady}), "hello")
	list := one(t, c.Receive(resp(hello.ID, nil)), "session.list")
	create := one(t, c.Receive(resp(list.ID, []proto.Session{})), "session.create")
	current := one(t, c.Receive(resp(create.ID, session(sid, "New session"))), "session.current")
	// Frames before the snapshot answer: rev 3 is covered by it, 4 and 5 are not.
	c.Receive(ev(3, 3, "run.start", map[string]any{"runId": "r1"}))
	c.Receive(ev(4, 4, "text.delta", map[string]any{"text": "lo"}))
	c.Receive(ev(5, 5, "text.delta", map[string]any{"text": "!"}))
	if len(c.Entries) != 0 {
		t.Fatal("frames applied before the snapshot")
	}
	runID := "r1"
	c.Receive(resp(current.ID, proto.Snapshot{Session: &proto.Session{ID: sid}, Revision: 3, Cursor: 3,
		Busy: true, Stream: "Hel", RunID: &runID, RunStatus: "running"}))
	if c.Phase != Ready || !c.Busy || c.RunID != "r1" {
		t.Fatalf("phase %s busy %v run %q", c.Phase, c.Busy, c.RunID)
	}
	if got := texts(c); len(got) != 1 || got[0] != "assistant:Hello!" {
		t.Errorf("stream %q", got)
	}
}

func TestBufferOverflowRereads(t *testing.T) {
	c := newClient("desk", "t")
	hello := one(t, c.Receive(proto.Frame{Type: proto.TypeReady}), "hello")
	list := one(t, c.Receive(resp(hello.ID, nil)), "session.list")
	create := one(t, c.Receive(resp(list.ID, nil)), "session.create")
	current := one(t, c.Receive(resp(create.ID, session(sid, ""))), "session.current")
	for i := uint64(1); i <= maxBuffered+1; i++ {
		c.Receive(ev(i, i, "text.delta", map[string]any{"text": "x"}))
	}
	cmds := c.Receive(resp(current.ID, proto.Snapshot{Session: &proto.Session{ID: sid}}))
	one(t, cmds, "session.current")
	if c.Phase != Syncing {
		t.Errorf("phase %s after overflow", c.Phase)
	}
}

func TestDuplicatesIgnoredGapRereads(t *testing.T) {
	c, _ := connected(t, 10, 10, nil)
	c.Receive(ev(11, 11, "text.delta", map[string]any{"text": "a"}))
	c.Receive(ev(11, 11, "text.delta", map[string]any{"text": "a"}))
	c.Receive(ev(10, 10, "text.delta", map[string]any{"text": "old"}))
	if got := texts(c); len(got) != 1 || got[0] != "assistant:a" {
		t.Fatalf("after duplicates %q", got)
	}
	cmds := c.Receive(ev(13, 13, "text.delta", map[string]any{"text": "c"}))
	one(t, cmds, "session.current")
	if c.Phase != Syncing || c.CanAct() {
		t.Errorf("gap left phase %s", c.Phase)
	}
	if _, err := c.Prompt("x"); err == nil {
		t.Error("prompt allowed while re-syncing")
	}
}

func TestOutOfOrderResponses(t *testing.T) {
	c, history := connected(t, 0, 0, nil)
	p, _ := c.Prompt("hi")
	m, _ := c.SearchModels("a")
	m2, _ := c.SearchModels("ab")
	// Answers in reverse order; the stale search answer is dropped.
	c.Receive(resp(m2[0].ID, proto.ModelPage{Items: []proto.Model{{Provider: "x", ID: "ab"}}}))
	c.Receive(resp(m[0].ID, proto.ModelPage{Items: []proto.Model{{Provider: "x", ID: "a"}}}))
	c.Receive(resp(history.ID, proto.MessagePage{}))
	c.Receive(resp(p[0].ID, map[string]any{"accepted": true}))
	if len(c.Models) != 1 || c.Models[0].ID != "ab" {
		t.Errorf("models %+v", c.Models)
	}
	for _, id := range []string{p[0].ID, m[0].ID, m2[0].ID, history.ID} {
		if _, ok := c.pending[id]; ok {
			t.Errorf("%s still pending", id)
		}
	}
}

func TestRunEventsAndTools(t *testing.T) {
	c, _ := connected(t, 0, 0, nil)
	frames := []proto.Frame{
		ev(1, 1, "message", msg("user", "edit it")),
		ev(2, 2, "run.start", map[string]any{"runId": "r1"}),
		ev(3, 3, "text.delta", map[string]any{"text": "Looking."}),
		ev(4, 4, "tool.start", map[string]any{"callId": "c1", "name": "read_file", "arguments": map[string]any{"path": "a.go"}}),
		ev(5, 5, "tool.end", map[string]any{"callId": "c1", "result": map[string]any{"text": "no", "isError": true}}),
		ev(6, 6, "text.delta", map[string]any{"text": "Done."}),
		ev(7, 7, "message", msg("assistant", "Done.")),
	}
	for _, f := range frames {
		c.Receive(f)
	}
	if !c.Busy || c.RunID != "r1" {
		t.Fatalf("busy %v run %q", c.Busy, c.RunID)
	}
	cmds := c.Receive(ev(8, 8, "run.end", map[string]any{"runId": "r1", "status": "completed"}))
	one(t, cmds, "session.list")
	want := []string{"user:edit it", "assistant:Looking.", "tool:read_file", "assistant:Done."}
	if fmt.Sprint(texts(c)) != fmt.Sprint(want) {
		t.Errorf("transcript %q", texts(c))
	}
	tool := c.Entries[2]
	if !tool.Done || !tool.Failed || tool.Detail != "a.go" || !c.Entries[1].Done {
		t.Errorf("tool %+v, first reply done %v", tool, c.Entries[1].Done)
	}
	if c.Busy {
		t.Error("still busy after run.end")
	}
}

func TestApprovals(t *testing.T) {
	pending := proto.Permission{ID: "a0", SessionID: sid, Tool: "shell", Reason: "run tests"}
	c, _ := connected(t, 0, 0, func(s *proto.Snapshot) { s.Approvals = []proto.Permission{pending} })
	c.Receive(proto.Frame{Type: proto.TypeApproval, Scope: "desk", Revision: 1,
		Request: &proto.Permission{ID: "a1", SessionID: sid, Tool: "write_file"}})
	if len(c.Approvals) != 2 {
		t.Fatalf("approvals %+v", c.Approvals)
	}
	cmds, err := c.Answer("a1", true)
	if err != nil || cmds[0].Method != "approval.respond" || cmds[0].Params["allow"] != true {
		t.Fatalf("answer %+v %v", cmds, err)
	}
	if _, err := c.Answer("a1", false); err == nil {
		t.Error("answered twice")
	}
	// A failed answer can be tried again.
	deny, _ := c.Answer("a0", false)
	c.Receive(errResp(deny[0].ID, "Approval expired"))
	if c.Answering["a0"] {
		t.Error("failed answer still in flight")
	}
	allow := true
	c.Receive(proto.Frame{Type: proto.TypeResolved, Scope: "desk", Revision: 2, ID: "a1", Allow: &allow})
	if len(c.Approvals) != 1 || c.Approvals[0].ID != "a0" || c.Answering["a1"] {
		t.Errorf("after resolve %+v %v", c.Approvals, c.Answering)
	}
}

func TestDisconnectThenReceipts(t *testing.T) {
	c, _ := connected(t, 0, 0, nil)
	p, _ := c.Prompt("hi")
	read, _ := c.SearchModels("x")
	_ = read
	c.Disconnected(nil)
	if c.Phase != Disconnected || len(c.Unknown) != 1 || c.Unknown[0].ID != p[0].ID {
		t.Fatalf("phase %s unknown %+v", c.Phase, c.Unknown)
	}
	if _, err := c.Prompt("again"); err == nil {
		t.Error("prompt while disconnected")
	}
	// A late answer from the old kernel changes nothing.
	c.Receive(resp(p[0].ID, map[string]any{"accepted": true}))
	// A new kernel: the client asks for the receipt and resumes the session.
	hello := one(t, c.Receive(proto.Frame{Type: proto.TypeReady}), "hello")
	cmds := c.Receive(resp(hello.ID, nil))
	receipt := one(t, cmds, "command.receipt")
	if receipt.Params["commandId"] != p[0].ID {
		t.Errorf("receipt for %v", receipt.Params)
	}
	list := one(t, cmds, "session.list")
	resume := one(t, c.Receive(resp(list.ID, []proto.Session{session("s9", "newer"), session(sid, "first")})), "session.resume")
	if resume.Params["sessionId"] != sid {
		t.Errorf("resumed %v, want the session shown before", resume.Params)
	}
	c.Receive(resp(receipt.ID, map[string]any{"receipt": proto.Receipt{ID: p[0].ID, Method: "prompt", Status: "completed"}}))
	if c.Unknown[0].Outcome != "completed" {
		t.Errorf("outcome %q", c.Unknown[0].Outcome)
	}
}

func TestDeletedSessionShowsAnother(t *testing.T) {
	c, _ := connected(t, 0, 0, nil)
	del, err := c.Delete(sid)
	if err != nil {
		t.Fatal(err)
	}
	c.Receive(resp(del[0].ID, map[string]any{}))
	current := one(t, c.Receive(proto.Frame{Type: proto.TypeInvalidated, Scope: "desk", Revision: 1}), "session.current")
	list := one(t, c.Receive(resp(current.ID, proto.Snapshot{})), "session.list")
	resume := one(t, c.Receive(resp(list.ID, []proto.Session{session("s0", "older")})), "session.resume")
	if resume.Params["sessionId"] != "s0" {
		t.Errorf("resume %v", resume.Params)
	}
}

func TestResumeFailureCreates(t *testing.T) {
	c := newClient("desk", "t")
	hello := one(t, c.Receive(proto.Frame{Type: proto.TypeReady}), "hello")
	list := one(t, c.Receive(resp(hello.ID, nil)), "session.list")
	resume := one(t, c.Receive(resp(list.ID, []proto.Session{session("gone", "")})), "session.resume")
	one(t, c.Receive(errResp(resume.ID, "Session not found")), "session.create")
}

func TestModeAndModel(t *testing.T) {
	c, _ := connected(t, 0, 0, nil)
	if _, err := c.SetPermissionMode("yolo"); err == nil {
		t.Error("unknown mode accepted")
	}
	mode, _ := c.SetPermissionMode("auto")
	c.Receive(resp(mode[0].ID, map[string]any{"mode": "auto"}))
	if c.Workspace.PermissionMode != "auto" {
		t.Errorf("mode %q", c.Workspace.PermissionMode)
	}
	set, _ := c.SetModel("demo", "offline")
	s := session(sid, "first")
	s.ModelSelection = &proto.ModelSelection{Provider: "demo", Model: "offline"}
	c.Receive(resp(set[0].ID, s))
	if c.Current.ModelSelection == nil || c.Current.ModelSelection.Model != "offline" {
		t.Errorf("current %+v", c.Current)
	}
}

func TestProtocolMismatch(t *testing.T) {
	c := newClient("desk", "t")
	if cmds := c.Receive(proto.Frame{Type: proto.TypeReady, ProtocolVer: 3}); len(cmds) != 0 || c.Phase != Disconnected {
		t.Errorf("protocol 3: %v, %s", methods(cmds), c.Phase)
	}
}
