package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/boot"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

const addGo = `package calc

// Add returns the sum of a and b.
func Add(a, b int) int { return a - b }

// Sub returns a minus b.
func Sub(a, b int) int { return a - b }
`

// TestReviewAgainstKernel shows a turn's changes in the review panel,
// takes them back and redoes them.
func TestReviewAgainstKernel(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	os.WriteFile(filepath.Join(dir, "add.go"), []byte(addGo), 0o644)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "calc"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git: %s %v", out, err)
		}
	}
	script := &llm.Script{Turns: []llm.Turn{
		{Calls: []llm.ToolCall{
			llm.Call("c1", "edit_file", map[string]string{"path": "add.go", "old_text": "return a - b }\n\n// Sub", "new_text": "return a + b }\n\n// Sub"}),
			llm.Call("c2", "write_file", map[string]string{"path": "internal/sub_test.go", "content": "package calc\n\nfunc TestSub(t *testing.T) {}\n"}),
		}},
		{Text: "改好了。"},
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
	tt := ui.NewTester(a.View, 1440, 880)
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for tt.Frame(); !cond(); tt.Frame() {
			select {
			case fn := <-updates:
				fn()
			case <-deadline:
				t.Fatalf("waiting for %s: %q", what, tt.Texts())
			}
		}
	}
	until("the session", func() bool { return a.C.CanAct() })
	full := "full"
	a.do(a.C.Update(proto.SessionUpdate{Mode: &full}))
	until("full mode", func() bool { return a.C.Current.Mode == "full" })
	tt.Type("修复 Add")
	tt.Key(0, ui.KeyEnter)
	until("the reply", func() bool { return !a.C.Busy && tt.HasText("改好") })

	if err := tt.Click("显示变更"); err != nil {
		t.Fatal(err)
	}
	until("the diffs", func() bool { return tt.HasText("@@ -1,7 +1,7 @@") && len(a.C.Changes.Diffs) == 2 })
	if !tt.HasText("A") {
		t.Error("the new file is not marked A")
	}
	for _, want := range []string{"add.go", "sub_test.go", "internal/", "撤销本轮", "func Add(a, b int) int { return a + b }"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	snapshot(t, tt, "review-kernel")

	if err := tt.Click("撤销本轮"); err != nil {
		t.Fatal(err)
	}
	until("the revert", func() bool { return tt.HasText("重做本轮") })
	if b, _ := os.ReadFile(filepath.Join(dir, "add.go")); string(b) != addGo {
		t.Errorf("add.go after revert:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/sub_test.go")); !os.IsNotExist(err) {
		t.Errorf("sub_test.go after revert: %v", err)
	}
	snapshot(t, tt, "review-reverted")

	if err := tt.Click("重做本轮"); err != nil {
		t.Fatal(err)
	}
	until("the redo", func() bool { return tt.HasText("撤销本轮") })
	if b, _ := os.ReadFile(filepath.Join(dir, "add.go")); !strings.Contains(string(b), "return a + b }\n\n// Sub") {
		t.Errorf("add.go after redo:\n%s", b)
	}

	until("the diffs again", func() bool { return tt.HasText("撤销此块") && len(a.C.Changes.Diffs) == 2 })
	snapshot(t, tt, "review-hunk")
	if err := tt.Click("撤销此块"); err != nil {
		t.Fatal(err)
	}
	until("the hunk taken back", func() bool { return tt.HasText("内容没有变化") })
	if b, _ := os.ReadFile(filepath.Join(dir, "add.go")); string(b) != addGo {
		t.Errorf("add.go after taking back its hunk:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/sub_test.go")); err != nil {
		t.Errorf("the other file went too: %v", err)
	}

	if err := tt.Click("未提交"); err != nil {
		t.Fatal(err)
	}
	until("the uncommitted files", func() bool {
		return len(a.C.Workspace.Files) == 1 && tt.HasText("func TestSub(t *testing.T) {}")
	})
	if tt.HasText("撤销此块") || !tt.HasText("提交改动") {
		t.Errorf("uncommitted view: %q", tt.Texts())
	}
	snapshot(t, tt, "review-uncommitted")

	// Against the branch's base, the new file is still a change.
	if err := tt.Click("对 main"); err != nil {
		t.Fatal(err)
	}
	until("the branch's files", func() bool { return a.C.Workspace.Base == "main" && tt.HasText("func TestSub(t *testing.T) {}") })

	var opened string
	a.openIn = func(app, path string) error { opened = path; return nil }
	if err := tt.Click("在编辑器里打开"); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "internal/sub_test.go"); opened != want {
		t.Errorf("opened %q, want %q", opened, want)
	}
	if err := tt.Click("internal/sub_test.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("func TestSub(t *testing.T) {}") {
		t.Error("the folded file still shows its diff")
	}
	tt.Click("internal/sub_test.go")
	tt.Frame()

	// A remark on a line, sent with the others as one prompt.
	r, ok := tt.Find("func TestSub(t *testing.T) {}")
	if !ok {
		t.Fatalf("no line to remark on: %q", tt.Texts())
	}
	tt.Move(r.X, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("批注这一行"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("测试名改成 TestSubtract")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("测试名改成 TestSubtract") || !tt.HasText("1 条批注") {
		t.Fatalf("the remark is not kept: %q", tt.Texts())
	}
	snapshot(t, tt, "review-remark")
	if err := tt.Click("发给 agent"); err != nil {
		t.Fatal(err)
	}
	until("the remarks sent", func() bool {
		for _, e := range a.C.Entries {
			if e.Role == client.User && strings.Contains(e.Text, "`internal/sub_test.go:3`") && strings.Contains(e.Text, "测试名改成 TestSubtract") {
				return true
			}
		}
		return false
	})
	if len(a.rv.remarks) != 0 {
		t.Errorf("remarks left after sending: %v", a.rv.remarks)
	}
}
