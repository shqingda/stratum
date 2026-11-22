package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

func TestTask8NewAndSearchShortcuts(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("保留草稿")
	tt.Key(ui.Cmd, ui.KeyK)
	if !tt.Focused("搜索") {
		t.Fatal("Cmd+K did not focus search")
	}
	tt.Type("client")
	if a.query != "client" || a.draft != "保留草稿" {
		t.Fatalf("query %q, draft %q", a.query, a.draft)
	}
	tt.Key(0, ui.KeyEscape)
	if a.pal || a.query != "" {
		t.Fatalf("Escape left the palette open: %v %q", a.pal, a.query)
	}
	task8NoCommand(t, a)
	tt.Key(ui.Cmd, ui.KeyN)
	var create proto.SessionCreate
	task8Command(t, a, proto.MethodSessionCreate, &create)
	if create.Cwd != a.C.Cwd {
		t.Fatalf("new session cwd = %q", create.Cwd)
	}
}

func TestTask8SessionShortcuts(t *testing.T) {
	keys := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	// Folders appear in first-seen order ("a" then "b"); waiting sessions stay
	// in their project group without a separate "Needs you" section.
	want := []string{"1", "3", "5", "7", "9", "2", "4", "6", "8"}
	for i, key := range keys {
		t.Run(fmt.Sprint(i+1), func(t *testing.T) {
			a := task8App()
			a.C.Sessions = nil
			for j := range 10 {
				folder := []string{"a", "b"}[j%2]
				a.C.Sessions = append(a.C.Sessions, proto.Session{ID: fmt.Sprint(j + 1), Title: "会话" + fmt.Sprint(j+1), Cwd: "/work/" + folder})
			}
			a.C.Current = &a.C.Sessions[5] // ID "6", at index 7 in sidebar order
			a.C.Approvals = []proto.Approval{{ID: "oldest", Tool: "shell"}}
			tt := task8Tester(a, false)
			tt.Key(ui.Cmd, key)
			if i == 7 {
				task8NoCommand(t, a)
				return
			}
			var ref proto.SessionRef
			task8Command(t, a, proto.MethodSessionGet, &ref)
			if ref.SessionID != want[i] {
				t.Fatalf("Cmd+%d opened %q, want %q", i+1, ref.SessionID, want[i])
			}
		})
	}
}

// TestPaletteOpensSession finds a session in the ⌘K palette and opens it
// with Enter.
func TestPaletteOpensSession(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("client")
	tt.Key(0, ui.KeyEnter)
	var ref proto.SessionRef
	task8Command(t, a, proto.MethodSessionGet, &ref)
	if ref.SessionID != "3" || a.pal {
		t.Fatalf("opened %q, palette open %v", ref.SessionID, a.pal)
	}
}

func TestTask8ApprovalShortcuts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mods          ui.Modifiers
		key           ui.Key
		allow, always bool
	}{{"allow", 0, ui.KeyEnter, true, false}, {"always", ui.Cmd, ui.KeyEnter, true, true}, {"deny", 0, ui.KeyEscape, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a := task8App()
			a.C.Busy = true
			a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell", Rule: goTest}, {ID: "second", Tool: "write_file"}, {ID: "third", Tool: "edit_file"}}
			tt := task8Tester(a, false)
			tt.Key(tc.mods, tc.key)
			var answer proto.ApprovalAnswer
			task8Command(t, a, proto.MethodApprovalRespond, &answer)
			if answer.SessionID != "1" || answer.ApprovalID != "first" || answer.Allow != tc.allow || answer.Always != tc.always {
				t.Fatalf("answer = %+v", answer)
			}
			tt.Key(tc.mods, tc.key)
			task8NoCommand(t, a)
			if a.C.Answering["second"] || a.C.Answering["third"] {
				t.Fatal("shortcut skipped the answering oldest approval")
			}
		})
	}
}

func TestTask8ComposerConflicts(t *testing.T) {
	a := task8App()
	a.C.Busy = true
	a.C.Approvals = []proto.Approval{{ID: "oldest", Tool: "shell", Rule: goTest}}
	tt := task8Tester(a, false)
	// Words written while a call waits decline it and say why.
	tt.Type("别跑这个")
	tt.Key(0, ui.KeyEnter)
	var no proto.ApprovalAnswer
	task8Command(t, a, proto.MethodApprovalRespond, &no)
	if no.Allow || no.Text != "别跑这个" || a.draft != "" {
		t.Fatalf("declined %+v, draft %q", no, a.draft)
	}
	delete(a.C.Answering, "oldest")
	tt.Type("保留")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("换行")
	if a.draft != "保留\n换行" {
		t.Fatalf("Shift+Enter draft = %q", a.draft)
	}
	task8NoCommand(t, a)
	tt.Key(ui.Cmd, ui.KeyEnter)
	var answer proto.ApprovalAnswer
	task8Command(t, a, proto.MethodApprovalRespond, &answer)
	if !answer.Allow || !answer.Always || a.draft != "保留\n换行" {
		t.Fatalf("Cmd+Enter answer %+v, draft %q", answer, a.draft)
	}
}

func TestTask8SearchAndMenuKeepTheirKeys(t *testing.T) {
	a := task8App()
	a.C.Busy = true
	a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell"}}
	tt := task8Tester(a, false)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("没有这个")
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Key(0, ui.KeyEscape) // closes the palette, not the running turn
	task8NoCommand(t, a)
	task8Click(t, tt, "手动")
	tt.Key(0, ui.KeyEscape)
	if len(tt.Menu()) != 0 {
		t.Fatal("Escape did not close the mode menu")
	}
	task8NoCommand(t, a)
	task8Click(t, tt, "手动")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	task8Command(t, a, proto.MethodSessionUpdate, nil)
	task8NoCommand(t, a)
}

func TestTask8EscapeStopsWithoutApproval(t *testing.T) {
	for _, draft := range []string{"", "保留输入"} {
		a := task8App()
		a.C.Busy = true
		tt := task8Tester(a, false)
		tt.Type(draft)
		tt.Key(0, ui.KeyEscape)
		var ref proto.SessionRef
		task8Command(t, a, proto.MethodCancel, &ref)
		if ref.SessionID != "1" || a.draft != draft {
			t.Fatalf("cancel %+v, draft %q", ref, a.draft)
		}
	}
}

func TestTask8ApprovalHints(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := task8App()
		a.C.Entries = nil
		a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell", Command: "go test ./...", Rule: goTest}, {ID: "second", Tool: "write_file", Paths: []string{"notes.txt"}}}
		tt := task8Tester(a, dark)
		appearance := map[bool]string{false: "light", true: "dark"}[dark]
		texts := strings.Join(tt.Texts(), "|")
		if strings.Count(texts, "|⌘⏎|") != 1 || strings.Count(texts, "esc") != 1 {
			t.Fatalf("only oldest should show shortcut hints: %q", texts)
		}
		snapshot(t, tt, "t8-approval-keys-"+appearance)
		tt.Type("回车发送这条消息")
		// Enter declines with the words now: the hint moves to 拒绝.
		if texts := strings.Join(tt.Texts(), "|"); strings.Count(texts, "|⏎|") != 1 || !strings.Contains(texts, "拒绝|拒绝|⏎|总是允许") {
			t.Fatalf("Enter is not on 拒绝 while a draft is written: %q", texts)
		}
		snapshot(t, tt, "t8-draft-keys-"+appearance)
	}
}

func TestTask8HeldApprovalKeyDoesNotAnswerNext(t *testing.T) {
	for _, composerFocus := range []bool{true, false} {
		for _, key := range []struct {
			mods ui.Modifiers
			key  ui.Key
		}{{0, ui.KeyEnter}, {ui.Cmd, ui.KeyEnter}, {0, ui.KeyEscape}} {
			t.Run(fmt.Sprintf("composer-%v/key-%d/mods-%d", composerFocus, key.key, key.mods), func(t *testing.T) {
				a := task8App()
				a.C.Busy = true
				a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell", Rule: goTest}, {ID: "second", Tool: "write_file"}}
				var tt *ui.Tester
				if composerFocus {
					tt = task8Tester(a, false)
				} else {
					a.keyRepeat = true // No focus means ObserveInput cannot clear an earlier repeat.
					tt = ui.NewTester(a.keys, 1200, 780)
				}
				tt.HoldModifiers(key.mods)
				tt.KeyDown(key.key)
				task8Command(t, a, proto.MethodApprovalRespond, nil)
				a.C.Approvals = a.C.Approvals[1:]
				tt.Frame()
				tt.KeyRepeat(key.mods, key.key)
				task8NoCommand(t, a)
				tt.KeyUp(key.key)
				tt.HoldModifiers(0)
				tt.Key(key.mods, key.key)
				var answer proto.ApprovalAnswer
				task8Command(t, a, proto.MethodApprovalRespond, &answer)
				if answer.ApprovalID != "second" {
					t.Fatalf("a new press did not answer second approval: %+v", answer)
				}
			})
		}
	}
}

func TestTask8SearchFocusViews(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := task8App()
		tt := task8Tester(a, dark)
		tt.Key(ui.Cmd, ui.KeyK)
		tt.Type("client")
		if !tt.Focused("搜索") || !tt.HasText("talos · 3 时") || tt.HasText("site · ") {
			t.Fatalf("search focus/filter failed: %q", tt.Texts())
		}
		appearance := map[bool]string{false: "light", true: "dark"}[dark]
		snapshot(t, tt, "t8-search-focus-"+appearance)
	}
}

// While a turn runs, ⏎ queues, ⌘⏎ steers, ⏎ in an empty composer sends
// the oldest waiting prompt now, ↑ brings back the last one sent, and
// what a stopped turn gives back lands in the composer.
func TestQueueKeys(t *testing.T) {
	a := task8App()
	a.C.Busy = true
	a.C.Entries = append(a.C.Entries, &client.Entry{Key: 99, Role: client.User, Text: "上一条", Done: true})
	tt := task8Tester(a, false)

	tt.Type("排队")
	tt.Key(0, ui.KeyEnter)
	var p proto.Prompt
	task8Command(t, a, proto.MethodPrompt, &p)
	if p.Text != "排队" || p.Steer {
		t.Fatalf("⏎ sent %+v", p)
	}
	tt.Type("插话")
	tt.Key(ui.Cmd, ui.KeyEnter)
	task8Command(t, a, proto.MethodPrompt, &p)
	if p.Text != "插话" || !p.Steer {
		t.Fatalf("⌘⏎ sent %+v", p)
	}

	tt.Key(0, ui.KeyUp)
	if a.draft != "上一条" {
		t.Fatalf("↑ draft %q", a.draft)
	}

	for range 3 {
		tt.Key(0, ui.KeyBackspace)
	}
	a.C.Queued = []proto.Queued{{ID: "q1", Text: "排队"}}
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	var edit proto.QueueEdit
	task8Command(t, a, proto.MethodQueueEdit, &edit)
	if edit.ID != "q1" || edit.Action != "steer" {
		t.Fatalf("⏎ on empty %+v", edit)
	}
	if err := tt.Click("排队"); err != nil { // its words: back to change
		t.Fatal(err)
	}
	task8Command(t, a, proto.MethodQueueEdit, &edit)
	if edit.Action != "remove" || a.draft != "排队" {
		t.Fatalf("edit %+v, draft %q", edit, a.draft)
	}
	a.draft = ""
	tt.Frame()
	if err := tt.Click("不发了"); err != nil {
		t.Fatal(err)
	}
	task8Command(t, a, proto.MethodQueueEdit, &edit)
	if edit.Action != "remove" {
		t.Fatalf("remove %+v", edit)
	}

	a.C.Queued = nil
	a.C.Returned = map[string][]string{a.C.Current.ID: {"没发出去的"}}
	tt.Frame()
	if a.draft != "没发出去的" || len(a.C.Returned) != 0 {
		t.Fatalf("taken back %q %v", a.draft, a.C.Returned)
	}
}

// Each session keeps its own draft: leaving one keeps it in the kernel,
// and the one opened shows what the kernel kept for it.
func TestDraftPerSession(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("写给 1 的")
	tt.Frame()
	a.C.Current, a.C.Draft = nil, "" // on the way to session 2
	tt.Frame()
	var d proto.DraftSet
	task8Command(t, a, proto.MethodDraftSet, &d)
	if d.SessionID != "1" || d.Text != "写给 1 的" || a.draft != "" {
		t.Fatalf("kept %+v, draft %q", d, a.draft)
	}
	a.C.Current, a.C.Draft = &a.C.Sessions[1], "2 的旧稿"
	tt.Frame()
	if a.draft != "2 的旧稿" {
		t.Fatalf("draft %q", a.draft)
	}
	// A pause in typing keeps it.
	tt.Type("，接着写")
	tt.Advance(2 * time.Second)
	tt.Frame()
	task8Command(t, a, proto.MethodDraftSet, &d)
	if d.SessionID != "2" || d.Text != "2 的旧稿，接着写" {
		t.Fatalf("kept %+v", d)
	}
}

// A draft that starts with ! runs as a command, once no turn runs.
func TestBangCommand(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("!go test ./...")
	if !tt.HasText("在终端运行") {
		t.Fatal("no terminal hint")
	}
	tt.Key(0, ui.KeyEnter)
	var sh proto.ShellRun
	task8Command(t, a, proto.MethodShell, &sh)
	if sh.Command != "go test ./..." || a.draft != "" {
		t.Fatalf("ran %+v, draft %q", sh, a.draft)
	}
	a.C.Busy = true
	tt.Type("!ls")
	tt.Key(0, ui.KeyEnter)
	task8NoCommand(t, a)
	if a.err == "" || a.draft != "!ls" {
		t.Fatalf("err %q draft %q", a.err, a.draft)
	}
}

// @ and a few letters list the workspace's files; ↓ and ⏎ put one in.
func TestMention(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("看看 @comp")
	tt.Frame()
	var q proto.FilesQuery
	for done := false; !done; {
		select {
		case cmds := <-a.out:
			if len(cmds) == 1 && cmds[0].Method == proto.MethodFilesFind {
				json.Unmarshal(cmds[0].Params, &q)
			}
		default:
			done = true
		}
	}
	if q.Query != "comp" {
		t.Fatalf("asked for %+v", q)
	}
	a.C.Files, a.C.FilesFor = []string{"app/composer.go", "app/compact.go"}, "comp"
	tt.Frame()
	if !tt.HasText("composer.go") {
		t.Fatal("no list")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	task8NoCommand(t, a)
	tt.Frame()
	tt.Type("里")
	tt.Frame()
	if a.draft != "看看 @app/compact.go 里" {
		t.Fatalf("draft %q", a.draft)
	}

	a.C.Busy = true
	tt.Type(" @c")
	a.C.FilesFor = "c"
	tt.Frame()
	for _, ok := next(a); ok; _, ok = next(a) {
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	task8NoCommand(t, a) // Escape put the list away and stopped nothing
	if tt.HasText("composer.go") {
		t.Fatal("the list stayed")
	}
}

var goTest = &proto.Rule{Tool: "shell", Prefix: "go test"}

// With the setting, ⏎ steers while a turn runs and ⌘⏎ queues; when idle
// ⏎ sends as always.
func TestEnterSteersSetting(t *testing.T) {
	a := task8App()
	a.C.Busy = true
	a.C.Settings = &proto.Settings{EnterSteers: true}
	tt := task8Tester(a, false)
	var p proto.Prompt
	for _, k := range []struct {
		text  string
		mods  ui.Modifiers
		steer bool
	}{{"插话", 0, true}, {"排队", ui.Cmd, false}} {
		tt.Type(k.text)
		tt.Key(k.mods, ui.KeyEnter)
		p = proto.Prompt{}
		task8Command(t, a, proto.MethodPrompt, &p)
		if p.Text != k.text || p.Steer != k.steer {
			t.Fatalf("sent %+v", p)
		}
	}
	a.C.Busy = false
	tt.Frame()
	tt.Type("新的一轮")
	tt.Key(0, ui.KeyEnter)
	p = proto.Prompt{}
	task8Command(t, a, proto.MethodPrompt, &p)
	if p.Steer {
		t.Fatalf("idle ⏎ sent %+v", p)
	}
}

// The palette's actions reach what the session's menus and the settings
// do: here, find and pinning the session.
func TestPaletteActions(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("查找")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.pal || !a.find.on {
		t.Fatalf("palette open %v, find on %v", a.pal, a.find.on)
	}
	a.closeFind()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("置顶")
	tt.Key(0, ui.KeyEnter)
	var f proto.SessionFlag
	task8Command(t, a, proto.MethodSessionFlag, &f)
	if f.Pinned == nil || !*f.Pinned {
		t.Fatalf("flag %+v", f)
	}
	tt.Key(ui.Cmd, ui.KeyK)
	a.hasApp = func(name string) bool { return name == "Visual Studio Code" }
	tt.Frame()
	for _, want := range []string{"切换模型…", "打开终端面板", "用 VS Code 打开", "改名", "归档", "外观：深色"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the palette", want)
		}
	}
}

// The palette finds the workspace's files; one chosen opens in the editor.
func TestPaletteFiles(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := task8App()
		var opened string
		a.openIn = func(app, path string) error { opened = path; return nil }
		tt := task8Tester(a, dark)
		tt.Key(ui.Cmd, ui.KeyK)
		tt.Type("sandbox")
		a.C.FilesFor, a.C.Files = "sandbox", []string{"kernel/sandbox/sandbox.go", "kernel/sandbox/profile.sb"}
		tt.Frame()
		if !tt.HasText("文件") || !tt.HasText("kernel/sandbox/sandbox.go") {
			t.Fatalf("no files: %q", tt.Texts())
		}
		snapshot(t, tt, "palette-files-"+map[bool]string{false: "light", true: "dark"}[dark])
		if err := tt.Click("kernel/sandbox/sandbox.go"); err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(a.C.Current.Cwd, "kernel/sandbox/sandbox.go"); opened != want {
			t.Errorf("opened %q, want %q", opened, want)
		}
	}
}

// A bare @ lists the files the session changed first.
func TestMentionRecent(t *testing.T) {
	a := task8App()
	a.C.Changes.Turns = []proto.ChangeTurn{
		{TurnSeq: 1, Files: []proto.ChangeFile{{Path: "old.go"}}},
		{TurnSeq: 2, Files: []proto.ChangeFile{{Path: "new.go"}, {Path: "old.go"}}},
	}
	a.C.Files, a.C.FilesFor = []string{"a.go", "new.go"}, ""
	a.mention.query = ""
	got := a.mentionList()
	if strings.Join(got, ",") != "new.go,old.go,a.go" {
		t.Fatalf("%v", got)
	}
}

// A bare @ asks for the files at once: the empty query is asked too.
func TestMentionBare(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("@")
	tt.Frame()
	asked := false
	for done := false; !done; {
		select {
		case cmds := <-a.out:
			for _, c := range cmds {
				asked = asked || c.Method == proto.MethodFilesFind
			}
		default:
			done = true
		}
	}
	if !asked {
		t.Fatal("a bare @ asked for no files")
	}
}

// @ lists other sessions in their own group; Enter inserts @session:<id>.
func TestMentionSession(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Type("@沙箱")
	tt.Frame()
	for done := false; !done; {
		select {
		case <-a.out:
		default:
			done = true
		}
	}
	a.C.Files = nil
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	task8NoCommand(t, a)
	tt.Frame()
	if a.draft != "" {
		t.Fatalf("draft %q, want empty when session chip is stored", a.draft)
	}
	if len(a.mentionSessions) != 1 || a.mentionSessions[0] != "2" {
		t.Fatalf("mentionSessions = %v, want [2]", a.mentionSessions)
	}
	if got, _ := a.prompt(); got != "@session:2" {
		t.Fatalf("prompt = %q, want @session:2", got)
	}
}

// @ lists an MCP resource as @server:uri, the Claude Code form.
func TestMentionResource(t *testing.T) {
	a := task8App()
	a.C.MCP = &proto.MCPList{Resources: []proto.MCPResource{{Server: "notes", URI: "note://today", Name: "today"}}}
	a.C.MCPFor = a.C.Current.Cwd
	tt := task8Tester(a, false)
	tt.Type("@today")
	tt.Frame()
	for done := false; !done; {
		select {
		case <-a.out:
		default:
			done = true
		}
	}
	a.C.Files = nil
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	task8NoCommand(t, a)
	if a.draft != "@notes:note://today " {
		t.Fatalf("draft %q", a.draft)
	}
}

func TestMentionGroups(t *testing.T) {
	a := task8App()
	a.C.Files = []string{"src/", "main.go"}
	a.C.Sessions = append(a.C.Sessions, proto.Session{ID: "gone", Title: "已归档", Archived: true, Cwd: a.C.Current.Cwd})
	a.C.MCP = &proto.MCPList{Resources: []proto.MCPResource{{Server: "notes", URI: "note://today", Name: "today"}}}
	a.C.MCPFor = a.C.Current.Cwd
	var texts []string
	groups := a.mentionGroups()
	for _, g := range groups {
		for _, it := range g.items {
			texts = append(texts, it.kind+":"+it.text)
		}
	}
	want := "session:session:2,session:session:3,session:session:4,session:session:5,folder:src/,file:main.go,resource:notes:note://today"
	if strings.Join(texts, ",") != want {
		t.Fatalf("%s", strings.Join(texts, ","))
	}
	if groups[0].items[0].note != "talos" {
		t.Fatalf("note %q", groups[0].items[0].note)
	}
	a.mention.query = "沙箱"
	a.C.Files = nil
	got := a.mentionItems()
	if len(got) != 1 || got[0].text != "session:2" {
		t.Fatalf("%+v", got)
	}
}

// The palette sets what Settings sets, a row for each value but the
// current one.
func TestPaletteSettings(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	for _, want := range []string{
		"语言：简体中文", "语言：English", "文字大小：小", "文字大小：大", "减弱动态效果：开",
		"按回车：立即发送", "需要确认通知：关", "在程序坞图标上显示数字：关", "什么时候通知：总是", "什么时候通知：从不",
	} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the palette", want)
		}
	}
	if tt.HasText("文字大小：默认") || tt.HasText("语言：跟随系统") || tt.HasText("什么时候通知：后台时") {
		t.Error("the palette offers a current value")
	}
	tt.Type("通知")
	tt.Frame()
	snapshot(t, tt, "palette-settings")
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyK)

	tt.Type("文字大小：大")
	tt.Key(0, ui.KeyEnter)
	var s proto.Settings
	task8Command(t, a, proto.MethodSetSettings, &s)
	if s.TextSize != "large" {
		t.Fatalf("text size %q, want large", s.TextSize)
	}

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("完成通知")
	tt.Key(0, ui.KeyEnter)
	s = proto.Settings{}
	task8Command(t, a, proto.MethodSetSettings, &s)
	if !slices.Contains(s.Quiet, "done") {
		t.Fatalf("quiet %v, want done in it", s.Quiet)
	}

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("English")
	tt.Key(0, ui.KeyEnter)
	if got := a.locale.Chosen(); got != "en" {
		t.Fatalf("locale %q, want en", got)
	}
}
