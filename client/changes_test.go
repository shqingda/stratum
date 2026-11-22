package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/kernel/session"
	"github.com/shqingda/stratum/kernel/store"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestChangesAgainstKernel(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	notes := filepath.Join(dir, "notes.txt")
	os.WriteFile(notes, []byte("todo: ship\n"), 0o644)
	st, err := store.Open(filepath.Join(t.TempDir(), "stratum.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{llm.Call("c1", "write_file", map[string]string{"path": "notes.txt", "content": "done: ship\n"})}},
		{Text: "Done."},
	}}
	m := session.New(st, func(string) (llm.Provider, error) { return script, nil })
	m.Defaults = func() (string, string) { return "script", "script" }
	defer m.Close()

	c := newClient(dir, "t")
	k := dial(t, m, c)
	k.until("ready", func() bool { return c.Phase == Ready })
	if !c.Changes.Stale {
		t.Fatal("changes not stale before the first read")
	}
	full := "full"
	k.do(c.Update(proto.SessionUpdate{Mode: &full}))
	k.until("full mode", func() bool { return c.Current.Mode == "full" })
	k.do(c.Prompt("mark it done"))
	k.until("the turn's end", func() bool { return !c.Busy && len(c.Entries) >= 3 && c.Changes.Stale })

	k.do(c.ListChanges())
	k.until("the changes", func() bool { return !c.Changes.Loading })
	if len(c.Changes.Turns) != 1 || len(c.Changes.Turns[0].Files) != 1 {
		t.Fatalf("%+v %s", c.Changes.Turns, c.Changes.Err)
	}
	turn, f := c.Changes.Turns[0].TurnSeq, c.Changes.Turns[0].Files[0]
	if f.Path != notes || f.Added != 1 || f.Removed != 1 {
		t.Errorf("%+v", f)
	}
	k.do(c.DiffChange(turn, f.Path))
	key := DiffKey(turn, f.Path)
	k.until("the diff", func() bool { _, ok := c.Changes.Diffs[key]; return ok })
	if d := c.Changes.Diffs[key]; d.Before == nil || *d.Before != "todo: ship\n" || d.After == nil || *d.After != "done: ship\n" {
		t.Errorf("%+v", d)
	}

	k.do(c.Revert(turn, "", false))
	k.until("the revert", func() bool { return c.Changes.Reverted[turn] })
	if b, _ := os.ReadFile(notes); string(b) != "todo: ship\n" || !c.Changes.Stale || len(c.Changes.Diffs) != 0 {
		t.Errorf("after revert: %q stale %v diffs %d", b, c.Changes.Stale, len(c.Changes.Diffs))
	}
	k.do(c.Revert(turn, "", true))
	k.until("the redo", func() bool { return !c.Changes.Reverted[turn] })
	if b, _ := os.ReadFile(notes); string(b) != "done: ship\n" {
		t.Errorf("after redo: %q", b)
	}

	// The log rebuilds which turns are reverted on reconnect.
	k.do(c.Revert(turn, "", false))
	k.until("the second revert", func() bool { return c.Changes.Reverted[turn] })
	c2 := newClient(dir, "u")
	k2 := dial(t, m, c2)
	k2.until("ready", func() bool { return c2.Phase == Ready })
	if !c2.Changes.Reverted[turn] {
		t.Error("reverted turn not rebuilt from the log")
	}
}

// TestRevertedFiles follows restores of single files and whole turns.
func TestRevertedFiles(t *testing.T) {
	c := newClient("/w", "t")
	c.resetChanges()
	c.Changes.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{{Path: "/w/a"}, {Path: "/w/b"}}}}
	restore := func(typ, path string) {
		data, _ := json.Marshal(proto.ChangeResult{TurnSeq: 3, Path: path})
		c.changesEvent(proto.Event{Type: typ, Data: data})
	}
	is := func(a, b bool) {
		t.Helper()
		if c.Changes.FileReverted(3, "/w/a") != a || c.Changes.FileReverted(3, "/w/b") != b {
			t.Fatalf("a %v b %v, want %v %v", c.Changes.FileReverted(3, "/w/a"), c.Changes.FileReverted(3, "/w/b"), a, b)
		}
	}
	restore(proto.EventChangesReverted, "/w/a")
	is(true, false)
	restore(proto.EventChangesReverted, "")
	is(true, true)
	restore(proto.EventChangesRedone, "/w/a")
	is(false, true)
	restore(proto.EventChangesRedone, "")
	is(false, false)
}

// A session not yet begun moves to another folder: the new one is shown,
// though the old one's deletion answers before the new one is read.
func TestNewSessionInAgainstKernel(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	other, _ := filepath.EvalSymlinks(t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "stratum.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := session.New(st, func(string) (llm.Provider, error) { return &llm.Script{}, nil })
	m.Defaults = func() (string, string) { return "script", "script" }
	defer m.Close()

	c := newClient(dir, "t")
	k := dial(t, m, c)
	k.until("ready", func() bool { return c.Phase == Ready })
	old := c.Current.ID
	drop, err := c.Delete(old)
	if err != nil {
		t.Fatal(err)
	}
	k.do(c.NewSessionIn(other))
	k.do(drop, nil)
	k.until("the new session", func() bool { return c.Phase == Ready && c.Current != nil && c.Current.Cwd == other })
	for _, s := range c.Sessions {
		if s.ID == old {
			t.Fatal("the empty session stayed")
		}
	}
}
