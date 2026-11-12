package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/boot"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
)

// TestAgainstKernel drives the window against a real kernel answering
// from a script: type a prompt, allow a write, see the reply.
func TestAgainstKernel(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("todo: ship\n"), 0o644)
	script := &llm.Script{Turns: []llm.Turn{
		{Text: "我先看看。", Calls: []llm.ToolCall{llm.Call("c1", "read_file", map[string]string{"path": "notes.txt"})}},
		{Calls: []llm.ToolCall{llm.Call("c2", "write_file", map[string]string{"path": "notes.txt", "content": "done: ship\n"})}},
		{Text: "已经标成 **done**。"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k, err := boot.Start(ctx, boot.Options{DB: filepath.Join(t.TempDir(), "talos.db"), Creds: &auth.Memory{}, Script: script})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	a := New(client.New(dir))
	updates := make(chan func(), 1024)
	a.Serve(ctx, k.Server, func(fn func()) { updates <- fn })
	tt := ui.NewTester(a.View, 1200, 780)
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for !cond() {
			select {
			case fn := <-updates:
				fn()
				tt.Frame()
			case <-deadline:
				t.Fatalf("waiting for %s: %q", what, tt.Texts())
			}
		}
	}
	until("the session", func() bool { return a.C.CanAct() })

	tt.Type("把 todo 标成完成")
	tt.Key(0, ui.KeyEnter)
	until("the approval", func() bool { return tt.HasText("需要你允许") })
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	until("the reply", func() bool { return !a.C.Busy && tt.HasText("已经") })
	for _, want := range []string{"把 todo 标成完成", "读取", "写入", "notes.txt"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "done: ship\n" {
		t.Errorf("notes.txt = %q", b)
	}
	tt.Advance(2 * time.Second) // let the reply fade in
	tt.Frame()
	snapshot(t, tt, "kernel")
}
