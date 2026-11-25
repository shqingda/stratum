package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func TestLRUCache(t *testing.T) {
	c := newLRU[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("get a = %d, %v", v, ok)
	}
	c.Put("c", 3) // evicts "b"
	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b to be evicted")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("expected a to stay, got %d, %v", v, ok)
	}
	c.Put("d", 4) // evicts "c"
	c.DeleteFunc(func(k string, _ int) bool { return k == "d" })
	if c.Len() != 1 {
		t.Fatalf("len = %d, want 1", c.Len())
	}
}

func TestPaletteLerp(t *testing.T) {
	if p := PaletteLerp(0); p != light || p.Dark {
		t.Fatalf("t=0 should return light palette")
	}
	if p := PaletteLerp(1); p != dark || !p.Dark {
		t.Fatalf("t=1 should return dark palette")
	}
	mid := PaletteLerp(0.5)
	if !mid.Dark {
		t.Fatal("t=0.5 should have Dark=true")
	}
	if mid.Surface == light.Surface || mid.Surface == dark.Surface {
		t.Fatal("t=0.5 Surface should be interpolated")
	}
}

func TestSyncAnnounce(t *testing.T) {
	a := New(client.New("/w"))
	a.C.Current = &proto.Session{ID: "s1", Cwd: "/w"}
	tt := ui.NewTester(a.View, 1200, 780)
	// First frame establishes baseline without announcing.
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("initial frame announced: %v", got)
	}

	// New approval triggers announcement once.
	a.C.Approvals = []proto.Approval{{ID: "ap1", Tool: "shell"}}
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 || got[0] != locales.NotifyWait() {
		t.Fatalf("approval announcements = %v", got)
	}
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("duplicate approval announcement: %v", got)
	}

	// Resolving approval and ending a busy turn triggers NotifyDone.
	a.C.Approvals = nil
	a.C.Busy = true
	tt.Frame()
	_ = tt.Announcements()
	a.C.Busy = false
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 || got[0] != locales.NotifyDone() {
		t.Fatalf("turn end announcements = %v", got)
	}

	// Switching session does not spuriously announce.
	a.C.Current = &proto.Session{ID: "s2", Cwd: "/w"}
	a.C.Approvals = []proto.Approval{{ID: "ap2", Tool: "shell"}}
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("session switch announced: %v", got)
	}
}

func TestDiffHashSkipsRecomputeOnUnchangedWorkspaceRefresh(t *testing.T) {
	a := New(client.New("/w"))
	a.C.Phase = client.Ready
	a.C.Current = &proto.Session{ID: "s1", Cwd: "/w"}
	a.rv.open = true
	a.rv.uncommitted = true

	before, after := "a\nb\n", "a\nc\n"
	a.C.Workspace.Read = 1
	a.C.Workspace.Files = []proto.ChangeFile{{Path: "/w/foo.go", Added: 1, Removed: 1}}
	a.C.Workspace.Diffs = map[string]proto.ChangeDiff{
		"/w/foo.go": {Path: "/w/foo.go", Before: &before, After: &after},
	}
	a.syncReview()
	key := client.DiffKey(0, "/w/foo.go")
	fd1 := a.fileDiff(key)
	if fd1 == nil {
		t.Fatal("expected initial fileDiff")
	}

	// Workspace refreshes with identical content (new string pointers).
	b2, a2 := "a\nb\n", "a\nc\n"
	a.C.Workspace.Read = 2
	a.C.Workspace.Diffs = map[string]proto.ChangeDiff{
		"/w/foo.go": {Path: "/w/foo.go", Before: &b2, After: &a2},
	}
	a.syncReview()
	fd2 := a.fileDiff(key)
	if fd2 != fd1 {
		t.Fatal("expected cached *fileDiff pointer to be reused when content hash matches")
	}

	// Workspace refreshes with changed content: recomputes.
	a3 := "a\nd\n"
	a.C.Workspace.Read = 3
	a.C.Workspace.Diffs = map[string]proto.ChangeDiff{
		"/w/foo.go": {Path: "/w/foo.go", Before: &b2, After: &a3},
	}
	a.syncReview()
	fd3 := a.fileDiff(key)
	if fd3 == fd1 {
		t.Fatal("expected new *fileDiff when content changes")
	}
}

func TestFindIndexCachesLowercase(t *testing.T) {
	a := New(client.New("/w"))
	a.C.Current = &proto.Session{ID: "s1", Cwd: "/w"}
	a.find.on = true
	a.find.query = "HELLO"
	e := &client.Entry{Key: 42, Role: client.Assistant, Text: "Hello World"}
	items := []item{{e: e}}

	a.findIn(items)
	if len(a.find.hits) != 1 || a.find.hits[0] != 0 {
		t.Fatalf("hits = %v", a.find.hits)
	}
	fl, ok := a.find.idx[42]
	if !ok || fl.text != "hello world" {
		t.Fatalf("cached lower = %+v, ok=%v", fl, ok)
	}

	// Streaming append updates cached lowercase when length changes.
	e.Text = "Hello World Again"
	a.find.query = "again"
	a.findIn(items)
	if len(a.find.hits) != 1 || a.find.idx[42].text != "hello world again" {
		t.Fatalf("updated cache = %+v", a.find.idx[42])
	}
}
