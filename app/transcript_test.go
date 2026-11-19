package app

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/llm"
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

// A file change asked about names the file from the workspace and shows
// what it would change.
func TestApprovalDiff(t *testing.T) {
	a := working()
	cwd := a.C.Current.Cwd
	args, _ := json.Marshal(map[string]string{"path": "add.go", "old_text": "\treturn a - b\n", "new_text": "\treturn a + b\n"})
	a.C.Approvals = []proto.Approval{{ID: "e", Tool: "edit_file", Kind: "write", Paths: []string{cwd + "/add.go"}, Args: args}}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	for _, want := range []string{"add.go", "\treturn a - b", "\treturn a + b"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText(cwd + "/add.go") {
		t.Error("the card shows the absolute path")
	}
}

func TestTurnCost(t *testing.T) {
	m := proto.Model{ID: "claude-sonnet-5-5", Context: 1000000, Input: 2, Output: 10, CacheRead: 0.1, CacheWrite: 2.5}
	u := llm.Usage{Input: 10000, Output: 2000, CacheRead: 4000, CacheWrite: 1000}
	// fresh = 5000 -> 0.01 + 0.02 + 0.0004 + 0.0025 = 0.0329
	got := costOf(m, u)
	if math.Abs(got-0.0329) > 1e-6 {
		t.Fatalf("costOf = %v", got)
	}
	if formatCost(0) != "" || formatCost(0.005) != "<$0.01" || formatCost(got) != "$0.03" {
		t.Fatalf("formatCost mismatch: %q %q %q", formatCost(0), formatCost(0.005), formatCost(got))
	}

	a := finishedApp()
	a.C.Current.Account = "anthropic"
	a.C.Current.Model = "claude-sonnet-5-5"
	a.C.Models = []proto.Model{m}
	if gotCost := a.turnCost(u); gotCost != "$0.03" {
		t.Fatalf("turnCost = %q, want $0.03", gotCost)
	}
}

func TestAllIconsParse(t *testing.T) {
	entries, err := iconFiles.ReadDir("icons")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded icons")
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".svg")
		if icon(name) == nil {
			t.Errorf("icon(%q) returned nil", name)
		}
	}
}

func TestMsgTime(t *testing.T) {
	a := task8App()
	base := time.Date(2026, 11, 19, 23, 0, 0, 0, testZone)
	a.now = func() time.Time { return base }

	t.Cleanup(func() { i18n.SetLocaleFunc(chinese) })
	i18n.SetLocaleFunc(chinese)
	if got := a.msgTime(base.Add(-30 * time.Second)); got != "刚刚" {
		t.Errorf("zh <1m: %q", got)
	}
	if got := a.msgTime(base.Add(-12 * time.Minute)); got != "12 分钟前" {
		t.Errorf("zh 12m: %q", got)
	}
	if got := a.msgTime(base.Add(-3 * time.Hour)); got != "3 小时前" {
		t.Errorf("zh 3h: %q", got)
	}
	if got := a.msgTime(time.Date(2026, 11, 10, 12, 0, 0, 0, testZone)); got != "11月10日" {
		t.Errorf("zh >1d: %q", got)
	}
	if got := a.msgTime(time.Date(2025, 11, 10, 12, 0, 0, 0, testZone)); got != "2025年11月10日" {
		t.Errorf("zh prev year: %q", got)
	}

	i18n.SetLocaleFunc(func() string { return "en" })
	if got := a.msgTime(base.Add(-12 * time.Minute)); got != "12m ago" {
		t.Errorf("en 12m: %q", got)
	}
	if got := a.msgTime(base.Add(-3 * time.Hour)); got != "3h ago" {
		t.Errorf("en 3h: %q", got)
	}
	if got := a.msgTime(time.Date(2026, 11, 10, 12, 0, 0, 0, testZone)); got != "Nov 10" {
		t.Errorf("en >1d: %q", got)
	}
}

func TestRetryTurn(t *testing.T) {
	a := finishedApp()
	a.C.Turns[0].End = a.now().Add(-12 * time.Minute)
	tt := task8Tester(a, false)
	drain(a)
	if !tt.HasText("12 分钟前") {
		t.Fatalf("missing timestamp in %q", tt.Texts())
	}
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	var rw proto.Rewind
	task8Command(t, a, proto.MethodRewind, &rw)
	if rw.TurnSeq != 1 || rw.Action != "conversation" {
		t.Fatalf("unexpected rewind: %+v", rw)
	}
}
