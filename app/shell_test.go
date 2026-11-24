package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
)

func TestSidebarToggle(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if err := tt.Click("隐藏边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if a.side.sideOpen <= 0 || a.side.sideOpen >= 1 {
		t.Fatalf("the sidebar did not slide: %v", a.side.sideOpen)
	}
	tt.Advance(time.Second)
	if a.side.sideOpen > 0.001 || tt.HasText("技能与 MCP") {
		t.Fatalf("the sidebar stayed: %v", a.side.sideOpen)
	}
	snapshot(t, tt, "sidebar-hidden")
	// The toggle has not moved, so the same click brings the sidebar back.
	if err := tt.Click("显示边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if a.side.sideOpen < .999 || !tt.HasText("技能与 MCP") {
		t.Fatalf("the sidebar did not come back: %v", a.side.sideOpen)
	}
}

func TestVibrantSidebarVisible(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.SetVibrancy(true)
	tt.Frame()
	for _, s := range []string{"搜索", "新会话", "技能与 MCP"} {
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

// TestSidebarSlidesWithoutFading: while the sidebar slides, its wash over
// the window's glass stays as it is, so the sidebar does not flash gray.
func TestSidebarSlidesWithoutFading(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.SetVibrancy(true)
	tt.Frame()
	r, ok := tt.Find("技能与 MCP")
	if !ok {
		t.Fatal("no sidebar")
	}
	img := tt.Image()
	scale := float32(img.Bounds().Dx()) / 1200
	x, y := int(4*scale), int((r.Y+r.H+40)*scale)
	open := img.RGBAAt(x, y)
	a.toggleSide()
	checked := 0
	for i := 0; i < 60 && a.side.sideOpen > 0.05; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if a.side.sideOpen <= 0.05 || a.side.sideOpen >= 0.95 {
			continue
		}
		checked++
		if got := tt.Image().RGBAAt(x, y); got != open {
			t.Fatalf("at %.2f open the sidebar is %v, not %v as when open", a.side.sideOpen, got, open)
		}
	}
	if checked < 3 {
		t.Fatalf("only %d frames of the slide", checked)
	}
}
