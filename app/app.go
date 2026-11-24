// Package app is Stratum's window: a sidebar of sessions, the transcript of
// the one shown and the composer under it, drawn with mygo's native UI
// over a client.Client.
package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/kernel/rpc"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// App is the state of the window. Everything in it is touched on the UI
// thread only: frames from the kernel come in through update.
type App struct {
	C *client.Client

	out        chan []proto.Command
	desk       Desktop
	marks      map[string]string // the sessions' states desk last saw
	badge      int
	awake      bool
	p          *Palette
	now        func() time.Time
	err        string // the last command that could not be sent
	draft      string // what the composer holds
	recall     string // a sent message ↑ brings back, for the next frame
	toEnd      bool   // put the composer's caret at the end of its draft
	mention    mention
	slash      slash
	rewind     rewind
	escAt      time.Time   // the last Esc in an empty composer, for a second one
	slashRun   func(i int) // runs the / menu's item i, as last shown
	slashLen   int
	filesKey   string // the folder and @ query the files were asked for
	slashAsked string // the folder the / menu last read skills for
	find       find
	btwOpen    bool
	btwFocus   bool
	btwDraft   string
	btwInput   ui.Handle
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
	announcedSession  string
	announcedApproval string
	announcedBusy     bool
	cardCache         *lruCache[string, parsedCard]
	keptUnread        string // marked unread while shown
	showArchived      bool
	drafts            drafts                   // keeps each session's draft in the kernel
	query             string                   // the sidebar's search
	lists             map[string]*ui.ListState // per-session transcript scroll state
	md                markdown.Options
	opened            map[string]bool // tool calls whose output shows
	inWork            bool            // building a finished turn's folded work
	noSide            bool            // the sidebar is hidden
	// panelWas is whether the review panel was open last frame; sideMoved
	// that opening it in a narrow window put the sidebar away, so that
	// closing it brings the sidebar back.
	panelWas, sideMoved bool
	rvOverlay           bool                  // review panel floats over main when window is very narrow
	lastW               float32               // last window width seen by fitPanel
	sideOpen            float32               // how far the sidebar is out, 0 to 1, on its way
	rvOpen              float32               // how far the review inspector is out, 0 to 1
	sideW               float32               // user-adjusted sidebar width, 0 means default
	rvW                 float32               // user-adjusted review width, 0 means default
	split               splitDrag             // the seam being dragged
	dockH               float32               // how tall the composer floating on the transcript was
	listShown           *ui.ListState         // the transcript's list in the last frame
	hero                hero                  // where the composer of an empty session stands
	apDiffs             map[string][]diffLine // the approvals' diffs, by ID
	sys                 ui.Preferences        // the desktop's accessibility preferences, read each frame
	focusSession        string                // the session the composer last took the focus for
	overWas             bool                  // the palette, settings or automations showed last frame
	removing            []string              // the sessions of the project the user asked to remove, waiting on Remove
	removingName        string
	pal                 bool   // the ⌘K palette shows
	jump                int64  // a message to scroll to, from a search
	jumpIn              string // the session it is in
	palSel              int    // its row chosen by the arrow keys
	palScroll           bool   // scroll palette selection into view once after ↑/↓
	stats               map[string][2]int
	rv                  review
	pierre              pierreState
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

	searchInput                                                                                       ui.Handle
	composerInput                                                                                     ui.Handle
	closeWindow                                                                                       func()
	reconnect                                                                                         func()
	keyRepeat                                                                                         bool
	visibleSessions                                                                                   []string
	menuSession                                                                                       string
	modelMenu, modeMenu, agentMenu, detailMenu, folderMenu, worktreeMenu, branchMenu, accountMenuOpen bool
	modelQuery                                                                                        string
	modelSearch                                                                                       ui.Handle
	modelsRequested                                                                                   bool
	modelsPending, modelsAccount, modelsError                                                         string
	modelsLoadedAccount                                                                               string

	keyOpen, keyMenu bool
	keyProvider, key string

	settingsOpen, addingAccount, setModelMenu bool
	settingsPage                              string // a settingsPages id
	settingsQuery, keysQuery                  string
	subMenu, lightMenu                        bool
	accountsSeen                              int
	mcpUI                                     mcpState
	previewUI                                 previewState
	term                                      termState
	webTabs                                   []*browser // the panel's browser's tabs, the first made when first used
	webActive, webSeq                         int        // the tab in front; the last tab's id
	webAgent                                  time.Time  // the agent's last action in the browser
	webFocus                                  bool       // the address field takes the focus next frame
	webClearing                               bool       // the dialog that clears the browser's data shows
	clearKinds                                WebData    // what that dialog clears
	webHistory                                []webVisit // the pages the browser showed, oldest first
	historyFile                               string     // where the history is kept between runs
	historyDirty                              bool       // the history changed since it was kept
	historyQuery                              string     // Settings' search of the history
	webSuggest                                int        // the address field's suggestion picked with ↓, or -1
	webSuggestOff                             string     // the address text the suggestions were put away at
	webPick                                   string     // the suggestion Return chose, opened next frame
	webDownloads                              []*webDownload
	nextDownload                              int
	downloadsOpen                             bool // the toolbar's Downloads shows its list
	dlPrefs                                   downloadPrefs
	askSavePath                               func(name, dir string) (string, error)
	webRestored                               bool // the tabs the settings keep were opened
	clearWebData                              func(WebData) error
	webErr, webAddr                           string
	webInput                                  ui.Handle
	newBrowser                                func() (*browser, error)
	// newTerm starts a shell for the Terminal tab, shellJob tells what runs
	// in one (foregroundJob); tests put fakes in their place.
	newTerm         func(terminal.Options) (*terminal.Terminal, error)
	shellJob        func(live []string, id string) (name string, busy bool)
	automationsOpen bool
	automationsUI   automationsState
	locale          Locale
	theme           string // the appearance last applied

	attached        []attachment
	mentionSessions []string
	thumbs          *lruCache[string, ui.ImageSource] // of sent pictures, by entry key and index
	pick            func() []string                   // the open panel
	pickDir         func() string                     // the open panel, for a folder
	update          func(func())                      // runs on the UI thread, as Serve was given
	haptic          func(hapticPattern)               // trackpad haptic feedback hook
	menuRuns        []func(c *ui.Context)             // commands from the menu bar, for the next frame
}

// New returns an app over c, not connected yet.
func New(c *client.Client) *App {
	a := &App{C: c, now: time.Now, webSuggest: -1, openURL: openBrowser, pick: pickFiles, pickDir: pickFolder, haptic: performHaptic, opened: map[string]bool{}, locale: &memLocale{},
		rv: review{asked: map[string]bool{}, diffs: newLRU[string, *fileDiff](64)}}
	a.openIn = a.openInBackground
	a.newTerm, a.shellJob = startShell, foregroundJob
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
			client.PreDecodeFrame(&f)
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
	a.sys = c.Preferences()
	if a.reduceMotion() {
		c.DisableAnimations()
	}
	isDark := c.Theme().Dark
	darkTarget := float32(0)
	if isDark {
		darkTarget = 1
	}
	themeMix := c.Root().Spring("theme", darkTarget, springDefault)
	a.p = PaletteLerp(themeMix)
	c.SetTheme(Theme(a.p, a.p.Dark))
	if z := textZoom(a.settings().TextSize); c.Zoom() != z {
		c.SetZoom(z)
	}
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	if a.needsLogin() {
		a.onboarding(c)
		a.clearWebDialog(c)
		a.keys(c)
		return
	}
	a.runMenu(c)
	a.syncDraft(c)
	a.markSeen(c)
	a.syncAnnounce(c)
	a.sweepTerminals()
	if a.pierre.focusComposerNext {
		a.pierre.focusComposerNext = false
		a.composerInput.Focus(c)
	}
	if a.settingsOpen {
		ui.Box(c.Key("view-settings")).Fill().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .985, Opacity: 0}, Duration: 180 * time.Millisecond}).
			Children(func() { a.settingsView(c, vibrant) })
		a.clearWebDialog(c)
		a.keys(c)
		return
	}
	if a.automationsOpen {
		ui.Box(c.Key("view-automations")).Fill().
			Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .985, Opacity: 0}, Duration: 180 * time.Millisecond}).
			Children(func() { a.automationsView(c, vibrant) })
		a.clearWebDialog(c)
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
	// not squeezed, so its rows hold still while it slides. It does not
	// fade, as Finder's does not: faded, its wash over the window's glass
	// let the dark glass through, a flash of gray at each toggle.
	target := float32(1)
	if a.noSide {
		target = 0
	}
	a.sideOpen = shell.Spring("side", target, springDefault)
	sw := a.sideWidth()
	shell.Children(func() {
		if a.sideOpen > 0.001 {
			sideW := float32(math.Round(float64(a.sideOpen * sw)))
			ui.Row(c).Width(sideW).Shrink(0).AlignItems(ui.Stretch).Clip().
				Children(func() { a.sidebar(c, vibrant) })
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
	a.clearWebDialog(c)
	a.saveWebTabs()
	a.syncDownloadPrefs()
	a.keys(c)
	a.restoreFocus(c)
}

// reduceMotion is whether to keep still what would move: as the user set
// in Stratum, or in macOS (Accessibility › Display › Reduce motion).
func (a *App) reduceMotion() bool {
	return a.settings().ReduceMotion || a.sys.ReduceMotion
}

const (
	sideMinWidth   = 220
	sideMaxWidth   = 360
	reviewMinWidth = 360
	reviewMaxWidth = 680
)

// The sidebar and the review panel keep the widths the user drags them
// to, across launches, as Finder's and Mail's do.
func (a *App) sideWidth() float32 {
	w := a.sideW
	if w <= 0 {
		w = a.settings().SidebarWidth
	}
	if w <= 0 {
		return sidebarWidth
	}
	return max(sideMinWidth, min(sideMaxWidth, w))
}

func (a *App) reviewWidth() float32 {
	w := a.rvW
	if w <= 0 {
		w = a.settings().InspectorWidth
	}
	if w <= 0 {
		return reviewWidth
	}
	return max(reviewMinWidth, min(reviewMaxWidth, w))
}

// splitDrag is a seam under the pointer: the width it had when pressed,
// and the width the pointer asks for, which runs past the limits so that
// the seam meets the pointer again where it left it.
type splitDrag struct {
	key        string
	start, raw float32
}

func (a *App) dragSplit(key string, width float32, dx float32) *splitDrag {
	d := &a.split
	if d.key != key {
		*d = splitDrag{key: key, start: width, raw: width}
	}
	d.raw += dx
	return d
}

func (a *App) fireHaptic(p hapticPattern) {
	if a.haptic != nil {
		a.haptic(p)
	}
}

func (a *App) resizeSide(dx float32) {
	d := a.dragSplit("split-side", a.sideWidth(), dx)
	// Dragged well past its narrowest, the sidebar folds away at the width
	// it had, as Finder's and Mail's do; ⌘\ brings it back.
	if d.raw < sideMinWidth/2 {
		a.sideW = d.start
		a.noSide, a.sideMoved = true, false
		a.split = splitDrag{}
		a.fireHaptic(hapticAlignment)
		return
	}
	a.sideW = max(sideMinWidth, min(sideMaxWidth, d.raw))
}

func (a *App) resetSideWidth() {
	a.sideW = sidebarWidth
	a.saveWidths()
}

func (a *App) resizeReview(dx float32) {
	d := a.dragSplit("split-review", a.reviewWidth(), -dx)
	// Past half its narrowest, the panel closes, as Xcode's inspectors do.
	if d.raw < reviewMinWidth/2 {
		a.rvW = d.start
		a.rv.open = false
		a.split = splitDrag{}
		a.fireHaptic(hapticAlignment)
		return
	}
	a.rvW = max(reviewMinWidth, min(reviewMaxWidth, d.raw))
}

func (a *App) resetReviewWidth() {
	a.rvW = reviewWidth
	a.saveWidths()
}

// saveWidths keeps the seams' widths in the settings, once a drag ends.
func (a *App) saveWidths() {
	if a.C.Settings == nil {
		return
	}
	s := *a.C.Settings
	side, rv := a.sideWidth(), a.reviewWidth()
	if side == sidebarWidth {
		side = 0
	}
	if rv == reviewWidth {
		rv = 0
	}
	if s.SidebarWidth == side && s.InspectorWidth == rv {
		return
	}
	s.SidebarWidth, s.InspectorWidth = side, rv
	a.do(a.C.SaveSettings(s))
}

// splitHandle draws an 8pt ergonomic resize target centered on a pane seam.
// Hovering reveals a subtle accent hairline; double-clicking resets to default.
func (a *App) splitHandle(c *ui.Context, key string, x float32, onDrag func(dx float32), onReset func()) {
	p := a.p
	h := ui.Box(c.Key(key)).Absolute().Top(0).Bottom(0).Left(x - 4).Width(8).Cursor(ui.CursorResizeColumn).Center().ZIndex(12)
	if dx, _, ok := h.Dragged(); ok && dx != 0 {
		onDrag(dx)
	} else if a.split.key == key && !h.Pressed() {
		a.split = splitDrag{}
		a.saveWidths()
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
	// Keyed: the sidebar before it comes and goes at the ends of its
	// slide, and the sheet, by its place, would be built anew, the
	// composer and the transcript's scroll with it, as a jump.
	sheet := ui.Column(c.Key("sheet")).Grow(1).MinWidth(0).Background(p.Sheet).Clip()
	a.hero.sheet = sheet.LayoutBounds()
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
		a.sideAskCard(c)
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
func (a *App) closeOrDismiss(c *ui.Context) {
	switch {
	case a.termFocused(c):
		// ⌘W in a terminal closes its tab, as in Terminal.app.
		a.closeActiveTerminal()
	case a.browserFocused(c):
		if b := a.activeWeb(); b != nil {
			a.closeWebTab(b.id)
		} else {
			a.rv.open = false
		}
	case a.pal:
		a.pal, a.query = false, ""
	case a.btwOpen:
		a.btwOpen = false
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

// syncAnnounce informs VoiceOver when a new approval arrives or a turn ends.
func (a *App) syncAnnounce(c *ui.Context) {
	sid := a.sessionID()
	if sid != a.announcedSession {
		a.announcedSession = sid
		a.announcedBusy = a.C.Busy
		a.announcedApproval = ""
		if len(a.C.Approvals) > 0 {
			a.announcedApproval = a.C.Approvals[0].ID
		}
		return
	}
	if len(a.C.Approvals) > 0 {
		id := a.C.Approvals[0].ID
		if id != a.announcedApproval {
			a.announcedApproval = id
			c.Announce(locales.NotifyWait())
		}
	} else {
		a.announcedApproval = ""
	}
	if a.announcedBusy && !a.C.Busy && len(a.C.Approvals) == 0 {
		failed := a.C.Current != nil && a.C.Current.Mark == "fail"
		if !failed {
			if n := len(a.C.Entries); n > 0 {
				last := a.C.Entries[n-1]
				failed = last.Role == client.Notice && last.Note == "fail"
			}
		}
		if failed {
			c.Announce(locales.NotifyFail())
		} else {
			c.Announce(locales.NotifyDone())
		}
	}
	a.announcedBusy = a.C.Busy
}
