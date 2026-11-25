package app

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

const calcDir = "/Users/me/Projects/calc"

// emptySession is a new session in calc whose folder was read as w.
func emptySession(w proto.Workspace) *App {
	a := working()
	a.C.Entries, a.C.Approvals, a.C.Queued, a.C.Busy = nil, nil, nil, false
	a.C.Workspace.Workspace = w
	a.C.Workspace.Read = 1
	return a
}

func helloTester(a *App) *ui.Tester {
	tt := ui.NewTester(a.View, 1200, 780)
	tt.SetTitleBar(ui.TitleBar{Left: 78, Height: 52})
	tt.Frame()
	tt.Frame()
	return tt
}

func wantTexts(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	for _, s := range want {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
}

func TestHelloOverviewDirty(t *testing.T) {
	a := emptySession(proto.Workspace{
		Branch:  "main",
		Dirty:   true,
		Files:   []proto.ChangeFile{{Path: calcDir + "/calc.go", Added: 12, Removed: 3}, {Path: calcDir + "/calc_test.go", Added: 20}},
		Commits: []proto.CommitSummary{{Hash: "abc1234", Subject: "fix: guard divide by zero", Time: now.Add(-2 * time.Hour).Unix()}},
		Recent:  []proto.RecentFile{{Path: calcDir + "/calc.go", Time: now.Unix()}, {Path: calcDir + "/docs/usage.md", Time: now.Add(-26 * time.Hour).Unix()}},
	})
	tt := helloTester(a)
	snapshot(t, tt, "hello-dirty")
	wantTexts(t, tt, "calc · main", "2 处未提交", "calc.go", "+12 −3", "calc_test.go", "最近提交", "fix: guard divide by zero", "2 时", "最近文件", "docs/usage.md")
	if r, _ := tt.Find("手动"); r.Y < 780*.8 {
		t.Errorf("the composer of a folder with an overview stays at the foot, not at y %v", r.Y)
	}
	// calc.go is listed once, as uncommitted, not again as recent: its
	// row's label and its text.
	n := 0
	for _, s := range tt.Texts() {
		if s == "calc.go" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("calc.go listed %d times", n)
	}
	if err := tt.Click("calc.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.rv.open || !a.rv.uncommitted || a.rv.sel != client.DiffKey(0, calcDir+"/calc.go") {
		t.Errorf("an uncommitted file opens its diff: open %v uncommitted %v sel %q", a.rv.open, a.rv.uncommitted, a.rv.sel)
	}
}

func TestHelloOverviewClean(t *testing.T) {
	a := emptySession(proto.Workspace{
		Branch:  "main",
		Commits: []proto.CommitSummary{{Hash: "abc1234", Subject: "add modulo", Time: now.Add(-30 * time.Minute).Unix()}},
		Recent:  []proto.RecentFile{{Path: calcDir + "/docs/usage.md", Time: now.Add(-time.Hour).Unix()}},
	})
	tt := helloTester(a)
	snapshot(t, tt, "hello-clean")
	wantTexts(t, tt, "calc · main", "没有未提交的改动", "最近提交", "add modulo", "最近文件", "docs/usage.md")
	if err := tt.Click("docs/usage.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.rv.open || a.rv.tab != "files" || a.rv.previewPath != "docs/usage.md" {
		t.Errorf("a recent file opens in Files: open %v tab %q path %q", a.rv.open, a.rv.tab, a.rv.previewPath)
	}
}

func TestHelloOverviewOutsideGit(t *testing.T) {
	a := emptySession(proto.Workspace{Recent: []proto.RecentFile{{Path: calcDir + "/notes.txt", Time: now.Add(-time.Hour).Unix()}}})
	tt := helloTester(a)
	wantTexts(t, tt, "calc", "最近文件", "notes.txt")
	for _, s := range []string{"没有未提交的改动", "最近提交"} {
		if tt.HasText(s) {
			t.Errorf("%q shows outside git", s)
		}
	}
}

// TestHelloEmptyFolder: a folder holding nothing has no overview; the
// composer sits on the canvas, and goes down to the foot once the first
// message is in, along the way, not at once.
func TestHelloEmptyFolder(t *testing.T) {
	a := emptySession(proto.Workspace{})
	tt := helloTester(a)
	snapshot(t, tt, "hello-empty")
	for _, s := range []string{"最近提交", "最近文件", "没有未提交的改动"} {
		if tt.HasText(s) {
			t.Errorf("%q shows for an empty folder", s)
		}
	}
	r, _ := tt.Find("手动")
	if r.Y < 780*.3 || r.Y > 780*.6 {
		t.Errorf("the composer at y %v, not on the canvas", r.Y)
	}
	a.C.Entries = []*client.Entry{{Key: 1, Role: client.User, Text: "写一个计算器", Done: true}}
	a.C.Busy = true
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	snapshot(t, tt, "hello-empty-falling")
	mid, _ := tt.Find("手动")
	if mid.Y <= r.Y+1 || mid.Y >= 780*.9 {
		t.Errorf("120 ms after, the composer at y %v: not on its way down from %v", mid.Y, r.Y)
	}
	tt.Advance(time.Second)
	tt.Frame()
	snapshot(t, tt, "hello-empty-sent")
	if end, _ := tt.Find("手动"); end.Y < 780*.9 {
		t.Errorf("the composer at y %v, not at the foot", end.Y)
	}
	if a.heroed() {
		t.Error("the composer belongs on the canvas after the first message")
	}
}

// TestHelloRemembersFolder: a new session in a folder read before starts
// in place, before its own read answers.
func TestHelloRemembersFolder(t *testing.T) {
	a := emptySession(proto.Workspace{})
	if known, empty := a.overview(); !known || !empty {
		t.Fatalf("read: known %v empty %v", known, empty)
	}
	a.C.Workspace = client.Workspace{BranchStale: true}
	if known, empty := a.overview(); !known || !empty {
		t.Errorf("not read yet, the last read of calc stands: known %v empty %v", known, empty)
	}
	a.C.Current = &a.C.Sessions[4] // site, never read
	if known, _ := a.overview(); known {
		t.Error("a folder never read is known")
	}
}

// TestComposerAttached: what goes with the words is a row of tokens at
// the card's top, over a hairline, and the card is the words alone without
// it; the words keep still as the row comes; the pop-up buttons say they
// are, and the mode is its name alone.
func TestComposerAttached(t *testing.T) {
	a := emptySession(proto.Workspace{})
	tt := helloTester(a)
	snapshot(t, tt, "composer-idle")
	wantTexts(t, tt, "chevron.down")
	if tt.HasText("附带") || tt.HasText("eye") {
		t.Errorf("an empty card shows the attached row or a mode symbol: %q", tt.Texts())
	}
	idle, _ := tt.Find("手动")
	a.attached = []attachment{{name: "screenshot.png", path: "/tmp/screenshot.png"}}
	a.mentionSessions = []string{"5"}
	a.draft = "@add_test.go 把计算器的除零改成返回错误"
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	snapshot(t, tt, "composer-attached")
	wantTexts(t, tt, "附带", "首页文案改写", "screenshot.png")
	if _, ok := tt.Find("add_test.go"); ok {
		t.Error("a file the words name with @ is said again in the attached row")
	}
	row, _ := tt.Find("附带")
	words, _ := tt.Find("手动")
	if row.Y >= words.Y {
		t.Errorf("the attached row at y %v is not over the controls at %v", row.Y, words.Y)
	}
	if words.Y < idle.Y-1 || words.Y > idle.Y+60 {
		t.Errorf("the controls went from y %v to %v: the words did not keep still", idle.Y, words.Y)
	}
	if err := tt.Click("移除 screenshot.png"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.attached) != 0 {
		t.Error("the x of a token does not take it out")
	}
	a.C.Current.Mode = "full"
	tt.Frame()
	if tt.HasText("shield.slash") {
		t.Error("full access shows a symbol")
	}
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	snapshot(t, tt, "composer-attached-dark")
}

// TestComposerMarksReferences: the skills and the files the draft names
// show in the accent in the words, the / or @ faint; a / word that names
// no skill, and the @ word still being written, show plain.
func TestComposerMarksReferences(t *testing.T) {
	a := emptySession(proto.Workspace{})
	a.C.MCP = &proto.MCPList{Skills: []proto.SkillInfo{{Name: "review"}}}
	a.draft = "看看 @add.go 和 @docs/ 用 /review 别管 /usr"
	tt := helloTester(a)
	snapshot(t, tt, "composer-marks")
	type span struct{ start, end int }
	var got []span
	for _, r := range a.draftMarks() {
		got = append(got, span{r.Start, r.End})
	}
	// 看看 is runes 0–1; @add.go 3–9, @docs/ 13–18, /review 22–28.
	want := []span{{3, 4}, {4, 10}, {13, 14}, {14, 19}, {22, 23}, {23, 29}}
	if !slices.Equal(got, want) {
		t.Fatalf("marks %v, want %v", got, want)
	}
	a.mention.open, a.mention.at = true, 13
	if n := len(a.draftMarks()); n != 4 {
		t.Errorf("the @ word being written is marked: %d marks", n)
	}
}

// TestComposerKeepsStillWhileRunning: sending a prompt and the turn running
// leave the composer where it is; nothing comes under it to push it up.
func TestComposerKeepsStillWhileRunning(t *testing.T) {
	a := emptySession(proto.Workspace{Branch: "main", Commits: []proto.CommitSummary{{Hash: "a", Subject: "x", Time: now.Unix()}}})
	tt := helloTester(a)
	idle, _ := tt.Find("手动")
	a.C.Entries = []*client.Entry{{Key: 1, Role: client.User, Text: "写一个计算器", Done: true}}
	a.C.Busy = true
	a.C.Turns = []client.Turn{{Seq: 1, Start: now.Add(-time.Second)}}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if run, _ := tt.Find("手动"); run.Y != idle.Y {
		t.Errorf("the composer went from y %v to %v as the turn ran", idle.Y, run.Y)
	}
	if tt.HasText("esc 中断") || tt.HasText("1 秒 · esc 中断") {
		t.Errorf("a line under the composer tells the time: %q", tt.Texts())
	}
	a.C.Busy = false
	a.C.Turns[0].Done = true
	tt.Frame()
	tt.Advance(time.Second)
	if done, _ := tt.Find("手动"); done.Y != idle.Y {
		t.Errorf("the composer went from y %v to %v as the turn ended", idle.Y, done.Y)
	}
}

func TestComposerKeepsStillWithAnError(t *testing.T) {
	a := emptySession(proto.Workspace{Branch: "main", Commits: []proto.CommitSummary{{Hash: "a", Subject: "x", Time: now.Unix()}}})
	a.C.Entries = []*client.Entry{{Key: 1, Role: client.User, Text: "写一个计算器", Done: true}}
	tt := helloTester(a)
	tt.Advance(time.Second)
	tt.Frame()
	before, _ := tt.Find("手动")
	a.err = "打不开 Xcode：没有这个应用"
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText(a.err) {
		t.Fatalf("the error does not show: %q", tt.Texts())
	}
	if after, _ := tt.Find("手动"); after.Y != before.Y {
		t.Errorf("the error moved the composer from y %v to %v", before.Y, after.Y)
	}
}
