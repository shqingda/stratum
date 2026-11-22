package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

// TestAnswerKeepsComposerFocus checks that answering an approval with the
// mouse leaves the keyboard in the composer, for the next message.
func TestAnswerKeepsComposerFocus(t *testing.T) {
	a := working()
	a.C.Queued = nil
	focused := false
	tt := ui.NewTester(func(c *ui.Context) {
		a.View(c)
		focused = a.composerInput.Focused(c)
	}, 1440, 880)
	tt.Frame()
	tt.Frame()
	if !focused {
		t.Fatal("the composer does not have the focus to begin with")
	}
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focused {
		t.Error("clicking Allow took the focus from the composer")
	}
}

// TestOptionCmdEnterAllowsAlways checks that ⌥⌘⏎ gives the strongest
// answer, allowed from now on, as Option adds to a Mac's commands.
func TestOptionCmdEnterAllowsAlways(t *testing.T) {
	a := working()
	a.C.Queued = nil
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyEnter)
	var ans proto.ApprovalAnswer
	task8Command(t, a, proto.MethodApprovalRespond, &ans)
	if !ans.Allow || !ans.Forever || ans.ApprovalID != "x" {
		t.Errorf("answer = %+v, want x allowed forever", ans)
	}
}

// TestRunningShowsElapsed checks that a running turn says how long it has
// run and that Esc stops it.
func TestRunningShowsElapsed(t *testing.T) {
	a := working()
	a.C.Approvals, a.C.Queued = nil, nil
	a.C.Turns = []client.Turn{{Seq: 1, Start: now.Add(-12 * time.Second)}}
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if want := "12 秒 · esc 中断"; !tt.HasText(want) {
		t.Errorf("no %q in %q", want, tt.Texts())
	}
}

// TestCmdPlusZoomsText checks that ⌘+ steps the text size up, as View ›
// Zoom In does in Safari and Mail.
func TestCmdPlusZoomsText(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals, a.C.Queued = false, nil, nil
	a.C.Settings = &proto.Settings{}
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEqual)
	var s proto.Settings
	task8Command(t, a, proto.MethodSetSettings, &s)
	if s.TextSize != "large" {
		t.Errorf("text size = %q, want large", s.TextSize)
	}
}

// TestCmdNumberWithSidebarHidden checks that ⌘2 opens the second session
// the sidebar would show, though the sidebar is hidden.
func TestCmdNumberWithSidebarHidden(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals, a.C.Queued = false, nil, nil
	a.out = make(chan []proto.Command, 16)
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	a.toggleSide()
	tt.Advance(time.Second)
	tt.Frame()
	a.C.Sessions = append([]proto.Session{{ID: "new", Title: "新的", Cwd: "/Users/me/Projects/calc", Updated: now}}, a.C.Sessions...)
	a.C.Current = &a.C.Sessions[1]
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key1)
	cmds, ok := next(a)
	if !ok || len(cmds) != 1 || !strings.Contains(string(cmds[0].Params), `"new"`) {
		t.Errorf("⌘1 sent %+v, want the new session opened", cmds)
	}
}

// TestAttachmentsStayWithSession checks that a file attached in one
// session is not sent from another, and is there again on coming back.
func TestAttachmentsStayWithSession(t *testing.T) {
	a := working()
	a.C.Busy, a.C.Approvals, a.C.Queued = false, nil, nil
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	a.attached = []attachment{{name: "shot.png"}}
	a.C.Current = &a.C.Sessions[1]
	tt.Frame()
	if len(a.attached) != 0 {
		t.Fatalf("the other session's composer holds %d attachments", len(a.attached))
	}
	a.C.Current = &a.C.Sessions[0]
	tt.Frame()
	if len(a.attached) != 1 || a.attached[0].name != "shot.png" {
		t.Errorf("coming back, attached = %+v", a.attached)
	}
}

// TestFollowsSystemReduceMotion checks that macOS's Reduce motion keeps
// the app still, as its own setting does.
func TestFollowsSystemReduceMotion(t *testing.T) {
	a := working()
	a.C.Approvals, a.C.Queued = nil, nil
	tt := ui.NewTesterWith(a.View, 1440, 880, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	tt.Frame()
	if !a.reduceMotion() {
		t.Error("the app does not see macOS's Reduce motion")
	}
}
