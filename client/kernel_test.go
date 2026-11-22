package client

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/rpc"
	"github.com/shqingda/stratum/kernel/session"
	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// conn drives a Client against a real kernel over pipes, as the app does
// on its main thread.
type conn struct {
	t      *testing.T
	c      *Client
	in     io.Writer
	frames chan proto.Frame
}

func dial(t *testing.T, m *session.Manager, c *Client) *conn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cr, cw := io.Pipe()
	fr, fw := io.Pipe()
	srv := &rpc.Server{Sessions: m}
	done := make(chan struct{})
	go func() { srv.Serve(ctx, cr, fw); fw.Close(); close(done) }()
	t.Cleanup(func() { cancel(); cw.Close(); <-done })
	k := &conn{t: t, c: c, in: cw, frames: make(chan proto.Frame, 4096)}
	go func() {
		sc := bufio.NewScanner(fr)
		sc.Buffer(nil, proto.MaxLine)
		for sc.Scan() {
			var f proto.Frame
			json.Unmarshal(sc.Bytes(), &f)
			k.frames <- f
		}
		close(k.frames)
	}()
	return k
}

func (k *conn) send(cmds []proto.Command) {
	for _, cmd := range cmds {
		b, _ := json.Marshal(cmd)
		k.in.Write(append(b, '\n'))
	}
}

// until applies frames until cond holds.
func (k *conn) until(what string, cond func() bool) {
	k.t.Helper()
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case f, ok := <-k.frames:
			if !ok {
				k.t.Fatalf("kernel went away waiting for %s", what)
			}
			k.send(k.c.Receive(f))
		case <-deadline:
			k.t.Fatalf("waiting for %s: phase %s, entries %v", what, k.c.Phase, texts(k.c))
		}
	}
}

func (k *conn) do(cmds []proto.Command, err error) {
	k.t.Helper()
	if err != nil {
		k.t.Fatal(err)
	}
	k.send(cmds)
}

func TestAgainstKernel(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("todo: ship\n"), 0o644)
	st, err := store.Open(filepath.Join(t.TempDir(), "stratum.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "Let me look.", Calls: []llm.ToolCall{llm.Call("c1", "read_file", map[string]string{"path": "notes.txt"})}},
		{Calls: []llm.ToolCall{llm.Call("c2", "write_file", map[string]string{"path": "notes.txt", "content": "done: ship\n"})}},
		{Text: "Marked it done."},
		{Text: "Still here."},
	}}
	m := session.New(st, func(string) (llm.Provider, error) { return script, nil })
	m.Defaults = func() (string, string) { return "script", "script" }
	defer m.Close()

	c := newClient(dir, "t")
	k := dial(t, m, c)
	k.until("ready", func() bool { return c.Phase == Ready })
	if c.Current == nil || c.Current.Cwd != dir || c.Current.Mode != "ask" {
		t.Fatalf("%+v", c.Current)
	}

	k.do(c.Prompt("mark the todo done"))
	k.until("the approval", func() bool { return len(c.Approvals) == 1 })
	if a := c.Approvals[0]; a.Tool != "write_file" || a.Kind != "write" {
		t.Errorf("%+v", a)
	}
	k.do(c.Answer(c.Approvals[0].ID, true, false))
	k.until("the turn's end", func() bool { return !c.Busy && len(c.Approvals) == 0 && len(c.Entries) >= 5 })

	want := []string{"user:mark the todo done", "assistant:Let me look.", "tool:read_file", "tool:write_file", "assistant:Marked it done."}
	if got := strings.Join(texts(c), "|"); got != strings.Join(want, "|") {
		t.Errorf("live transcript\n got %s\nwant %s", got, strings.Join(want, "|"))
	}
	for _, e := range c.Entries {
		if !e.Done || e.Failed {
			t.Errorf("%+v", e)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "done: ship\n" {
		t.Errorf("%q", b)
	}

	// Another window on the same kernel rebuilds the same transcript from
	// the log alone.
	c2 := newClient(dir, "u")
	k2 := dial(t, m, c2)
	k2.until("ready", func() bool { return c2.Phase == Ready })
	if c2.Current.ID != c.Current.ID || strings.Join(texts(c2), "|") != strings.Join(want, "|") {
		t.Errorf("rebuilt %v", texts(c2))
	}
	if c2.Current.Title != "mark the todo done" {
		t.Errorf("title %q", c2.Current.Title)
	}

	// A change made in one shows in the other.
	mode := "auto"
	k2.do(c2.Update(proto.SessionUpdate{Mode: &mode}))
	k.do(c.Prompt("still there?"))
	k.until("the mode and the answer", func() bool {
		return c.Current.Mode == "auto" && !c.Busy && c.Entries[len(c.Entries)-1].Text == "Still here."
	})
}
