package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
)

func TestLineDiff(t *testing.T) {
	for _, tc := range []struct {
		name, old, next string
		want            []diffLine
	}{
		{"empty", "", "", nil},
		{"add", "", "a\nb\n", []diffLine{{"a\n", "+", 1}, {"b\n", "+", 2}}},
		{"delete", "a\nb\n", "", []diffLine{{"a\n", "−", 1}, {"b\n", "−", 2}}},
		{"middle", "a\nb\nc\n", "a\nx\nc\n", []diffLine{{"a\n", "", 1}, {"b\n", "−", 2}, {"x\n", "+", 2}, {"c\n", "", 3}}},
		{"ends", "a\nb\nc\n", "x\nb\ny\n", []diffLine{{"a\n", "−", 1}, {"x\n", "+", 1}, {"b\n", "", 2}, {"c\n", "−", 3}, {"y\n", "+", 3}}},
		{"repeated", "a\nb\na\n", "a\na\n", []diffLine{{"a\n", "", 1}, {"b\n", "−", 2}, {"a\n", "", 2}}},
		{"crlf", "a\r\nb\r\n", "a\nb\n", []diffLine{{"a\n", "", 1}, {"b\n", "", 2}}},
		{"no-final-newline", "a\nb", "a\nc", []diffLine{{"a\n", "", 1}, {"b", "−", 2}, {"c", "+", 2}}},
		{"newline-added", "a", "a\n", []diffLine{{"a", "−", 1}, {"a\n", "+", 1}}},
		{"blank-lines", "\n\n", "\n", []diffLine{{"\n", "", 1}, {"\n", "−", 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, replaced := lineDiff(tc.old, tc.next)
			if replaced || !slices.Equal(got, tc.want) {
				t.Errorf("diff = %#v, replaced = %v; want %#v", got, replaced, tc.want)
			}
		})
	}
	for _, n := range []int{1000, 1001} {
		text := strings.Repeat("line\n", n)
		got, replaced := lineDiff(text, text)
		if replaced != (n == 1001) || len(got) != map[bool]int{false: n, true: 2 * n}[replaced] {
			t.Fatalf("%d + %d lines: len=%d, replaced=%v", n, n, len(got), replaced)
		}
		if replaced && (got[0].sign != "−" || got[n].sign != "+" || got[len(got)-1].number != n) {
			t.Errorf("whole replacement bounds: %+v, %+v, %+v", got[0], got[n], got[len(got)-1])
		}
	}
}

func TestShellTail(t *testing.T) {
	out := ""
	for i := 1; i <= 24; i++ {
		out += fmt.Sprintf("line %d\n", i)
	}
	for _, tc := range []struct {
		output, first, last, exit string
		omitted, count            int
	}{
		{out, "line 5", "line 24", "", 4, 20},
		{out + "[exit status 2]", "line 6", "[exit status 2]", "2", 5, 20},
		{"[exit status nope]", "[exit status nope]", "[exit status nope]", "", 0, 1},
		{"(no output)", "(no output)", "(no output)", "", 0, 1},
		{"a\n\n", "a", "", "", 0, 2},
		{"", "", "", "", 0, 1},
	} {
		lines, omitted, exit := shellTail(tc.output)
		if len(lines) != tc.count || lines[0] != tc.first || lines[len(lines)-1] != tc.last || omitted != tc.omitted || exit != tc.exit {
			t.Errorf("tail(%q) = %q, %d, %q", tc.output, lines, omitted, exit)
		}
	}
}

func TestToolResultFormats(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args, output string
		count                    int
		ok                       bool
	}{
		{"search", "search", `{"pattern":"Add"}`, "a.go:3:func Add() {}\nb.go:2:Add()\n", 2, true},
		{"colon-path", "search", `{"pattern":"Add"}`, "my:file.go:3:Add()\n", 1, true},
		{"single-file", "search", `{"pattern":"Add","path":"a.go"}`, "3:Add()\n", 1, true},
		{"files-only", "search", `{"pattern":"Add","files":true}`, "a.go\nb.go\n", 2, true},
		{"no-matches", "search", `{"pattern":"Add"}`, "No matches.", 0, true},
		{"list", "list_dir", `{"path":"."}`, "a.go\ndir/\n", 2, true},
		{"empty-dir", "list_dir", `{"path":"."}`, "(empty directory)", 0, true},
		{"more", "list_dir", `{"path":"."}`, strings.Repeat("file\n", 1000) + "[5 more]\n", 1005, true},
		{"marker-filename", "list_dir", `{"path":"."}`, "a.go\n[5 more]\n", 2, true},
		{"bad-args", "search", `{broken`, "a.go:1:a\n", 0, false},
		{"missing-pattern", "search", `{}`, "a.go:1:a\n", 0, false},
		{"bad-output", "search", `{"pattern":"Add"}`, "unparseable", 0, false},
		{"zero-line", "search", `{"pattern":"Add"}`, "a.go:0:Add", 0, false},
		{"empty-output", "list_dir", `{"path":"."}`, "", 0, false},
		{"clipped", "search", `{"pattern":"Add"}`, "a.go:1:a\n\n[… 42 bytes cut …]\n\na.go:30:a\n", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, count, ok := resultRows(&client.Entry{Text: tc.tool, Args: json.RawMessage(tc.args), Output: tc.output})
			if count != tc.count || ok != tc.ok {
				t.Errorf("count=%d ok=%v; want %d %v", count, ok, tc.count, tc.ok)
			}
		})
	}
	for _, tc := range []struct {
		output string
		want   int
	}{
		{"Edited a (b).go (1 replacement)", 1},
		{"Edited a (b).go (3 replacements)", 3},
		{"Edited a (b).go (3 replacement)", 0},
		{"Edited a (b).go (0 replacements)", 0},
		{"Edited a (b).go (1 replacement) trailing", 0},
		{"Edited another.go (1 replacement)", 0},
	} {
		if got := editReplacements(tc.output, "a (b).go"); got != tc.want {
			t.Errorf("editReplacements(%q)=%d, want %d", tc.output, got, tc.want)
		}
	}
}

func cardEntry(tool string, args any, output string) *client.Entry {
	raw, _ := json.Marshal(args)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	detail := ""
	for _, key := range []string{"path", "command", "pattern"} {
		if text, ok := fields[key].(string); ok && text != "" {
			detail = text
			break
		}
	}
	return &client.Entry{Key: 1, CallID: "card-test", Role: client.Tool, Text: tool, Args: raw, Detail: detail, Output: output, Done: true}
}

func cardApp(e *client.Entry) *App {
	a := working()
	a.C.Entries = []*client.Entry{e}
	a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, false
	return a
}

func TestToolCardFallbacks(t *testing.T) {
	valid := `{"path":"a.go","old_text":"a\n","new_text":"b\n"}`
	for _, tc := range []struct {
		name, tool, args, output string
		done, failed             bool
	}{
		{"unknown", "unknown_tool", `{}`, "raw output", true, false},
		{"bad-args", "edit_file", `{broken`, "raw output", true, false},
		{"null-args", "list_dir", `null`, "raw output", true, false},
		{"wrong-field-type", "write_file", `{"path":"a.go","content":3}`, "raw output", true, false},
		{"missing-content", "write_file", `{"path":"a.go"}`, "raw output", true, false},
		{"empty-output", "edit_file", valid, "", true, false},
		{"failed-edit", "edit_file", valid, "old_text not found", true, true},
		{"running-edit", "edit_file", valid, "", false, false},
		{"bad-result", "edit_file", valid, "raw output", true, false},
		{"bad-write-result", "write_file", `{"path":"a.go","content":"x"}`, "Wrote a.go (999 lines)", true, false},
		{"bad-search-result", "search", `{"pattern":"Add"}`, "raw output", true, false},
		{"failed-list", "list_dir", `{"path":"."}`, "permission denied", true, true},
		{"bad-shell-args", "shell", `{"command":3}`, "raw output", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &client.Entry{Key: 1, CallID: tc.name, Role: client.Tool, Text: tc.tool, Detail: "sample", Args: json.RawMessage(tc.args), Output: tc.output, Done: tc.done, Failed: tc.failed}
			a := cardApp(e)
			a.opened[e.CallID] = tc.output != ""
			tt := ui.NewTester(a.View, 1200, 780)
			if tt.HasText("撤销") || tt.HasText("新写入") || tt.HasText("+1") {
				t.Fatalf("fallback rendered a success card: %q", tt.Texts())
			}
			if tc.output != "" && !tt.HasText(tc.output) {
				t.Errorf("raw output missing: %q", tt.Texts())
			}
		})
	}
}

func TestToolCardViews(t *testing.T) {
	long := ""
	for i := 1; i <= 16; i++ {
		long += fmt.Sprintf("line %02d\n", i)
	}
	shell := long + "line 17\nline 18\nline 19\nline 20\nline 21\nline 22\nline 23\nline 24\n[exit status 1]"
	for _, tc := range []struct {
		name  string
		entry *client.Entry
		open  bool
		want  []string
	}{
		{"edit", cardEntry("edit_file", map[string]string{"path": "add.go", "old_text": "func Add(a, b int) int { return a - b }\n", "new_text": "func Add(a, b int) int { return a + b }\n"}, "Edited add.go (1 replacement)"), false, []string{"add.go", "+1", "−1", "撤销"}},
		{"write", cardEntry("write_file", map[string]string{"path": "README.md", "content": "# calc\n\nAdd(2, 3) == 5\n"}, "Created README.md (4 lines)"), false, []string{"新写入", "+3", "−0"}},
		{"write-empty", cardEntry("write_file", map[string]string{"path": "empty.txt", "content": ""}, "Wrote empty.txt (1 lines)"), false, []string{"新写入", "+0", "−0"}},
		{"replace-all", cardEntry("edit_file", map[string]any{"path": "a.go", "old_text": "foo", "new_text": "bar", "replace_all": true}, "Edited a.go (2 replacements)"), false, []string{"同一片段替换 2 处 · 下方显示一处", "+1", "−1"}},
		{"line-endings", cardEntry("edit_file", map[string]string{"path": "a.go", "old_text": "package calc\r\n\r\n", "new_text": "package calc\n\n"}, "Edited a.go (1 replacement)"), false, []string{"换行格式已变更", "+0", "−0", "package calc"}},
		{"shell", cardEntry("shell", map[string]string{"command": "go test ./..."}, "=== RUN TestAdd\n--- PASS: TestAdd\nok calc\n"), true, []string{"ok calc"}},
		{"shell-failed", cardEntry("shell", map[string]string{"command": "go test ./..."}, shell), true, []string{"退出码 1", "… 已省略前 5 行", "[exit status 1]"}},
		{"shell-empty", cardEntry("shell", map[string]string{"command": "true"}, "(no output)"), true, []string{"(no output)"}},
		{"search", cardEntry("search", map[string]string{"pattern": "Add"}, "add.go:3:func Add(a, b int) int { return a + b }\nadd_test.go:8:Add(2, 3)\n"), true, []string{"2 个结果", "add_test.go:8:Add(2, 3)"}},
		{"search-empty", cardEntry("search", map[string]string{"pattern": "missing"}, "No matches."), true, []string{"0 个结果", "没有匹配结果"}},
		{"list", cardEntry("list_dir", map[string]string{"path": "."}, "add.go\nadd_test.go\ninternal/\n"), true, []string{"3 个结果", "internal/"}},
		{"list-empty", cardEntry("list_dir", map[string]string{"path": "."}, "(empty directory)"), true, []string{"0 个结果", "目录为空"}},
		{"diff-collapsed", cardEntry("write_file", map[string]string{"path": "lines.txt", "content": long}, "Created lines.txt (17 lines)"), false, []string{"展开其余 4 行"}},
		{"diff-expanded", cardEntry("write_file", map[string]string{"path": "lines.txt", "content": long}, "Created lines.txt (17 lines)"), true, []string{"line 16", "收起"}},
	} {
		for _, dark := range []bool{false, true} {
			name := "t7-" + tc.name + map[bool]string{false: "-light", true: "-dark"}[dark]
			t.Run(name, func(t *testing.T) {
				a := cardApp(tc.entry)
				a.opened[tc.entry.CallID] = tc.open
				tt := ui.NewTester(a.View, 1200, 780)
				tt.SetDark(dark)
				tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
				tt.Frame()
				for _, want := range tc.want {
					if !tt.HasText(want) {
						t.Errorf("missing %q: %q", want, tt.Texts())
					}
				}
				snapshot(t, tt, name)
			})
		}
	}
}

func TestToolCardInteractions(t *testing.T) {
	e := cardEntry("write_file", map[string]string{"path": "a.txt", "content": strings.Repeat("line\n", 13)}, "Created a.txt (14 lines)")
	a := cardApp(e)
	tt := ui.NewTester(a.View, 1200, 780)
	if err := tt.Click("展开其余 1 行"); err != nil {
		t.Fatal(err)
	}
	if !a.opened[e.CallID] || !tt.HasText("收起") {
		t.Fatal("diff did not expand")
	}
	if err := tt.Click("收起"); err != nil {
		t.Fatal(err)
	}
	if a.opened[e.CallID] || !tt.HasText("展开其余 1 行") {
		t.Fatal("diff did not collapse")
	}
	if undo, ok := tt.Access("撤销 a.txt"); !ok || !undo.Disabled {
		t.Fatalf("unwired undo must be disabled: %+v, %v", undo, ok)
	}
	for _, tool := range []string{"shell", "search", "list_dir"} {
		t.Run(tool, func(t *testing.T) {
			e := cardEntry(tool, map[string]any{"path": ".", "command": "echo a", "pattern": "a"}, "a.go:1:a\n")
			a := cardApp(e)
			tt := ui.NewTester(a.View, 1200, 780)
			label := tools[tool].verb + " " + e.Detail
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			if !a.opened[e.CallID] || !tt.HasText("a.go:1:a") {
				t.Fatal("output did not expand")
			}
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			if a.opened[e.CallID] || tt.HasText("a.go:1:a") {
				t.Fatal("output did not collapse")
			}
		})
	}
}

func TestToolLargeDiffAndCache(t *testing.T) {
	content := strings.Repeat("line\n", 2001)
	e := cardEntry("write_file", map[string]string{"path": "large.txt", "content": content}, "Created large.txt (2002 lines)")
	a := cardApp(e)
	tt := ui.NewTester(a.View, 1200, 780)
	if !tt.HasText("新写入 · 整体替换（超过 2000 行）") || !tt.HasText("展开其余 1989 行") {
		t.Fatal("large diff was not bounded and labeled")
	}
	if err := tt.Click("展开其余 1989 行"); err != nil {
		t.Fatal(err)
	}
	if !a.opened[e.CallID] || !tt.HasText("2001") {
		t.Fatal("large diff did not expand completely")
	}
	// Reusing a row with changed arguments must invalidate its local diff.
	e.Args = json.RawMessage(`{"path":"large.txt","content":"updated\n"}`)
	e.Output = "Wrote large.txt (2 lines)"
	tt.Frame()
	if !tt.HasText("updated") || tt.HasText("+2001") || !tt.HasText("+1") {
		t.Fatalf("stale cached diff: %q", tt.Texts())
	}
}

func TestToolCardDimensions(t *testing.T) {
	e := cardEntry("edit_file", map[string]string{"path": "a.go", "old_text": "before\n", "new_text": "after\n"}, "Edited a.go (1 replacement)")
	a := cardApp(e)
	tt := ui.NewTester(a.View, 1200, 780)
	number, ok := tt.Find("1")
	if !ok {
		t.Fatal("no line number")
	}
	minus, _ := tt.Find("−")
	plus, _ := tt.Find("+")
	before, _ := tt.Find("before")
	after, _ := tt.Find("after")
	undo, _ := tt.Find("撤销 a.go")
	if number.W != 34 || minus.W != 14 || minus.X-number.X != 34 || before.X-minus.X != 14 || before.H != 20 || after.Y-before.Y != 20 || plus.Y-minus.Y != 20 {
		t.Fatalf("diff grid: number=%+v minus=%+v plus=%+v before=%+v after=%+v", number, minus, plus, before, after)
	}
	// The 28-high undo is centered in a 36-high header, followed by a .5 border.
	if before.Y-(undo.Y-4) != 36.5 {
		t.Errorf("header height: first row=%+v undo=%+v", before, undo)
	}
}

func TestToolCardNarrowViews(t *testing.T) {
	path := strings.Repeat("很长的路径/", 12) + "file.go"
	for _, dark := range []bool{false, true} {
		for _, tool := range []string{"edit_file", "search", "list_dir"} {
			name := "t7-narrow-" + tool + map[bool]string{false: "-light", true: "-dark"}[dark]
			t.Run(name, func(t *testing.T) {
				e := cardEntry(tool, map[string]string{"path": path, "pattern": "test", "old_text": "before\n", "new_text": "after\n"}, "Edited "+path+" (1 replacement)")
				row := path + ":1:" + strings.Repeat("long content ", 20)
				if tool != "edit_file" {
					e.Output = row + "\nshort.go:2:test\n"
				}
				a := cardApp(e)
				a.opened[e.CallID] = true
				tt := ui.NewTester(a.View, 760, 480)
				tt.SetDark(dark)
				tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
				tt.Frame()
				if tool == "edit_file" {
					for _, label := range []string{"+1", "−1", "撤销 " + path} {
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.X+r.W > 760 {
							t.Errorf("header item %q clipped: %+v", label, r)
						}
					}
				} else {
					first, ok := tt.Find(row)
					second, ok2 := tt.Find("short.go:2:test")
					if !ok || !ok2 || first.H != second.H || second.Y-first.Y != first.H || first.X+first.W > 760 {
						t.Errorf("results must remain single lines: first=%+v second=%+v", first, second)
					}
				}
				snapshot(t, tt, name)
			})
		}
	}
}

func TestToolCardDiffAccuracy(t *testing.T) {
	for _, tc := range []struct {
		name, old, next string
		copies          int
		endings         bool
		added, removed  string
	}{
		// Both matches may be on the same line (foofoo -> barbar).
		{"same-line-replacements", "foo", "bar", 2, false, "+1", "−1"},
		{"crlf-to-lf", "a\r\nb\r\n", "a\nb\n", 1, true, "+0", "−0"},
		{"lf-to-crlf", "a\nb\n", "a\r\nb\r\n", 1, true, "+0", "−0"},
		{"mixed-endings", "a\r\nb\n", "a\nb\n", 1, true, "+0", "−0"},
		{"unchanged-crlf", "a\r\n", "a\r\n", 1, false, "+0", "−0"},
		{"missing-final-newline", "a\n", "a", 1, false, "+1", "−1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := "Edited a.go (1 replacement)"
			if tc.copies > 1 {
				output = fmt.Sprintf("Edited a.go (%d replacements)", tc.copies)
			}
			e := cardEntry("edit_file", map[string]any{"path": "a.go", "old_text": tc.old, "new_text": tc.next, "replace_all": tc.copies > 1}, output)
			tt := ui.NewTester(cardApp(e).View, 1200, 780)
			if tt.HasText("换行格式已变更") != tc.endings || !tt.HasText(tc.added) || !tt.HasText(tc.removed) {
				t.Fatalf("wrong fragment counts or line-ending notice: %q", tt.Texts())
			}
			if tc.copies > 1 && (!tt.HasText("同一片段替换 2 处 · 下方显示一处") || tt.HasText("+2") || tt.HasText("−2")) {
				t.Fatalf("replacement count was mistaken for changed file lines: %q", tt.Texts())
			}
		})
	}
}
