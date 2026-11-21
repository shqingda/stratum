// Package app is Talos's window: a sidebar of sessions, the transcript of
// the one shown and the composer under it, drawn with mygo's native UI
// over a client.Client.
package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/rpc"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// App is the state of the window. Everything in it is touched on the UI
// thread only: frames from the kernel come in through update.
type App struct {
	C *client.Client

	out        chan []proto.Command
	desk       Desktop
	marks      map[string]string // the sessions' states desk last saw
	badge      int
	p          *Palette
	now        func() time.Time
	err        string // the last command that could not be sent
	draft      string // what the composer holds
	recall     string // a sent message ↑ brings back, for the next frame
	toEnd      bool   // put the composer's caret at the end of its draft
	mention    mention
	slash      slash
	rewind     rewind
	escAt      time.Time // the last Esc in an empty composer, for a second one
	slashRun   func(i int) // runs the / menu's item i, as last shown
	slashLen   int
	filesKey   string // the folder and @ query the files were asked for
	slashAsked string // the folder the / menu last read skills for
	find       find
	editing    string // the approval whose command is being changed
	cmdDraft   string // the command as changed
	cmdInput   ui.Handle

	renaming, renameIn string // the session being renamed, in "side" or "bar"
	renameDraft        string
	renameField        ui.Handle
	renameFocus        bool
	deleting           string // the session asked about deleting
	undone             undo   // the archiving that can be undone
	toast              struct {
		icon, text string
		at         time.Time
		said       bool // VoiceOver has been told
	}
	cardCache          map[string]parsedCard
	keptUnread         string // marked unread while shown
	showArchived       bool
	drafts             drafts       // keeps each session's draft in the kernel
	query              string                   // the sidebar's search
	lists              map[string]*ui.ListState // per-session transcript scroll state
	md                 markdown.Options
	opened             map[string]bool // tool calls whose output shows
	inWork             bool            // building a finished turn's folded work
	noSide             bool            // the sidebar is hidden
	// panelWas is whether the review panel was open last frame; sideMoved
	// that opening it in a narrow window put the sidebar away, so that
	// closing it brings the sidebar back.
	panelWas, sideMoved bool
	rvOverlay           bool    // review panel floats over main when window is very narrow
	lastW               float32 // last window width seen by fitPanel
	sideOpen            float32 // how far the sidebar is out, 0 to 1, on its way
	rvOpen              float32 // how far the review inspector is out, 0 to 1
	sideW               float32 // user-adjusted sidebar width, 0 means default
	rvW                 float32 // user-adjusted review width, 0 means default
	dockH               float32 // how tall the composer floating on the transcript was
	apDiffs             map[string][]diffLine // the approvals' diffs, by ID
	sys                 ui.Preferences        // the desktop's accessibility preferences, read each frame
	focusSession        string                // the session the composer last took the focus for
	overWas             bool                  // the palette, settings or automations showed last frame
	removing            []string              // the sessions of the project the user asked to remove, waiting on Remove
	removingName        string
	pal                 bool    // the ⌘K palette shows
	jump                int64   // a message to scroll to, from a search
	jumpIn              string  // the session it is in
	palSel              int     // its row chosen by the arrow keys
	palScroll           bool    // scroll palette selection into view once after ↑/↓
	stats               map[string][2]int
	rv                  review
	seen                map[string]time.Time // when approvals first showed
	openIn              func(app, dir string) error
	openURL             func(url string) error
	hasApp              func(appName string) bool
	// version is the app's, as packaged; checkUpdates asks the updater,
	// nil where there is none.
	version      string
	checkUpdates func()

	openMenu    bool
	commitMsg   string
	commitInput ui.Handle

	searchInput                                             ui.Handle
	composerInput                                           ui.Handle
	closeWindow                                             func()
	reconnect                                               func()
	keyRepeat                                               bool
	visibleSessions                                         []string
	menuSession                                             string
	modelMenu, effortMenu, modeMenu, detailMenu, folderMenu, worktreeMenu, branchMenu bool
	modelsRequested                                         bool
	modelsPending, modelsAccount, modelsError               string
	modelsLoadedAccount                                     string

	keyOpen, keyMenu bool
	keyProvider, key string

	settingsOpen, addingAccount, setModelMenu bool
	settingsPage                              string // a settingsPages id
	settingsQuery, keysQuery                  string
	subMenu, lightMenu                        bool
	accountsSeen                              int
	mcpUI                                     mcpState
	previewUI                                 previewState
	automationsOpen                           bool
	automationsUI                             automationsState
	locale                                    Locale
	theme                                     string // the appearance last applied

	attached        []attachment
	mentionSessions []string
	thumbs   map[string]ui.ImageSource // of sent pictures, by entry key and index
	pick     func() []string           // the open panel
	pickDir  func() string             // the open panel, for a folder
	update   func(func())              // runs on the UI thread, as Serve was given
	menuRuns []func(c *ui.Context)     // commands from the menu bar, for the next frame
}

// New returns an app over c, not connected yet.
func New(c *client.Client) *App {
	a := &App{C: c, now: time.Now, openURL: openBrowser, pick: pickFiles, pickDir: pickFolder, opened: map[string]bool{}, locale: &memLocale{},
		rv: review{asked: map[string]bool{}, diffs: map[string]*fileDiff{}}}
	a.openIn = a.openInBackground
	return a
}

// restoreFocus puts the keyboard back in the composer when the session
// changes, and when the palette, settings or automations close: what
// comes next is typing, as in Claude's and Codex's apps.
func (a *App) restoreFocus(c *ui.Context) {
	id := ""
	if a.C.Current != nil {
		id = a.C.Current.ID
	}
	over := a.pal || a.settingsOpen || a.automationsOpen
	if !over && (id != a.focusSession || a.overWas) && id != "" {
		a.composerInput.Focus(c)
	}
	a.focusSession, a.overWas = id, over
}

// openInBackground opens dir in app off the UI thread, as LaunchServices
// can take a while to answer: a click never holds the window up. A failure
// shows as the action's error once it comes.
func shortOpenErr(err error) string {
	s := strings.TrimSpace(err.Error())
	if i := strings.Index(s, " (Error Domain="); i > 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func (a *App) openInBackground(app, dir string) error {
	go func() {
		err := openIn(app, dir)
		if err == nil || a.update == nil {
			return
		}
		name := app
		if name == "" {
			name = filepath.Base(dir)
		}
		a.update(func() { a.flash("alert-circle", locales.CouldntOpen(name, shortOpenErr(err))) })
	}()
	return nil
}

// Serve connects the app to a kernel on in-process pipes until ctx ends.
// update runs a function on the UI thread and draws again after it, as
// mygo.Window.Update does.
func (a *App) Serve(ctx context.Context, srv *rpc.Server, update func(func())) {
	cr, cw := io.Pipe()
	fr, fw := io.Pipe()
	a.out = make(chan []proto.Command, 256)
	a.update = update
	go func() { srv.Serve(ctx, cr, fw); fw.Close() }()
	// Commands go out on their own goroutine, so that the UI thread never
	// waits on a pipe the kernel is not reading yet.
	go func() {
		for {
			select {
			case cmds := <-a.out:
				for _, cmd := range cmds {
					b, _ := json.Marshal(cmd)
					if _, err := cw.Write(append(b, '\n')); err != nil {
						return
					}
				}
			case <-ctx.Done():
				cw.Close()
				return
			}
		}
	}()
	go func() {
		sc := bufio.NewScanner(fr)
		sc.Buffer(nil, proto.MaxLine)
		for sc.Scan() {
			var f proto.Frame
			if json.Unmarshal(sc.Bytes(), &f) != nil {
				continue
			}
			update(func() { a.receive(f) })
		}
		err := sc.Err()
		if err == nil {
			err = io.EOF
		}
		update(func() { a.C.Disconnected(err); a.modelsPending = "" })
	}()
}

func (a *App) send(cmds []proto.Command) {
	if len(cmds) > 0 && a.out != nil {
		a.out <- cmds
	}
}

// do sends what an action of the user's returned, or keeps its error to
// show.
func (a *App) do(cmds []proto.Command, err error) {
	if err != nil {
		a.err = err.Error()
		return
	}
	a.err = ""
	a.send(cmds)
}

// quiet sends what the app asked for by itself, leaving the error of the
// user's last action as it is.
func (a *App) quiet(cmds []proto.Command, err error) {
	if err == nil {
		a.send(cmds)
	}
}

// View builds a frame of the window.
func (a *App) View(c *ui.Context) {
	a.syncMenus()
	a.syncReview()
	isDark := c.Theme().Dark
	a.p = PaletteFor(isDark)
	c.SetTheme(Theme(a.p, isDark))
	if z := textZoom(a.settings().TextSize); c.Zoom() != z {
		c.SetZoom(z)
	}
	a.sys = c.Preferences()
	if a.reduceMotion() {
		c.DisableAnimations()
	}
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	if a.needsLogin() {
		a.onboarding(c)
		a.keys(c)
		return
	}
	a.runMenu(c)
	a.syncDraft(c)
	a.markSeen(c)
	if a.settingsOpen {
		ui.Box(c.Key("view-settings")).Fill().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .985, Opacity: 0}, Duration: 180 * time.Millisecond}).
			Children(func() { a.settingsView(c, vibrant) })
		a.keys(c)
		return
	}
	if a.automationsOpen {
		ui.Box(c.Key("view-automations")).Fill().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .985, Opacity: 0}, Duration: 180 * time.Millisecond}).
			Children(func() { a.automationsView(c, vibrant) })
		a.keys(c)
		return
	}
	a.fitPanel(c)
	rvTarget := float32(0)
	if a.rv.open {
		rvTarget = 1
	}
	// Keep the review drawer spring on the root so collapsing the sidebar
	// never shifts child indices and replays the opening animation.
	a.rvOpen = c.Root().Spring("review", rvTarget, springDefault)
	shell := ui.Row(c).Fill().AlignItems(ui.Stretch)
	// One spring moves the sidebar and the title bar beside it, as versta
	// does: it can be turned around halfway, and the sidebar is clipped,
	// not squeezed, so its rows hold still while it slides.
	target := float32(1)
	if a.noSide {
		target = 0
	}
	a.sideOpen = shell.Spring("side", target, springDefault)
	sw := a.sideWidth()
	shell.Children(func() {
		if a.sideOpen > 0.001 {
			sideWrap := ui.Row(c).Width(a.sideOpen * sw).Shrink(0).AlignItems(ui.Stretch).Clip()
			if a.sideOpen < 0.999 {
				sideWrap.Translate(-(1-a.sideOpen)*10, 0).Opacity(min(1, a.sideOpen*1.35))
			}
			sideWrap.Children(func() { a.sidebar(c, vibrant) })
		} else {
			a.visibleSessions = a.sessionOrder()
		}
		a.main(c)
		if a.sideOpen > 0.99 {
			a.splitHandle(c, "split-side", sw, func(dx float32) { a.resizeSide(dx) }, a.resetSideWidth)
		}
		a.sideToggle(c)
	})
	a.palette(c)
	a.toastHUD(c)
	a.keys(c)
	a.restoreFocus(c)
}

// reduceMotion is whether to keep still what would move: as the user set
// in Talos, or in macOS (Accessibility › Display › Reduce motion).
func (a *App) reduceMotion() bool {
	return a.settings().ReduceMotion || a.sys.ReduceMotion
}

const (
	sideMinWidth   = 220
	sideMaxWidth   = 360
	reviewMinWidth = 360
	reviewMaxWidth = 680
)

func (a *App) sideWidth() float32 {
	if a.sideW <= 0 {
		return sidebarWidth
	}
	return max(sideMinWidth, min(sideMaxWidth, a.sideW))
}

func (a *App) reviewWidth() float32 {
	if a.rvW <= 0 {
		return reviewWidth
	}
	return max(reviewMinWidth, min(reviewMaxWidth, a.rvW))
}

func (a *App) resizeSide(dx float32) {
	a.sideW = max(sideMinWidth, min(sideMaxWidth, a.sideWidth()+dx))
}

func (a *App) resetSideWidth() {
	a.sideW = sidebarWidth
}

func (a *App) resizeReview(dx float32) {
	a.rvW = max(reviewMinWidth, min(reviewMaxWidth, a.reviewWidth()-dx))
}

func (a *App) resetReviewWidth() {
	a.rvW = reviewWidth
}

// splitHandle draws an 8pt ergonomic resize target centered on a pane seam.
// Hovering reveals a subtle accent hairline; double-clicking resets to default.
func (a *App) splitHandle(c *ui.Context, key string, x float32, onDrag func(dx float32), onReset func()) {
	p := a.p
	h := ui.Box(c.Key(key)).Absolute().Top(0).Bottom(0).Left(x - 4).Width(8).Cursor(ui.CursorResizeColumn).Center().ZIndex(12)
	if dx, _, ok := h.Dragged(); ok && dx != 0 {
		onDrag(dx)
	}
	if h.DoubleClicked() {
		onReset()
	}
	target := float32(0)
	if h.Pressed() {
		target = .85
	} else if h.Hovered() {
		target = .4
	}
	op := h.AnimateTo("line", target, ui.Animation{Duration: 140 * time.Millisecond})
	if op > 0.01 {
		h.Children(func() {
			ui.Box(c).Width(1.5).FillHeight().Background(p.Live.Alpha(op))
		})
	}
}

// helloShown is an empty session, whose composer sits in the middle.
func (a *App) helloShown() bool {
	return a.C.CanAct() && len(a.C.Entries) == 0 && len(a.C.Approvals) == 0
}

// main is the sheet the session is on. It fills the window; a hairline
// parts it from the sidebar, and another from the review panel beside it.
func (a *App) main(c *ui.Context) {
	p := a.p
	if a.rv.open && a.rv.maximized {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Clip().Children(func() {
			a.reviewPanel(c)
		})
		return
	}
	sheet := ui.Column(c).Grow(1).MinWidth(0).Background(p.Sheet).Clip()
	if a.sideOpen > 0.001 && !c.Vibrancy() {
		sheet.BorderWidth(0, 0, 0, .5).BorderColor(p.Edge)
	}
	sheet.Children(func() {
		a.bar(c)
		a.transcript(c)
		if a.C.Current != nil && a.C.CanAct() {
			a.composer(c)
		}
		a.findBar(c)
	})
	if a.rvOpen > 0.001 {
		rw := a.reviewWidth()
		if a.rvOverlay {
			w, _ := c.Size()
			if w > 0 {
				rw = min(rw, w*0.85)
			}
			rvWrap := ui.Row(c).Absolute().Top(0).Bottom(0).Right(0).Width(rw).ZIndex(20).AlignItems(ui.Stretch).Clip()
			lift(rvWrap, p)
			if a.rvOpen < 0.999 {
				rvWrap.Translate((1-a.rvOpen)*20, 0).Opacity(min(1, a.rvOpen*1.35))
			}
			rvWrap.Children(func() {
				a.reviewPanel(c)
			})
		} else {
			rvWrap := ui.Row(c).Width(a.rvOpen * rw).Shrink(0).AlignItems(ui.Stretch).Clip()
			if a.rvOpen < 0.999 {
				rvWrap.Translate((1-a.rvOpen)*10, 0).Opacity(min(1, a.rvOpen*1.35))
			}
			rvWrap.Children(func() {
				a.reviewPanel(c)
				if a.rvOpen > 0.99 {
					a.splitHandle(c, "split-review", 0, func(dx float32) { a.resizeReview(dx) }, a.resetReviewWidth)
				}
			})
		}
	}
}

// textZoom is how much the window's content is zoomed for a text size.
func textZoom(size string) float32 {
	switch size {
	case "small":
		return .9
	case "large":
		return 1.15
	}
	return 1
}

// minMain is the narrowest the session's sheet gets beside the panel
// before the sidebar makes room.
const minMain = 600

// fitPanel puts the sidebar away when the review panel opens in a window
// too narrow for all three, overlays the panel when even without the sidebar
// the main sheet would be narrower than minMain, and restores the sidebar when
// the window widens or the panel closes.
func (a *App) fitPanel(c *ui.Context) {
	w, _ := c.Size()
	if a.rv.open == a.panelWas && w == a.lastW {
		return
	}
	a.panelWas = a.rv.open
	a.lastW = w
	if !a.rv.open {
		a.rvOverlay = false
		if a.sideMoved {
			a.noSide, a.sideMoved = false, false
		}
		return
	}
	needAll := a.sideWidth() + a.reviewWidth() + minMain
	if !a.noSide && w < needAll {
		a.noSide, a.sideMoved = true, true
	} else if a.sideMoved && w >= needAll {
		a.noSide, a.sideMoved = false, false
	}
	a.rvOverlay = w < a.reviewWidth()+minMain
}

// transcriptList returns the scroll state for the active session.
func (a *App) transcriptList() *ui.ListState {
	id := ""
	if a.C.Current != nil {
		id = a.C.Current.ID
	}
	if a.lists == nil {
		a.lists = map[string]*ui.ListState{}
	}
	st := a.lists[id]
	if st == nil {
		st = &ui.ListState{}
		a.lists[id] = st
	}
	return st
}

// SetReconnect registers a callback to reconnect when the kernel disconnects.
func (a *App) SetReconnect(fn func()) {
	a.reconnect = fn
}

// SetCloseWindow registers the host window close callback used by ⌘W
// once no modal sheet or overlay is open.
func (a *App) SetCloseWindow(fn func()) {
	a.closeWindow = fn
}

// closeOrDismiss handles ⌘W in layers: close active overlay/sheet first
// (Command Palette, Settings, Automations, Find bar), then close the main window.
func (a *App) closeOrDismiss() {
	switch {
	case a.pal:
		a.pal, a.query = false, ""
	case a.settingsOpen:
		a.settingsOpen = false
	case a.automationsOpen:
		if a.automationsUI.deletingID != "" {
			a.automationsUI.deletingID, a.automationsUI.deletingName = "", ""
		} else {
			a.automationsOpen = false
		}
	case a.find.on:
		a.closeFind()
	case a.closeWindow != nil:
		a.closeWindow()
	}
}

// toggleSide shows or hides the sidebar as the user asks, which the panel
// then leaves alone.
func (a *App) toggleSide() {
	a.noSide, a.sideMoved = !a.noSide, false
}
