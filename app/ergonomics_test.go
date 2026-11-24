package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func TestErgonomicsSplitWidths(t *testing.T) {
	a := task8App()
	a.C.Settings = &proto.Settings{}
	letGo := func() { a.split = splitDrag{}; a.saveWidths() }
	if got := a.sideWidth(); got != sidebarWidth {
		t.Fatalf("default sideWidth = %v, want %v", got, sidebarWidth)
	}
	a.resizeSide(40)
	if got := a.sideWidth(); got != sidebarWidth+40 {
		t.Fatalf("resized sideWidth = %v, want %v", got, sidebarWidth+40)
	}
	letGo()
	if got := a.C.Settings.SidebarWidth; got != sidebarWidth+40 {
		t.Fatalf("saved sidebar width = %v, want %v", got, sidebarWidth+40)
	}
	// Past the limit the seam stops, and meets the pointer again where it
	// left it, as an NSSplitView's does.
	a.resizeSide(200)
	if got := a.sideWidth(); got != sideMaxWidth {
		t.Fatalf("clamped max sideWidth = %v, want %v", got, sideMaxWidth)
	}
	a.resizeSide(-100)
	if got := a.sideWidth(); got != sideMaxWidth {
		t.Fatalf("the seam left the limit before the pointer came back: %v", got)
	}
	a.resizeSide(-100)
	if got := a.sideWidth(); got != sidebarWidth+40 {
		t.Fatalf("the seam lost the pointer: %v, want %v", got, sidebarWidth+40)
	}
	letGo()
	a.resetSideWidth()
	if got := a.sideWidth(); got != sidebarWidth || a.C.Settings.SidebarWidth != 0 {
		t.Fatalf("reset sideWidth = %v (saved %v), want %v", got, a.C.Settings.SidebarWidth, sidebarWidth)
	}
	a.resizeSide(-60)
	if got := a.sideWidth(); got != sideMinWidth || a.noSide {
		t.Fatalf("the sidebar at its narrowest is %v wide, hidden %v", got, a.noSide)
	}
	a.resizeSide(-100)
	if !a.noSide || a.sideWidth() != sidebarWidth {
		t.Fatalf("dragged past its narrowest the sidebar stayed (hidden %v, %v wide)", a.noSide, a.sideWidth())
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
	letGo()
	if got := a.C.Settings.InspectorWidth; got != reviewMaxWidth {
		t.Fatalf("saved review width = %v, want %v", got, reviewMaxWidth)
	}
	a.rv.open = true
	a.resizeReview(1000)
	if a.rv.open || a.reviewWidth() != reviewMaxWidth {
		t.Fatalf("dragged closed the panel stayed open %v at %v", a.rv.open, a.reviewWidth())
	}
	a.resetReviewWidth()
	if got := a.reviewWidth(); got != reviewWidth {
		t.Fatalf("reset reviewWidth = %v, want %v", got, reviewWidth)
	}

	// A new window starts at the saved widths.
	b := task8App()
	b.C.Settings = &proto.Settings{SidebarWidth: 300, InspectorWidth: 500}
	if b.sideWidth() != 300 || b.reviewWidth() != 500 {
		t.Fatalf("saved widths not used: %v, %v", b.sideWidth(), b.reviewWidth())
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

func TestErgonomicsPerSessionTranscriptList(t *testing.T) {
	a := task8App()
	a.C.Current = &proto.Session{ID: "s1", Cwd: "/tmp/proj"}
	s1List := a.transcriptList()
	if s1List == nil {
		t.Fatal("transcriptList returned nil for s1")
	}
	a.C.Current = &proto.Session{ID: "s2", Cwd: "/tmp/proj"}
	s2List := a.transcriptList()
	if s2List == s1List {
		t.Fatal("transcriptList returned same ListState for s1 and s2")
	}
	a.C.Current = &proto.Session{ID: "s1", Cwd: "/tmp/proj"}
	if got := a.transcriptList(); got != s1List {
		t.Fatal("transcriptList did not preserve s1 ListState on switch back")
	}
}

func TestErgonomicsProjectGroupCollapse(t *testing.T) {
	a := task8App()
	a.C.Sessions = []proto.Session{
		{ID: "s1", Title: "Alpha Session", Cwd: "/tmp/alpha"},
		{ID: "s2", Title: "Beta Session", Cwd: "/tmp/beta"},
	}
	a.C.Current = &a.C.Sessions[1]
	tt := task8Tester(a, false)
	if !tt.HasText("Alpha Session") {
		t.Fatal("expected Alpha Session visible before collapse")
	}
	for len(a.out) > 0 {
		<-a.out
	}
	if err := tt.Click("alpha"); err != nil {
		t.Fatalf("Click(alpha): %v", err)
	}
	var saved proto.Settings
	task8Command(t, a, proto.MethodSetSettings, &saved)
	if len(saved.CollapsedProjects) != 1 || saved.CollapsedProjects[0] != "/tmp/alpha" {
		t.Fatalf("CollapsedProjects = %v, want [/tmp/alpha]", saved.CollapsedProjects)
	}
	a.C.Settings.CollapsedProjects = saved.CollapsedProjects
	tt.Frame()
	if tt.HasText("Alpha Session") {
		t.Fatal("expected Alpha Session hidden after collapse")
	}
	if !tt.HasText("Beta Session") {
		t.Fatal("expected Beta Session still visible")
	}
	if got := a.sessionOrder(); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("sessionOrder = %v, want [s2]", got)
	}
}

func TestErgonomicsNarrowWindowRules(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)

	// Open review inspector at 1200pt: 264 + 452 + 600 = 1316 > 1200, so sidebar collapses, no overlay.
	a.rv.open = true
	tt.Frame()
	if !a.noSide || !a.sideMoved {
		t.Fatalf("expected sidebar collapsed at 1200pt with review open, got noSide=%v sideMoved=%v", a.noSide, a.sideMoved)
	}
	if a.rvOverlay {
		t.Fatal("expected rvOverlay=false at 1200pt")
	}

	// Narrow to 760pt (< 452 + 600 = 1052): review panel becomes overlay.
	tt.SetSize(760, 480)
	tt.Frame()
	if !a.rvOverlay {
		t.Fatal("expected rvOverlay=true at 760pt")
	}

	// Widen to 1400pt: sidebar is restored and overlay turns off.
	tt.SetSize(1400, 780)
	tt.Frame()
	if a.rvOverlay {
		t.Fatal("expected rvOverlay=false at 1400pt")
	}
	if a.noSide || a.sideMoved {
		t.Fatalf("expected sidebar restored at 1400pt, got noSide=%v sideMoved=%v", a.noSide, a.sideMoved)
	}

	// Narrow to 760pt again and close review panel: sidebar restores and overlay turns off.
	tt.SetSize(760, 480)
	tt.Frame()
	a.rv.open = false
	tt.Frame()
	if a.rvOverlay || a.noSide || a.sideMoved {
		t.Fatalf("expected overlay off and sidebar restored after closing review, got overlay=%v noSide=%v sideMoved=%v", a.rvOverlay, a.noSide, a.sideMoved)
	}
}

func TestCoreTextOpticalTracking(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core Text optical tracking check is macOS-only")
	}
	var w11, w11Spaced, w22 float32
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).AlignItems(ui.Start).Children(func() {
			e11 := ui.Text(c, "Stratum 1234567890").FontSize(11)
			e11Spaced := ui.Text(c, "Stratum 1234567890").FontSize(11).LetterSpacing(0.11)
			e22 := ui.Text(c, "Stratum 1234567890").FontSize(22)
			w11 = e11.Bounds().W
			w11Spaced = e11Spaced.Bounds().W
			w22 = e22.Bounds().W
		})
	}, 400, 160)
	tt.Frame()
	t.Logf("CoreText widths: 11pt=%.3f, 11pt+0.11=%.3f, 22pt=%.3f, 2*w11=%.3f (ratio=%.4f)",
		w11, w11Spaced, w22, w11*2, (w11*2)/w22)
	if w11 <= 0 || w11Spaced <= 0 || w22 <= 0 {
		t.Fatalf("expected positive text widths, got w11=%v w11Spaced=%v w22=%v", w11, w11Spaced, w22)
	}
	if (w11*2)/w22 <= 1.01 {
		t.Fatalf("expected Core Text to apply automatic positive optical tracking at 11pt vs 22pt, got ratio=%.4f", (w11*2)/w22)
	}
}

func TestErgonomicsScrollEdgePanes(t *testing.T) {
	a := task8App()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Title\n\nBody text\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a.C.Current.Cwd = dir
	a.C.Files = []string{"main.go", "README.md"}
	a.C.Entries = append(a.C.Entries, &client.Entry{
		Key:  -99,
		Role: client.Tool,
		Text: "update_plan",
		Args: json.RawMessage(`{"title":"Plan","steps":[{"step":"Step 1","status":"done"},{"step":"Step 2","status":"in_progress","note":"working"}]}`),
	})

	a.rv.open = true
	a.rv.tab = "files"
	tt := task8Tester(a, false)
	if tt.Image() == nil {
		t.Fatal("expected non-nil image for files tree")
	}

	// Open markdown preview and code preview inside filesPane.
	a.openFilePreview(dir, "README.md")
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("expected non-nil image for markdown preview")
	}
	a.openFilePreview(dir, "main.go")
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("expected non-nil image for code preview")
	}

	// Switch to planPane.
	a.rv.tab = "plan"
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("expected non-nil image for plan pane")
	}

	// Switch to native Changes view.
	a.rv.tab = ""
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("expected non-nil image for native changes pane")
	}
}

func TestErgonomicsHapticFeedback(t *testing.T) {
	a := task8App()
	a.C.Settings = &proto.Settings{}
	var got []hapticPattern
	a.haptic = func(p hapticPattern) {
		got = append(got, p)
	}

	// Dragging sidebar past collapse threshold fires alignment haptic.
	a.resizeSide(-sideMinWidth)
	if !a.noSide || !slices.Equal(got, []hapticPattern{hapticAlignment}) {
		t.Fatalf("expected alignment haptic on sidebar collapse, got noSide=%v haptics=%v", a.noSide, got)
	}

	// Resetting split widths and clicking tabs/scopes do not fire haptics.
	got = nil
	a.resetSideWidth()
	a.resetReviewWidth()
	if len(got) != 0 {
		t.Fatalf("expected no haptics on width resets, got %v", got)
	}

	// Dragging review panel past collapse threshold fires alignment haptic.
	got = nil
	a.rv.open = true
	a.resizeReview(reviewMinWidth)
	if a.rv.open || !slices.Equal(got, []hapticPattern{hapticAlignment}) {
		t.Fatalf("expected alignment haptic on review collapse, got open=%v haptics=%v", a.rv.open, got)
	}

	// Switching right-panel tab and Changes scope does not fire haptics.
	got = nil
	a.rv.open = true
	a.rv.tab = ""
	tt := task8Tester(a, false)
	tt.Click(locales.Plan())
	tt.Frame()
	if len(got) != 0 {
		t.Fatalf("expected no haptics when switching tab, got %v", got)
	}

	got = nil
	a.rv.tab = ""
	tt.Frame()
	tt.Click(locales.Uncommitted())
	tt.Frame()
	if len(got) != 0 {
		t.Fatalf("expected no haptics when switching scope, got %v", got)
	}
}

func TestErgonomicsUnreadShortcutEditResendAndRewindDigits(t *testing.T) {
	a := task8App()
	a.C.Turns = []client.Turn{{Seq: 1, Done: true}, {Seq: 2, Done: true}}
	a.C.Entries = []*client.Entry{
		{Key: 1, Role: client.User, Text: "first prompt", Turn: 0},
		{Key: 2, Role: client.Assistant, Text: "first answer", Done: true, Turn: 0},
		{Key: 3, Role: client.User, Text: "second prompt", Turn: 1},
		{Key: 4, Role: client.Assistant, Text: "second answer", Done: true, Turn: 1},
	}
	tt := task8Tester(a, false)

	// 1. ⌘⇧U marks current session unread.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyU)
	tt.Frame()
	if a.C.Marks[a.C.Current.ID] != "unread" {
		t.Fatalf("⌘⇧U mark = %q, want unread", a.C.Marks[a.C.Current.ID])
	}
	for len(a.out) > 0 {
		<-a.out
	}

	// 2. Rewind menu supports 1-9 direct selection.
	a.openRewind()
	tt.Frame()
	if !a.rewind.open || a.rewind.step != "prompts" {
		t.Fatalf("expected rewind prompts open, got open=%v step=%q", a.rewind.open, a.rewind.step)
	}
	if !a.rewindKey(ui.TextKey{Key: ui.Key2}) {
		t.Fatal("expected rewindKey to consume Key2")
	}
	if a.rewind.step != "actions" || a.rewind.prompt != "second prompt" {
		t.Fatalf("Key2 picked step=%q prompt=%q, want actions / second prompt", a.rewind.step, a.rewind.prompt)
	}
	a.rewind = rewind{}

	// 3. editAndResend puts prompt back in draft and sends MethodRewind(conversation).
	a.editAndResend(nil, 2, "second prompt")
	if a.draft != "second prompt" {
		t.Fatalf("editAndResend draft = %q, want second prompt", a.draft)
	}
	cmds, ok := next(a)
	if !ok || len(cmds) != 1 || cmds[0].Method != proto.MethodRewind {
		t.Fatalf("expected MethodRewind command, got ok=%v cmds=%+v", ok, cmds)
	}
	var rw proto.Rewind
	if err := json.Unmarshal(cmds[0].Params, &rw); err != nil {
		t.Fatal(err)
	}
	if rw.TurnSeq != 2 || rw.Action != "conversation" {
		t.Fatalf("unexpected Rewind params: %+v", rw)
	}
}

func TestRetitleSuggestionAndCmdHoldBadges(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)

	// 1. Retitle emits MethodSessionRetitle and updates session title on response.
	cmds, err := a.C.Retitle(a.C.Current.ID)
	if err != nil || len(cmds) != 1 || cmds[0].Method != proto.MethodSessionRetitle {
		t.Fatalf("Retitle cmds = %+v, err=%v", cmds, err)
	}
	a.C.Receive(proto.Frame{
		Type:   proto.FrameResponse,
		ID:     cmds[0].ID,
		Result: json.RawMessage(`{"id":"1","title":"Refactored Title","cwd":"/w"}`),
	})
	if a.C.Current.Title != "Refactored Title" {
		t.Fatalf("Current.Title = %q, want Refactored Title", a.C.Current.Title)
	}

	// 2. Post-reply suggestion: Escape dismisses, Tab accepts into composer draft.
	b, _ := json.Marshal(proto.Suggestion{Text: "add edge case tests"})
	a.C.Receive(proto.Frame{
		Type: proto.FrameEvent,
		Event: &proto.Event{
			SessionID: a.C.Current.ID,
			Type:      proto.EventSuggestion,
			Data:      b,
		},
	})
	tt.Frame()
	if a.activeSuggestion() != "add edge case tests" {
		t.Fatalf("activeSuggestion = %q", a.activeSuggestion())
	}
	if !tt.HasText("⇥ Tab") {
		t.Fatalf("expected ⇥ Tab pill when suggestion is active")
	}
	if err := tt.Click(locales.AcceptSuggestion()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.draft != "add edge case tests" || a.C.Suggestion != "" {
		t.Fatalf("expected clicking suggestion pill to accept: draft=%q suggestion=%q", a.draft, a.C.Suggestion)
	}

	a.draft = ""
	a.C.Suggestion = "run go test"
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if a.draft != "run go test" || a.C.Suggestion != "" {
		t.Fatalf("expected Tab to accept suggestion: draft=%q suggestion=%q", a.draft, a.C.Suggestion)
	}

	// 3. Holding Cmd shows ⌘1 badge in sidebar; releasing hides it.
	tt.HoldModifiers(ui.Cmd)
	tt.Frame()
	if !tt.HasText("⌘1") {
		t.Fatalf("expected ⌘1 badge when Cmd held, texts=%q", tt.Texts())
	}
	tt.HoldModifiers(0)
	tt.Frame()
	if tt.HasText("⌘1") {
		t.Fatalf("expected ⌘1 badge hidden after releasing Cmd")
	}
}
