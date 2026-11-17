package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
)

func TestSidebarToggle(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if err := tt.Click("隐藏边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if a.sideOpen <= 0 || a.sideOpen >= 1 {
		t.Fatalf("the sidebar did not slide: %v", a.sideOpen)
	}
	tt.Advance(time.Second)
	if a.sideOpen > 0.001 || tt.HasText("技能与 MCP") {
		t.Fatalf("the sidebar stayed: %v", a.sideOpen)
	}
	snapshot(t, tt, "sidebar-hidden")
	// The toggle has not moved, so the same click brings the sidebar back.
	if err := tt.Click("显示边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if a.sideOpen < .999 || !tt.HasText("技能与 MCP") {
		t.Fatalf("the sidebar did not come back: %v", a.sideOpen)
	}
}

func TestVibrantSidebarVisible(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.SetVibrancy(true)
	tt.Frame()
	for _, s := range []string{"Talos", "新会话", "技能与 MCP"} {
		r, ok := tt.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 || r.X >= sidebarWidth {
			t.Fatalf("vibrant sidebar item %q invalid rect: ok=%v rect=%+v", s, ok, r)
		}
	}
}

func TestCopyMessages(t *testing.T) {
	for _, e := range []*client.Entry{
		{Key: 1, Role: client.User, Text: "修一下 Add", Done: true},
		{Key: 2, Role: client.Assistant, Text: "改好了。", Done: true},
	} {
		a := working()
		a.C.Busy, a.C.Approvals = false, nil
		a.C.Entries = []*client.Entry{e}
		tt := ui.NewTester(a.View, 1200, 800)
		tt.Frame()
		r, ok := tt.Find(e.Text)
		if !ok {
			t.Fatalf("no %q", e.Text)
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Advance(200 * time.Millisecond)
		snapshot(t, tt, "message-hover-"+string(rune('0'+e.Key)))
		if err := tt.Click("复制"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if tt.Clipboard() != e.Text {
			t.Fatalf("copied %q, want %q", tt.Clipboard(), e.Text)
		}
	}
}
