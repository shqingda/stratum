package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

var talosd string // the binary under test

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "talosd-test")
	if err != nil {
		panic(err)
	}
	talosd = filepath.Join(dir, "talosd")
	if out, err := exec.Command("go", "build", "-o", talosd, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// kernel is a running talosd rpc driven through its stdio.
type kernel struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	ready  proto.Frame
	events chan proto.Event

	mu      sync.Mutex
	waiting map[string]chan proto.Frame
	n       int
}

func start(t *testing.T, db string, turns []llm.Turn) *kernel {
	t.Helper()
	script := filepath.Join(t.TempDir(), "script.json")
	b, _ := json.Marshal(turns)
	os.WriteFile(script, b, 0o600)
	cmd := exec.Command(talosd, "rpc", "-db", db, "-script", script)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	k := &kernel{t: t, cmd: cmd, in: in, events: make(chan proto.Event, 1000), waiting: map[string]chan proto.Frame{}}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ready := make(chan proto.Frame, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(nil, proto.MaxLine)
		for sc.Scan() {
			var f proto.Frame
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				t.Errorf("bad frame %s: %v", sc.Bytes(), err)
				continue
			}
			switch f.Type {
			case proto.FrameReady:
				ready <- f
			case proto.FrameEvent:
				k.events <- *f.Event
			case proto.FrameResponse:
				k.mu.Lock()
				ch := k.waiting[f.ID]
				k.mu.Unlock()
				if ch != nil {
					ch <- f
				}
			}
		}
	}()
	select {
	case k.ready = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("no ready frame")
	}
	return k
}

// call sends a command and decodes its result into out.
func (k *kernel) call(method string, params, out any) string {
	k.t.Helper()
	k.mu.Lock()
	k.n++
	id := fmt.Sprintf("c%d", k.n)
	ch := make(chan proto.Frame, 1)
	k.waiting[id] = ch
	k.mu.Unlock()
	p, _ := json.Marshal(params)
	line, _ := json.Marshal(proto.Command{ID: id, Method: method, Params: p})
	k.in.Write(append(line, '\n'))
	select {
	case f := <-ch:
		if f.Error != nil {
			k.t.Fatalf("%s: %v", method, f.Error)
		}
		if out != nil {
			if err := json.Unmarshal(f.Result, out); err != nil {
				k.t.Fatalf("%s result %s: %v", method, f.Result, err)
			}
		}
	case <-time.After(30 * time.Second):
		k.t.Fatalf("%s: no response", method)
	}
	return id
}

// await reads events until one of type typ, which it returns; seen gets
// every event read.
func (k *kernel) await(typ string, seen *[]proto.Event) proto.Event {
	k.t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case e := <-k.events:
			if seen != nil {
				*seen = append(*seen, e)
			}
			if e.Type == typ {
				return e
			}
		case <-deadline:
			k.t.Fatalf("no %s event", typ)
		}
	}
}

func call(id, name string, args any) llm.ToolCall { return llm.Call(id, name, args) }

func gitRepo(t *testing.T) string {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":      "module calc\n\ngo 1.22\n",
		"add.go":      "package calc\n\nfunc Add(a, b int) int { return a - b }\n",
		"add_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(Add(2, 3))\n\t}\n}\n",
	}
	for name, body := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	exec.Command("git", "-C", dir, "init", "-q").Run()
	return dir
}

// TestRPCFixBug drives a whole turn: read a file, edit it (approved), run
// the tests (approved), answer.
func TestRPCFixBug(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs talosd")
	}
	repo := gitRepo(t)
	db := filepath.Join(t.TempDir(), "talos.db")
	k := start(t, db, []llm.Turn{
		{Text: "Reading the code.", Calls: []llm.ToolCall{call("c1", "read_file", map[string]string{"path": "add.go"})}},
		{Text: "Add subtracts; fixing.", Calls: []llm.ToolCall{call("c2", "edit_file", map[string]string{"path": "add.go", "old_text": "a - b", "new_text": "a + b"})}},
		{Calls: []llm.ToolCall{call("c3", "shell", map[string]string{"command": "go test ./..."})}},
		{Text: "Fixed: Add now adds, and the tests pass."},
	})
	var hello proto.Hello
	k.call(proto.MethodInitialize, nil, &hello)
	if hello.Version != proto.Version || hello.Instance != k.ready.Instance {
		t.Errorf("%+v %+v", hello, k.ready)
	}
	var s proto.Session
	k.call(proto.MethodSessionCreate, proto.SessionCreate{Cwd: repo, Mode: "ask"}, &s)
	if s.Account != "script" || s.Mode != "ask" {
		t.Errorf("%+v", s)
	}
	var res proto.PromptResult
	promptID := k.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "Add is broken, fix it"}, &res)

	var seen []proto.Event
	for range 2 { // the edit, then the test run, need approval
		ev := k.await(proto.EventApprovalRequest, &seen)
		var a proto.Approval
		json.Unmarshal(ev.Data, &a)
		var snap proto.Snapshot
		k.call(proto.MethodSessionGet, proto.SessionRef{SessionID: s.ID}, &snap)
		if !snap.Running || len(snap.Approvals) != 1 || snap.Approvals[0].ID != a.ID {
			t.Errorf("snapshot while waiting: %+v", snap)
		}
		k.call(proto.MethodApprovalRespond, proto.ApprovalAnswer{SessionID: s.ID, ApprovalID: a.ID, Allow: true}, nil)
	}
	end := k.await(proto.EventTurnEnd, &seen)
	var te proto.TurnEnd
	json.Unmarshal(end.Data, &te)
	if te.Error != "" || te.Stopped {
		t.Fatalf("turn ended with %+v", te)
	}

	if b, _ := os.ReadFile(filepath.Join(repo, "add.go")); !strings.Contains(string(b), "a + b") {
		t.Errorf("not fixed: %s", b)
	}
	var testOut string
	var deltas strings.Builder
	for _, e := range seen {
		switch e.Type {
		case proto.EventToolEnd:
			var te proto.ToolEvent
			json.Unmarshal(e.Data, &te)
			if te.Call.Name == "shell" {
				testOut = te.Result.Content
			}
		case proto.EventTextDelta:
			var d proto.Delta
			json.Unmarshal(e.Data, &d)
			deltas.WriteString(d.Text)
		}
	}
	if !strings.Contains(testOut, "ok") {
		t.Errorf("test run: %q", testOut)
	}
	if !strings.Contains(deltas.String(), "tests pass") {
		t.Errorf("streamed %q", deltas.String())
	}

	// The log holds the turn: start, 7 messages, approvals, end.
	var page proto.EventPage
	k.call(proto.MethodEvents, proto.EventsQuery{SessionID: s.ID}, &page)
	var types []string
	for _, e := range page.Events {
		types = append(types, e.Type)
		if e.Seq == 0 {
			t.Error("a logged event has no seq")
		}
	}
	if n := strings.Count(strings.Join(types, " "), proto.EventMessage); n != 8 {
		t.Errorf("%d messages in %v", n, types)
	}
	var rc proto.Receipt
	k.call(proto.MethodReceipt, proto.ReceiptRef{ID: promptID}, &rc)
	if rc.Status != "completed" {
		t.Errorf("receipt %+v", rc)
	}
	var snap proto.Snapshot
	k.call(proto.MethodSessionGet, proto.SessionRef{SessionID: s.ID}, &snap)
	if snap.Running || snap.Session.Title != "Add is broken, fix it" || snap.Revision != page.Events[len(page.Events)-1].Seq {
		t.Errorf("%+v", snap)
	}
}

// TestRPCRestart kills the kernel while a call waits for approval: the
// next kernel ends the turn and answers the call.
func TestRPCRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs talosd")
	}
	repo := gitRepo(t)
	db := filepath.Join(t.TempDir(), "talos.db")
	k := start(t, db, []llm.Turn{
		{Calls: []llm.ToolCall{call("w1", "write_file", map[string]string{"path": "x.txt", "content": "x"})}},
	})
	var s proto.Session
	k.call(proto.MethodSessionCreate, proto.SessionCreate{Cwd: repo}, &s)
	promptID := k.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "write x"}, nil)
	k.await(proto.EventApprovalRequest, nil)
	k.cmd.Process.Kill()
	k.cmd.Wait()

	k2 := start(t, db, []llm.Turn{{Text: "Picking up."}})
	if k2.ready.Instance == k.ready.Instance {
		t.Error("the instance did not change")
	}
	var snap proto.Snapshot
	k2.call(proto.MethodSessionGet, proto.SessionRef{SessionID: s.ID}, &snap)
	if snap.Running || len(snap.Approvals) != 0 {
		t.Errorf("%+v", snap)
	}
	var page proto.EventPage
	k2.call(proto.MethodEvents, proto.EventsQuery{SessionID: s.ID, After: 0}, &page)
	last := page.Events[len(page.Events)-1]
	var answered bool
	for _, e := range page.Events {
		if e.Type == proto.EventMessage && strings.Contains(string(e.Data), "Interrupted") {
			answered = true
		}
	}
	if last.Type != proto.EventTurnEnd || !answered {
		t.Errorf("not tidied: %s, answered %v", last.Type, answered)
	}
	if _, err := os.Stat(filepath.Join(repo, "x.txt")); err == nil {
		t.Error("the unapproved write happened")
	}
	var rc proto.Receipt
	k2.call(proto.MethodReceipt, proto.ReceiptRef{ID: promptID}, &rc)
	if rc.Status != "accepted" {
		t.Errorf("receipt %+v", rc) // accepted, never completed: the client knows it was cut short
	}

	// The session goes on.
	k2.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "go on"}, nil)
	var te proto.TurnEnd
	json.Unmarshal(k2.await(proto.EventTurnEnd, nil).Data, &te)
	if te.Error != "" {
		t.Fatal(te.Error)
	}
}

// TestRPCQueueAndCancel queues a prompt into a running turn, then stops it.
func TestRPCQueueAndCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs talosd")
	}
	repo := gitRepo(t)
	k := start(t, filepath.Join(t.TempDir(), "talos.db"), []llm.Turn{
		{Calls: []llm.ToolCall{call("s1", "shell", map[string]string{"command": "sleep 30"})}},
	})
	var s proto.Session
	k.call(proto.MethodSessionCreate, proto.SessionCreate{Cwd: repo, Mode: "full"}, &s)
	k.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "wait"}, nil)
	k.await(proto.EventToolStart, nil)
	var res proto.PromptResult
	k.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "and then this"}, &res)
	var snap proto.Snapshot
	k.call(proto.MethodSessionGet, proto.SessionRef{SessionID: s.ID}, &snap)
	if !res.Queued || len(snap.Queued) != 1 {
		t.Errorf("%+v %+v", res, snap)
	}
	started := time.Now()
	k.call(proto.MethodCancel, proto.SessionRef{SessionID: s.ID}, nil)
	var te proto.TurnEnd
	json.Unmarshal(k.await(proto.EventTurnEnd, nil).Data, &te)
	if !te.Stopped || time.Since(started) > 10*time.Second {
		t.Errorf("%+v after %v", te, time.Since(started))
	}
}
