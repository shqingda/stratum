package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

// finishedApp has a finished turn that thought, looked at two files, had
// a command declined, answered and ran out of steps.
func finishedApp() *App {
	a := task8App()
	a.C.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "看看 README 的示例", Done: true},
		{Key: 2, Role: client.Thinking, Text: "先读一下两个文件", Done: true},
		{Key: 3, Role: client.Tool, Text: "read_file", Detail: "add.go", CallID: "a", Done: true, Output: "package calc"},
		{Key: 4, Role: client.Tool, Text: "read_file", Detail: "add_test.go", CallID: "b", Done: true, Output: "package calc"},
		{Key: 5, Role: client.Tool, Text: "shell", Detail: "go test ./...", CallID: "c", Done: true, Skipped: true, Output: "The user declined this call."},
		{Key: 6, Role: client.Assistant, Text: "README 的示例写错了。", Done: true},
		{Key: 7, Role: client.Notice, Note: "limit", Text: "步数用完了，先停在这里。", Done: true},
	}
	a.C.Turns = []client.Turn{{Seq: 1, Done: true}}
	return a
}

func TestDetail(t *testing.T) {
	a := finishedApp()
	tt := task8Tester(a, false)
	if !tt.HasText("工作了") || tt.HasText("先读一下两个文件") || tt.HasText("已跳过") {
		t.Fatalf("plain shows %q", tt.Texts())
	}
	a.C.Settings = &proto.Settings{Detail: "thinking"}
	tt.Frame()
	if tt.HasText("工作了") || !tt.HasText("先读一下两个文件") || !tt.HasText("已跳过") || tt.HasText("add_test.go") {
		t.Fatalf("with thinking shows %q", tt.Texts())
	}
	a.C.Settings.Detail = "all"
	tt.Frame()
	if !tt.HasText("add_test.go") {
		t.Fatalf("everything shows %q", tt.Texts())
	}
}

// A turn out of steps goes on with one click.
func TestStepLimit(t *testing.T) {
	a := finishedApp()
	tt := task8Tester(a, false)
	drain(a)
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	var p proto.Prompt
	task8Command(t, a, proto.MethodPrompt, &p)
	if p.Text != "继续" {
		t.Fatalf("sent %+v", p)
	}
}

func TestFind(t *testing.T) {
	a := finishedApp()
	tt := task8Tester(a, false)
	drain(a)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	tt.Type("readme")
	tt.Frame()
	if !tt.HasText("1/2") {
		t.Fatalf("hits %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("2/2") {
		t.Fatalf("next %q", tt.Texts())
	}
	tt.Type("x")
	tt.Frame()
	if !tt.HasText("0/0") {
		t.Fatalf("no hits %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.find.on {
		t.Fatal("Escape left the find open")
	}
	task8NoCommand(t, a)
}

func TestPlain(t *testing.T) {
	got := plain("## 结论\n\n`Add` 写成了 **a - b**，见 [add.go](add.go)。\n\n```go\nreturn a + b\n```\n")
	if want := "结论\n\nAdd 写成了 a - b，见 add.go。\n\nreturn a + b"; got != want {
		t.Fatalf("plain = %q", got)
	}
}

func drain(a *App) {
	for _, ok := next(a); ok; _, ok = next(a) {
	}
}

func TestApprovalRules(t *testing.T) {
	a := task8App()
	a.C.Busy = true
	a.C.Approvals = []proto.Approval{{ID: "x", Tool: "shell", Kind: "exec", Command: "go test ./...", Rule: goTest}}
	tt := task8Tester(a, false)
	drain(a)
	if !tt.HasText("放行：以 go test 开头的命令") {
		t.Fatalf("no rule shown: %q", tt.Texts())
	}
	if err := tt.Click("改命令"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type(" -run TestAdd")
	tt.Key(0, ui.KeyEnter)
	var ans proto.ApprovalAnswer
	task8Command(t, a, proto.MethodApprovalRespond, &ans)
	if !ans.Allow || ans.Command != "go test ./... -run TestAdd" || a.editing != "" {
		t.Fatalf("answer %+v, editing %q", ans, a.editing)
	}

	delete(a.C.Answering, "x")
	tt.Frame()
	if err := tt.Click("总是允许"); err != nil {
		t.Fatal(err)
	}
	task8Command(t, a, proto.MethodApprovalRespond, &ans)
	if !ans.Allow || !ans.Forever {
		t.Fatalf("answer %+v", ans)
	}

	// Esc while changing the command puts it back, and declines nothing.
	delete(a.C.Answering, "x")
	tt.Frame()
	tt.Click("改命令")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	task8NoCommand(t, a)
	if a.editing != "" {
		t.Fatal("still editing")
	}
}
