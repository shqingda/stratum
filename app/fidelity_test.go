package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

// The fixtures here hold the content of the screens in
// design/mockups.html, so the app can be shot beside them
// (design/fidelity/shoot.sh) and the two compared.

func str(s string) *string { return &s }

// screen1 is the first mockup: a session at work, dark, with the review
// panel open on add.go.
func screen1() *App {
	a := working()
	c := a.C
	c.Sessions = []proto.Session{
		c.Sessions[0],
		{ID: "2", Title: "沙箱 profile 收紧到只读 .git", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-2 * time.Minute)},
		{ID: "3", Title: "client 按日志重建", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-3 * time.Hour)},
		{ID: "4", Title: "协议定稿：回执与快照", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-26 * time.Hour)},
		{ID: "6", Title: "models.dev 目录合并", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-27 * time.Hour)},
		{ID: "7", Title: "给 Sub 补表驱动测试", Cwd: "/Users/me/Projects/calc", Updated: now.Add(-72 * time.Hour)},
		{ID: "5", Title: "首页文案改写", Cwd: "/Users/me/Projects/site", Updated: now.Add(-73 * time.Hour)},
		{ID: "8", Title: "把 OG 图换成新标志", Cwd: "/Users/me/Projects/site", Updated: now.Add(-30 * 24 * time.Hour)},
	}
	c.Current = &c.Sessions[0]
	c.Marks = map[string]string{"2": "run", "3": "unread", "6": "fail"}
	edit := func(path, old, new string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"path": path, "old_text": old, "new_text": new})
		return b
	}
	c.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "Add 的测试挂了，帮我修一下；顺便看看 README 里的示例对不对", Done: true},
		{Key: 20, Role: client.Thinking, Text: "先读 add.go 和测试，再看 README 的示例。", Done: true, Took: 6 * time.Second},
		{Key: 2, Role: client.Tool, Text: "read_file", Detail: "add.go", CallID: "a", Done: true},
		{Key: 3, Role: client.Tool, Text: "read_file", Detail: "add_test.go", CallID: "b", Done: true},
		{Key: 4, Role: client.Tool, Text: "search", Detail: "Add(", CallID: "c", Done: true},
		{Key: 5, Role: client.Assistant, Text: "`Add` 把 `a + b` 写成了 `a - b`，测试期望 `Add(2, 3) == 5`。README 里的示例输出也跟着写错了。", Done: true},
		{Key: 6, Role: client.Tool, Text: "edit_file", Detail: "add.go", CallID: "d", Done: true,
			Args:   edit("add.go", "func Add(a, b int) int { return a - b }", "func Add(a, b int) int { return a + b }"),
			Output: "Edited add.go (1 replacement)"},
		{Key: 7, Role: client.Tool, Text: "edit_file", Detail: "README.md", CallID: "f", Done: true,
			Args:   edit("README.md", "// Output: -1", "// Output: 5"),
			Output: "Edited README.md (1 replacement)"},
		{Key: 8, Role: client.Tool, Text: "shell", Detail: "go test ./... -run TestAdd -v", CallID: "e", Args: c.Approvals[0].Args},
	}
	before := "package calc\n\nfunc Add(a, b int) int { return a - b }\n\n// Sub returns a minus b.\nfunc Sub(a, b int) int { return a - b }\n"
	readme := "# calc\n\nA tiny calculator.\n\n## Install\n\n    go get calc\n\n## Use\n\nAdd two numbers:\n\n```go\nfmt.Println(calc.Add(2, 3))\n// Output: -1\n```\n"
	ch := &c.Changes
	ch.Stale = false
	ch.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{
		{Path: "add.go", Added: 1, Removed: 1},
		{Path: "README.md", Added: 1, Removed: 1},
		{Path: "internal/sub_test.go", Added: 6},
	}}}
	ch.Diffs = map[string]proto.ChangeDiff{
		client.DiffKey(3, "add.go"):    {Path: "add.go", Before: str(before), After: str(replace(before, "a - b }\n\n//", "a + b }\n\n//"))},
		client.DiffKey(3, "README.md"): {Path: "README.md", Before: str(readme), After: str(replace(readme, "-1", "5"))},
		client.DiffKey(3, "internal/sub_test.go"): {Path: "internal/sub_test.go", After: str(
			"package internal\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {\n}\n")},
	}
	a.rv.open = true
	a.rv.sel = client.DiffKey(3, "add.go")
	c.Models = []proto.Model{{ID: "gpt-6-luna", Context: 200000, Reasoning: true}}
	c.LastInput = 36000
	c.Workspace.Branch, c.Workspace.Dirty, c.Workspace.Added, c.Workspace.Removed = "fix-add", true, 9, 3
	return a
}

// screen2 is the second mockup: the same session done, light, its work
// folded open, the turn's changes and cost under its answer.
func screen2() *App {
	a := screen1()
	c := a.C
	a.rv = review{}
	c.Busy, c.Approvals, c.Queued = false, nil, nil
	c.Marks = map[string]string{"2": "run"}
	c.Sessions = []proto.Session{
		{ID: "1", Title: "修复 Add 测试", Cwd: "/Users/me/Projects/calc", Model: "gpt-6-luna", Effort: "medium", Mode: "ask", Updated: now.Add(-10 * time.Second)},
		{ID: "2", Title: "沙箱 profile 收紧到只读 .git", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-2 * time.Minute)},
		{ID: "4", Title: "协议定稿：回执与快照", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-26 * time.Hour)},
		{ID: "7", Title: "给 Sub 补表驱动测试", Cwd: "/Users/me/Projects/calc", Updated: now.Add(-72 * time.Hour)},
		{ID: "5", Title: "首页文案改写", Cwd: "/Users/me/Projects/site", Updated: now.Add(-73 * time.Hour)},
	}
	c.Current = &c.Sessions[0]
	c.Accounts[0].Email = "还剩 71% · 4 时后重置"
	c.Turns = []client.Turn{{Seq: 3, Start: now.Add(-48 * time.Second), End: now.Add(-10 * time.Second), Done: true,
		Usage: llm.Usage{Input: 4100, Output: 182, CacheRead: 3200}}}
	shell, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	es := c.Entries[:8]
	es[4] = &client.Entry{Key: 9, Role: client.Tool, Text: "read_file", Detail: "README.md", CallID: "g", Done: true}
	es[5], es[6], es[7] = c.Entries[6], c.Entries[7], &client.Entry{Key: 8, Role: client.Tool, Text: "shell", Detail: "go test ./...", CallID: "e", Done: true, Args: shell,
		Output: "=== RUN   TestAdd\n--- PASS: TestAdd (0.00s)\nok  \tcalc\t0.412s\n"}
	c.Entries = append(es, &client.Entry{Key: 10, Role: client.Assistant, Done: true, Text: "修好了。`Add` 原来返回的是 `a - b`，改回 `a + b` 后测试全部通过：\n\n" +
		"```输出\n=== RUN   TestAdd\n--- PASS: TestAdd (0.00s)\nok      calc    0.412s\n```\n\n另外两处顺手改了：\n\n" +
		"- README 的示例输出从 `-1` 改为 `5`。\n- `Sub` 没有测试，我没动它；需要的话可以补一组表驱动测试。"})
	c.Changes.Turns = []proto.ChangeTurn{{TurnSeq: 3, Files: []proto.ChangeFile{{Path: "add.go", Added: 1, Removed: 1}, {Path: "README.md", Added: 2, Removed: 2}}}}
	a.draft = "再给 @sub.go 补一组表驱动测试"
	a.opened["work:20"] = true
	c.LastInput = 42000
	return a
}

// screen3 is the third mockup's left window: a new session, light, the
// sidebar hidden.
func screen3() *App {
	a := screen2()
	a.C.Entries, a.C.Turns, a.C.Changes.Turns = nil, nil, nil
	a.C.Sessions[0] = proto.Session{ID: "9", Title: "", Cwd: "/Users/me/Projects/calc", Model: "gpt-6-luna", Effort: "medium", Mode: "ask", Updated: now}
	a.C.Sessions = append(a.C.Sessions, proto.Session{ID: "1", Title: "修复 Add 测试", Cwd: "/Users/me/Projects/calc", Updated: now.Add(-10 * time.Second)})
	a.C.Current = &a.C.Sessions[0]
	a.draft = ""
	a.side.noSide = true
	return a
}

// screen4 is the third mockup's right window: ⌘K over a session, dark.
func screen4() *App {
	a := screen2()
	a.draft = ""
	a.C.Sessions = append(a.C.Sessions, proto.Session{ID: "11", Title: "测试里跑 go test 要进沙箱", Cwd: "/Users/me/Projects/stratum", Updated: now.Add(-8 * 24 * time.Hour)})
	a.pal.open, a.search.query = true, "沙箱"
	return a
}

func replace(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	panic("no " + old)
}

func TestFidelity(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  func() *App
		w, h int
		dark bool
	}{
		{"screen1", screen1, 1440, 880, true},
		{"screen2", screen2, 1200, 860, false},
		{"screen3", screen3, 700, 600, false},
		{"screen4", screen4, 700, 600, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := ui.NewTester(tc.app().View, tc.w, tc.h)
			tt.SetScale(2)
			tt.SetDark(tc.dark)
			tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
			tt.Frame()
			tt.Advance(2 * time.Second)
			tt.Frame()
			snapshot(t, tt, "fid-"+tc.name)
		})
	}
}
