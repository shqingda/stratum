package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

func TestErgonomicsSplitWidths(t *testing.T) {
	a := task8App()
	if got := a.sideWidth(); got != sidebarWidth {
		t.Fatalf("default sideWidth = %v, want %v", got, sidebarWidth)
	}
	a.resizeSide(40)
	if got := a.sideWidth(); got != sidebarWidth+40 {
		t.Fatalf("resized sideWidth = %v, want %v", got, sidebarWidth+40)
	}
	a.resizeSide(500)
	if got := a.sideWidth(); got != sideMaxWidth {
		t.Fatalf("clamped max sideWidth = %v, want %v", got, sideMaxWidth)
	}
	a.resizeSide(-500)
	if got := a.sideWidth(); got != sideMinWidth {
		t.Fatalf("clamped min sideWidth = %v, want %v", got, sideMinWidth)
	}
	a.resetSideWidth()
	if got := a.sideWidth(); got != sidebarWidth {
		t.Fatalf("reset sideWidth = %v, want %v", got, sidebarWidth)
	}

	if got := a.reviewWidth(); got != reviewWidth {
		t.Fatalf("default reviewWidth = %v, want %v", got, reviewWidth)
	}
	// Dragging left edge left (dx < 0) widens the review panel.
	a.resizeReview(-50)
	if got := a.reviewWidth(); got != reviewWidth+50 {
		t.Fatalf("resized reviewWidth = %v, want %v", got, reviewWidth+50)
	}
	a.resizeReview(-1000)
	if got := a.reviewWidth(); got != reviewMaxWidth {
		t.Fatalf("clamped max reviewWidth = %v, want %v", got, reviewMaxWidth)
	}
	a.resizeReview(1000)
	if got := a.reviewWidth(); got != reviewMinWidth {
		t.Fatalf("clamped min reviewWidth = %v, want %v", got, reviewMinWidth)
	}
	a.resetReviewWidth()
	if got := a.reviewWidth(); got != reviewWidth {
		t.Fatalf("reset reviewWidth = %v, want %v", got, reviewWidth)
	}
}

func TestErgonomicsInspectorAndFindShortcuts(t *testing.T) {
	a := task8App()
	a.C.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "needle first"},
		{Key: 2, Role: client.Assistant, Text: "needle second", Done: true},
	}
	tt := task8Tester(a, false)

	// Cmd+Alt+I toggles the review inspector.
	if a.rv.open {
		t.Fatal("review panel should start closed")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyI)
	if !a.rv.open {
		t.Fatal("Cmd+Alt+I did not open review inspector")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyI)
	if a.rv.open {
		t.Fatal("second Cmd+Alt+I did not close review inspector")
	}

	// Cmd+F opens find, Cmd+G / Shift+Cmd+G steps through hits.
	tt.Key(ui.Cmd, ui.KeyF)
	if !a.find.on {
		t.Fatal("Cmd+F did not open find bar")
	}
	tt.Type("needle")
	if len(a.find.hits) != 2 || a.find.at != 0 {
		t.Fatalf("find hits = %v at %d, want 2 hits at 0", a.find.hits, a.find.at)
	}
	tt.Key(ui.Cmd, ui.KeyG)
	if a.find.at != 1 {
		t.Fatalf("Cmd+G at = %d, want 1", a.find.at)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	if a.find.at != 0 {
		t.Fatalf("Shift+Cmd+G at = %d, want 0", a.find.at)
	}
}

func TestErgonomicsPaletteWrapAndAutomationsEsc(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)

	// Palette Up from index 0 wraps to the last item.
	tt.Key(ui.Cmd, ui.KeyK)
	if !a.pal || a.palSel != 0 {
		t.Fatalf("palette open=%v sel=%d", a.pal, a.palSel)
	}
	tt.Key(0, ui.KeyUp)
	if a.palSel == 0 {
		t.Fatal("Up at index 0 did not wrap around to last item")
	}
	tt.Key(0, ui.KeyDown)
	if a.palSel != 0 {
		t.Fatalf("Down from last item did not wrap to 0, got %d", a.palSel)
	}
	tt.Key(0, ui.KeyEscape)

	// Automations Escape closes delete confirmation first, then the sheet.
	a.automationsOpen = true
	a.automationsUI.deletingID = "auto-1"
	a.automationsUI.deletingName = "Daily check"
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if a.automationsUI.deletingID != "" || !a.automationsOpen {
		t.Fatalf("first Esc should close delete dialog only: deleting=%q open=%v", a.automationsUI.deletingID, a.automationsOpen)
	}
	tt.Key(0, ui.KeyEscape)
	if a.automationsOpen {
		t.Fatal("second Esc should close automations view")
	}
}

func TestErgonomicsReduceMotionSetting(t *testing.T) {
	a := task8App()
	a.C.Settings = &proto.Settings{ReduceMotion: true}
	if !a.reduceMotion() {
		t.Fatal("reduceMotion() should reflect Settings.ReduceMotion")
	}
	tt := task8Tester(a, false)
	tt.Frame()
}

func TestErgonomicsCmdWLayeredCloseAndAgentShortcuts(t *testing.T) {
	a := task8App()
	a.C.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "hi"},
		{Key: 2, Role: client.Assistant, Text: "hello world", Done: true},
	}
	closed := false
	a.SetCloseWindow(func() { closed = true })
	tt := task8Tester(a, false)

	// 1. Cmd+W closes Settings first, not the main window.
	a.openSettings()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyW)
	if a.settingsOpen || closed {
		t.Fatalf("first Cmd+W in settings: settingsOpen=%v closed=%v", a.settingsOpen, closed)
	}
	// 2. Second Cmd+W on main window closes the window.
	tt.Key(ui.Cmd, ui.KeyW)
	if !closed {
		t.Fatal("second Cmd+W on main window did not close window")
	}

	// 3. Cmd+Alt+C copies latest assistant reply.
	tt.Key(ui.Cmd|ui.Alt, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != "hello world" {
		t.Fatalf("Cmd+Alt+C clipboard = %q, want %q", got, "hello world")
	}

	// 4. Cmd+/ toggles model menu.
	tt.Key(ui.Cmd, ui.KeySlash)
	if !a.modelMenu {
		t.Fatal("Cmd+/ did not open model menu")
	}
	tt.Key(ui.Cmd, ui.KeySlash)
	if a.modelMenu {
		t.Fatal("second Cmd+/ did not close model menu")
	}

	// 5. Cmd+Shift+P opens command palette.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	if !a.pal {
		t.Fatal("Cmd+Shift+P did not open palette")
	}
	tt.Key(ui.Cmd, ui.KeyW)
	if a.pal {
		t.Fatal("Cmd+W did not close palette")
	}

	// 6. Shift+Cmd+A toggles archive / unarchive on the current session.
	for len(a.out) > 0 {
		<-a.out
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
	var flag proto.SessionFlag
	task8Command(t, a, proto.MethodSessionFlag, &flag)
	if flag.Archived == nil || !*flag.Archived {
		t.Fatalf("Shift+Cmd+A archive flag = %+v, want Archived=true", flag)
	}
	a.C.Current.Archived = true
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
	flag = proto.SessionFlag{}
	task8Command(t, a, proto.MethodSessionFlag, &flag)
	if flag.Archived == nil || *flag.Archived {
		t.Fatalf("Shift+Cmd+A unarchive flag = %+v, want Archived=false", flag)
	}
}
