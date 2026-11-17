// Package app is Talos's window: a sidebar of sessions, the transcript of
// the one shown and the composer under it, drawn with mygo's native UI
// over a client.Client.
package app

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/rpc"
	"github.com/shqingda/talos/proto"
)

//go:embed assets/mark.png
var markPNG []byte

// mark is the app icon, drawn from a PNG: mygo's SVG leaves out the
// gradients and clips the icon is made of.
var mark = func() ui.ImageSource {
	src, err := ui.DecodeImage(markPNG)
	if err != nil {
		panic(err)
	}
	return src
}()

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
	keptUnread         string // marked unread while shown
	showArchived       bool
	drafts             drafts       // keeps each session's draft in the kernel
	query              string       // the sidebar's search
	list               ui.ListState // the transcript
	md                 markdown.Options
	opened             map[string]bool // tool calls whose output shows
	inWork             bool            // building a finished turn's folded work
	noSide             bool            // the sidebar is hidden
	// panelWas is whether the review panel was open last frame; sideMoved
	// that opening it in a narrow window put the sidebar away, so that
	// closing it brings the sidebar back.
	panelWas, sideMoved bool
	sideOpen            float32 // how far the sidebar is out, 0 to 1, on its way
	rvOpen              float32 // how far the review inspector is out, 0 to 1
	settingsAnim        float32 // how far the settings sheet is out, 0 to 1
	dockH               float32 // how tall the composer floating on the transcript was
	pal                 bool    // the ⌘K palette shows
	jump                int64   // a message to scroll to, from a search
	jumpIn              string  // the session it is in
	palSel              int     // its row chosen by the arrow keys
	stats               map[string][2]int
	rv                  review
	seen                map[string]time.Time // when approvals first showed
	openIn              func(app, dir string) error
	openURL             func(url string) error
	// version is the app's, as packaged; checkUpdates asks the updater,
	// nil where there is none.
	version      string
	checkUpdates func()

	openMenu    bool
	commitMsg   string
	commitInput ui.Handle

	searchInput                                             ui.Handle
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
	locale                                    Locale
	theme                                     string // the appearance last applied

	attached []attachment
	thumbs   map[string]ui.ImageSource // of sent pictures, by entry key and index
	pick     func() []string           // the open panel
	pickDir  func() string             // the open panel, for a folder
	update   func(func())              // runs on the UI thread, as Serve was given
	menuRuns []func(c *ui.Context)     // commands from the menu bar, for the next frame
}

// New returns an app over c, not connected yet.
func New(c *client.Client) *App {
	return &App{C: c, now: time.Now, openIn: openIn, openURL: openBrowser, pick: pickFiles, pickDir: pickFolder, opened: map[string]bool{}, locale: &memLocale{},
		rv: review{asked: map[string]bool{}, diffs: map[string]*fileDiff{}}}
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
	a.fitPanel(c)
	sTarget := float32(0)
	if a.settingsOpen {
		sTarget = 1
	}
	a.settingsAnim = c.Root().Spring("settings", sTarget, ui.SpringOptions{Stiffness: 420, Damping: 40})
	rvTarget := float32(0)
	if a.rv.open {
		rvTarget = 1
	}
	// Keep the review drawer spring on the root so collapsing the sidebar
	// never shifts child indices and replays the opening animation.
	a.rvOpen = c.Root().Spring("review", rvTarget, ui.SpringOptions{Stiffness: 380, Damping: 39})
	shell := ui.Row(c).Fill().AlignItems(ui.Stretch)
	// One spring moves the sidebar and the title bar beside it, as versta
	// does: it can be turned around halfway, and the sidebar is clipped,
	// not squeezed, so its rows hold still while it slides.
	target := float32(1)
	if a.noSide {
		target = 0
	}
	a.sideOpen = shell.Spring("side", target, ui.SpringOptions{Stiffness: 380, Damping: 39})
	if a.settingsAnim > 0.001 {
		// Push the live workspace subtly back while a sheet sits above it.
		s := 1 - 0.015*a.settingsAnim
		shell.Scale(s, s)
	}
	shell.Children(func() {
		if a.sideOpen > 0.001 {
			ui.Row(c).Width(a.sideOpen * sidebarWidth).Shrink(0).AlignItems(ui.Stretch).Clip().Children(func() { a.sidebar(c, vibrant) })
		}
		a.main(c)
		a.sideToggle(c)
	})
	if !a.settingsOpen {
		a.palette(c)
	}
	if a.settingsOpen {
		a.settingsSheet(c, vibrant)
	}
	a.keys(c)
}

// helloShown is an empty session, whose composer sits in the middle.
func (a *App) helloShown() bool {
	return a.C.CanAct() && len(a.C.Entries) == 0 && len(a.C.Approvals) == 0
}

// main is the sheet the session is on. It fills the window; a hairline
// parts it from the sidebar, and another from the review panel beside it.
func (a *App) main(c *ui.Context) {
	p := a.p
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
		ui.Row(c).Width(a.rvOpen * reviewWidth).Shrink(0).AlignItems(ui.Stretch).Clip().Children(func() { a.reviewPanel(c) })
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
// too narrow for all three, and brings it back when the panel closes.
func (a *App) fitPanel(c *ui.Context) {
	if a.rv.open == a.panelWas {
		return
	}
	a.panelWas = a.rv.open
	w, _ := c.Size()
	switch {
	case a.rv.open && !a.noSide && w < sidebarWidth+reviewWidth+minMain:
		a.noSide, a.sideMoved = true, true
	case !a.rv.open && a.sideMoved:
		a.noSide, a.sideMoved = false, false
	}
}

// toggleSide shows or hides the sidebar as the user asks, which the panel
// then leaves alone.
func (a *App) toggleSide() {
	a.noSide, a.sideMoved = !a.noSide, false
}
