package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
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
	// The waiting current session comes first, then folders in first-seen
	// order; the source list deliberately interleaves those folders.
	want := []string{"6", "1", "3", "5", "7", "9", "2", "4", "8"}
	for i, key := range keys {
		t.Run(fmt.Sprint(i+1), func(t *testing.T) {
			a := task8App()
			a.C.Sessions = nil
			for j := range 10 {
				folder := []string{"a", "b"}[j%2]
				a.C.Sessions = append(a.C.Sessions, proto.Session{ID: fmt.Sprint(j + 1), Title: "会话" + fmt.Sprint(j+1), Cwd: "/work/" + folder})
			}
			a.C.Current = &a.C.Sessions[5]
			a.C.Approvals = []proto.Approval{{ID: "oldest", Tool: "shell"}}
			tt := task8Tester(a, false)
			tt.Key(ui.Cmd, key)
			if i == 0 {
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
			a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell"}, {ID: "second", Tool: "write_file"}, {ID: "third", Tool: "edit_file"}}
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
	a.C.Approvals = []proto.Approval{{ID: "oldest", Tool: "shell"}}
	tt := task8Tester(a, false)
	for _, text := range []string{"第一条", "第二条"} {
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
		var prompt proto.Prompt
		task8Command(t, a, proto.MethodPrompt, &prompt)
		if prompt.Text != text || a.draft != "" {
			t.Fatalf("sent %q, want %q; draft %q", prompt.Text, text, a.draft)
		}
	}
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
	task8Click(t, tt, "询问")
	tt.Key(0, ui.KeyEscape)
	if len(tt.Menu()) != 0 {
		t.Fatal("Escape did not close the mode menu")
	}
	task8NoCommand(t, a)
	task8Click(t, tt, "询问")
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
		a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell", Command: "go test ./..."}, {ID: "second", Tool: "write_file", Paths: []string{"notes.txt"}}}
		tt := task8Tester(a, dark)
		appearance := map[bool]string{false: "light", true: "dark"}[dark]
		texts := strings.Join(tt.Texts(), "|")
		if strings.Count(texts, "⌘⏎") != 1 || strings.Count(texts, "esc") != 1 {
			t.Fatalf("only oldest should show shortcut hints: %q", texts)
		}
		snapshot(t, tt, "t8-approval-keys-"+appearance)
		tt.Type("回车发送这条消息")
		if strings.Contains(strings.Join(tt.Texts(), "|"), "|⏎|") {
			t.Fatal("allow still advertises Enter while draft is nonempty")
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
				a.C.Approvals = []proto.Approval{{ID: "first", Tool: "shell"}, {ID: "second", Tool: "write_file"}}
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
