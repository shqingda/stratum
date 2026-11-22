package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

func TestSessionMenu(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	drain(a)
	menu := func(row string, item string) proto.SessionFlag {
		t.Helper()
		if err := tt.RightClick(row); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("%v in %q", err, tt.Menu())
		}
		tt.Frame()
		var f proto.SessionFlag
		task8Command(t, a, proto.MethodSessionFlag, &f)
		return f
	}
	if f := menu("client 按日志重建", "置顶"); f.SessionID != "3" || f.Pinned == nil || !*f.Pinned {
		t.Fatalf("pin %+v", f)
	}
	if f := menu("client 按日志重建", "标为未读"); f.Mark == nil || *f.Mark != "unread" || a.C.Marks["3"] != "unread" {
		t.Fatalf("unread %+v", f)
	}

	// Archived, it goes away and can come back for a few seconds.
	f := menu("首页文案改写", "归档")
	if f.SessionID != "5" || !*f.Archived || !tt.HasText("已归档「首页文案改写」") {
		t.Fatalf("archive %+v: %q", f, tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	task8Command(t, a, proto.MethodSessionFlag, &f)
	if *f.Archived {
		t.Fatalf("undo %+v", f)
	}
	menu("首页文案改写", "归档")
	a.now = func() time.Time { return task8App().now().Add(6 * time.Second) }
	tt.Frame()
	if tt.HasText("已归档「首页文案改写」") {
		t.Fatal("the undo outlived its time")
	}

	// Deleting asks first.
	if err := tt.RightClick("协议定稿：回执与快照"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	task8NoCommand(t, a)
	if !tt.HasText("删除「协议定稿：回执与快照」？") {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	var ref proto.SessionRef
	task8Command(t, a, proto.MethodSessionDelete, &ref)
	if ref.SessionID != "4" {
		t.Fatalf("deleted %+v", ref)
	}
}

func TestRenameFromBar(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)
	drain(a)
	if err := tt.Click("改名"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("修 Add")
	tt.Key(0, ui.KeyEnter)
	var u proto.SessionUpdate
	task8Command(t, a, proto.MethodSessionUpdate, &u)
	if u.SessionID != "1" || u.Title == nil || *u.Title != "修 Add" || a.renaming != "" {
		t.Fatalf("renamed %+v", u)
	}
}

// ⌘J goes to the next session waiting on the user; looking at a session
// reads it.
func TestNextWaitingAndSeen(t *testing.T) {
	a := task8App()
	a.C.Marks = map[string]string{"3": "wait", "1": "unread"}
	a.C.Sessions[0].Mark = "unread"
	tt := task8Tester(a, false)
	var f proto.SessionFlag
	for cmds, ok := next(a); ok; cmds, ok = next(a) {
		if cmds[0].Method == proto.MethodSessionFlag {
			f.SessionID = "1"
		}
	}
	if f.SessionID != "1" || a.C.Marks["1"] != "" {
		t.Fatalf("looking did not read it: %v", a.C.Marks)
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	var ref proto.SessionRef
	task8Command(t, a, proto.MethodSessionGet, &ref)
	if ref.SessionID != "3" {
		t.Fatalf("⌘J opened %+v", ref)
	}
}
