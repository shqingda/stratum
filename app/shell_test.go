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
	if a.side.sideOpen > 0.001 || tt.HasText("首页文案改写") {
		t.Fatalf("the sidebar stayed: %v", a.side.sideOpen)
	}
	snapshot(t, tt, "sidebar-hidden")
	// The toggle brings the sidebar back.
	if err := tt.Click("显示边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if a.side.sideOpen < .999 || !tt.HasText("首页文案改写") {
		t.Fatalf("the sidebar did not come back: %v", a.side.sideOpen)
	}
}

func TestRailAndSidebarVisible(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	for _, s := range []string{"新会话", "扩展", "设置"} {
		r, ok := tt.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 || r.X >= railWidth {
			t.Fatalf("rail item %q invalid rect: ok=%v rect=%+v", s, ok, r)
		}
	}
	for _, s := range []string{"全部", "修复 Add 测试"} {
		r, ok := tt.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 || r.X < railWidth || r.X >= railWidth+sidebarWidth {
			t.Fatalf("sidebar item %q invalid rect: ok=%v rect=%+v", s, ok, r)
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

// TestSidebarSlidesWithoutFading verifies the opaque M3 drawer slides cleanly without grey wash flashing.
func TestSidebarSlidesWithoutFading(t *testing.T) {
	a := working()
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	r, ok := tt.Find("修复 Add 测试")
	if !ok {
		t.Fatal("no sidebar")
	}
	img := tt.Image()
	scale := float32(img.Bounds().Dx()) / 1200
	x, y := int((railWidth+16)*scale), int((r.Y+r.H+40)*scale)
	open := img.RGBAAt(x, y)
	a.toggleSide()
	checked := 0
	for i := 0; i < 60 && a.side.sideOpen > 0.15; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if a.side.sideOpen <= 0.15 || a.side.sideOpen >= 0.95 {
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
