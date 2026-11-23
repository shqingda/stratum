package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
)

// The Terminal tab holds the user's own shells, opened in the session's
// folder as Claude's app and Codex open theirs (⌃`), a tab each as in
// Terminal.app (⌘T, ⌘W). A session keeps its shells while the app runs.

// startTerminal starts the shells of new apps; the tests start none.
var startTerminal = terminal.New

type termTab struct {
	id     string
	seq    int // the order shells started in, which their process IDs keep
	t      *terminal.Terminal
	h      ui.Handle
	cwd    string
	exited bool
}

// termSet is a session's shells.
type termSet struct {
	tabs   []*termTab
	active int
	err    string // why the last shell did not start
}

type termFind struct {
	on           bool
	query, asked string
	input        ui.Handle
}

type termState struct {
	sets  map[string]*termSet // by session ID
	seq   int
	focus bool // give the active terminal the keyboard on the next frame
	// closing is the tab waiting on the terminate dialog, and job what
	// runs in it.
	closingSession, closing, job string
	find                         termFind
}

// termSet is the session's shells, made on first asking.
func (a *App) termSet(sid string) *termSet {
	if a.term.sets == nil {
		a.term.sets = map[string]*termSet{}
	}
	s := a.term.sets[sid]
	if s == nil {
		s = &termSet{}
		a.term.sets[sid] = s
	}
	return s
}

// activeTerm is the shell the Terminal tab shows for the session, nil
// before one starts.
func (a *App) activeTerm() *termTab {
	if a.C.Current == nil {
		return nil
	}
	s := a.term.sets[a.C.Current.ID]
	if s == nil || len(s.tabs) == 0 {
		return nil
	}
	return s.tabs[min(s.active, len(s.tabs)-1)]
}

// termShown is whether the panel shows the Terminal tab.
func (a *App) termShown() bool {
	return a.rv.open && a.rv.tab == "term" && !a.settingsOpen && !a.automationsOpen
}

// termFocused is whether the keyboard is in the session's terminal, which
// then gets the keys a shell reads (esc, ⏎, ⇧⇥, ⌥↑, ⌥↓) before the
// window's shortcuts.
func (a *App) termFocused(c *ui.Context) bool {
	t := a.activeTerm()
	return t != nil && a.termShown() && t.h.Focused(c)
}

// termTheme is the panel's own background and text over the palette of
// programs that the terminal plugin takes from Visual Studio Code.
func termTheme(p *Palette) *terminal.Theme {
	base := terminal.LightTheme()
	if p.Dark {
		base = terminal.DarkTheme()
	}
	th := *base
	th.Foreground, th.Background = p.Fg, p.Sheet
	return &th
}

// addTerminal starts a shell in the session's folder and shows it.
func (a *App) addTerminal() {
	cur := a.C.Current
	if cur == nil || a.newTerm == nil {
		return
	}
	set := a.termSet(cur.ID)
	a.term.seq++
	id := fmt.Sprintf("t%d", a.term.seq)
	sid := cur.ID
	t, err := a.newTerm(terminal.Options{
		Dir:              cur.Cwd,
		ShellIntegration: true,
		Theme:            termTheme(PaletteFor(false)),
		DarkTheme:        termTheme(PaletteFor(true)),
		Padding:          terminal.Padding{X: 14, Y: 8},
		OnExit: func(code int) {
			if a.update != nil {
				a.update(func() { a.termExited(sid, id, code) })
			}
		},
	})
	if err != nil {
		if len(set.tabs) > 0 {
			a.flash("alert-circle", locales.TerminalFailed())
			return
		}
		set.err = err.Error()
		return
	}
	set.err = ""
	set.tabs = append(set.tabs, &termTab{id: id, seq: a.term.seq, t: t, cwd: cur.Cwd})
	a.selectTerminal(set, len(set.tabs)-1)
}

// selectTerminal shows the set's i-th shell and gives it the keyboard; a
// find goes on in it.
func (a *App) selectTerminal(set *termSet, i int) {
	if set.active != i && set.active < len(set.tabs) && a.term.find.on {
		set.tabs[set.active].t.EndSearch()
		a.term.find.asked = ""
	}
	set.active = i
	a.term.focus = true
}

// termExited marks a shell that ended; one that ended cleanly closes its
// tab, as Terminal.app does by default.
func (a *App) termExited(sid, id string, code int) {
	set := a.term.sets[sid]
	if set == nil {
		return
	}
	for _, tab := range set.tabs {
		if tab.id == id {
			tab.exited = true
			if code == 0 {
				a.dropTerminal(sid, id)
			}
			return
		}
	}
}

// dropTerminal hangs up a shell and closes its tab; the keyboard goes to
// the tab beside it, or to the composer after the last.
func (a *App) dropTerminal(sid, id string) {
	set := a.term.sets[sid]
	if set == nil {
		return
	}
	i := -1
	for j, tab := range set.tabs {
		if tab.id == id {
			i = j
		}
	}
	if i < 0 {
		return
	}
	tab := set.tabs[i]
	go tab.t.Close()
	if i == set.active && a.term.find.on {
		a.term.find = termFind{}
	}
	set.tabs = append(set.tabs[:i], set.tabs[i+1:]...)
	if i < set.active || set.active >= len(set.tabs) {
		set.active = max(set.active-1, 0)
	}
	if a.term.closing == id {
		a.term.closingSession, a.term.closing, a.term.job = "", "", ""
	}
	current := a.C.Current != nil && a.C.Current.ID == sid
	if len(set.tabs) == 0 {
		a.term.focus = false
		if current {
			a.pierre.focusComposerNext = true
		}
		return
	}
	if current {
		a.term.focus = true
	}
}

// askCloseTerminal closes a tab, first asking when a job runs in its
// shell, as Terminal.app asks before it ends running processes.
func (a *App) askCloseTerminal(sid string, tab *termTab) {
	if tab.exited || a.shellJob == nil {
		a.dropTerminal(sid, tab.id)
		return
	}
	id, job, live := tab.id, a.shellJob, a.liveTerminals()
	answer := func(name string, busy bool) {
		if !busy {
			a.dropTerminal(sid, id)
			return
		}
		a.term.closingSession, a.term.closing, a.term.job = sid, id, name
	}
	if a.update == nil {
		answer(job(live, id))
		return
	}
	// ps takes a moment: the window never waits on it.
	go func() {
		name, busy := job(live, id)
		a.update(func() { answer(name, busy) })
	}()
}

// liveTerminals are the tabs whose shells run, of every session, in the
// order the shells started.
func (a *App) liveTerminals() []string {
	var tabs []*termTab
	for _, set := range a.term.sets {
		for _, tab := range set.tabs {
			if !tab.exited {
				tabs = append(tabs, tab)
			}
		}
	}
	slices.SortFunc(tabs, func(x, y *termTab) int { return x.seq - y.seq })
	ids := make([]string, len(tabs))
	for i, tab := range tabs {
		ids[i] = tab.id
	}
	return ids
}

// closeActiveTerminal is ⌘W in a terminal.
func (a *App) closeActiveTerminal() {
	if tab := a.activeTerm(); tab != nil {
		a.askCloseTerminal(a.C.Current.ID, tab)
	}
}

// toggleTerminal is ⌃`: it shows the session's terminal, starting one if
// none runs, and hides the panel when the terminal shows already.
func (a *App) toggleTerminal() {
	if a.C.Current == nil {
		return
	}
	if a.termShown() {
		a.rv.open = false
		a.pierre.focusComposerNext = true
		return
	}
	a.showTerminal()
}

// showTerminal opens the Terminal tab with the keyboard in it.
func (a *App) showTerminal() {
	a.rv.open, a.rv.tab = true, "term"
	if a.C.Current == nil {
		return
	}
	if set := a.termSet(a.C.Current.ID); len(set.tabs) == 0 {
		set.err = ""
		a.addTerminal()
		return
	}
	a.term.focus = true
}

// sweepTerminals hangs up the shells of sessions that are gone.
func (a *App) sweepTerminals() {
	if len(a.term.sets) == 0 || !a.C.CanAct() {
		return
	}
	for sid, set := range a.term.sets {
		found := false
		for _, s := range a.C.Sessions {
			if s.ID == sid {
				found = true
				break
			}
		}
		if found {
			continue
		}
		for _, tab := range set.tabs {
			go tab.t.Close()
		}
		delete(a.term.sets, sid)
	}
}

// termTitle is what the shell named its window, else its folder.
func termTitle(tab *termTab) string {
	if s := strings.TrimSpace(tab.t.Title()); s != "" {
		return s
	}
	dir := tab.t.Dir()
	if dir == "" {
		dir = tab.cwd
	}
	return filepath.Base(dir)
}

// terminalPane is the Terminal tab: the session's shells, a tab each.
func (a *App) terminalPane(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	if cur == nil {
		a.reviewNote(c, locales.NoOpenSession())
		return
	}
	// A shell starts when the user opens the terminal (showTerminal), not
	// for each session passed through with the tab showing.
	set := a.termSet(cur.ID)
	ui.Row(c).Height(40).Shrink(0).Padding(0, 6, 0, 10).Gap(2).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(2).AlignItems(ui.Center).Children(func() {
			for i, tab := range set.tabs {
				a.termTabButton(c, cur.ID, set, i, tab)
			}
		})
		if a.iconButtonSized(c, "term-new", "plus", locales.NewTerminal(), 15).Clicked() {
			a.addTerminal()
		}
	})
	switch {
	case len(set.tabs) == 0 && set.err != "":
		emptyState(c, p, "terminal-2", locales.TerminalFailed(), set.err, func() {
			if a.button(c, "term-retry", locales.Retry(), "", true).Clicked() {
				set.err = ""
				a.addTerminal()
			}
		})
	case len(set.tabs) == 0:
		emptyState(c, p, "terminal-2", locales.NoTerminal(), "", func() {
			if a.button(c, "term-open", locales.NewTerminal(), "⌘T", true).Clicked() {
				a.addTerminal()
			}
		})
	default:
		tab := set.tabs[min(set.active, len(set.tabs)-1)]
		ui.Box(c.Key("term-body")).Grow(1).MinHeight(0).Children(func() {
			terminal.View(c.Key("term:"+tab.id), tab.t).Fill().Bind(&tab.h)
			a.termFindBar(c, tab)
		})
		if a.term.focus {
			a.term.focus = false
			tab.h.Focus(c)
		}
	}
	a.terminateDialog(c)
}

// termTabButton is a shell's tab: its title, and on hover the close
// button on its left, as Terminal.app and Safari draw theirs.
func (a *App) termTabButton(c *ui.Context, sid string, set *termSet, i int, tab *termTab) {
	p := a.p
	on := i == set.active
	name := termTitle(tab)
	row := ui.Row(c.Key("term-tab:"+tab.id)).Height(26).Padding(0, 2).Gap(0).Radius(7).AlignItems(ui.Center).Shrink(1).MinWidth(72).MaxWidth(200)
	hover := row.Hovered()
	fg, weight := p.Fg2, 500
	switch {
	case on:
		row.Background(p.Chip)
		fg, weight = p.Fg, 600
	case hover:
		row.Background(p.Hover)
		fg = p.Fg
	}
	if tab.exited {
		fg = p.Mute
	}
	row.Children(func() {
		// 22pt to hit, inside the 26pt tab: the mark stays Safari's size.
		x := ui.ButtonBase(c.Key("term-x:"+tab.id)).Size(22, 22).Radius(5).Center().Shrink(0).Label(locales.CloseTerminal()).Tooltip(locales.CloseTerminal())
		press(x)
		if x.Hovered() {
			x.Background(p.Hover)
		}
		if hover || x.Focused() {
			x.Children(func() { sym(c, "x", 10, p.Mute) })
		}
		if x.Clicked() {
			a.askCloseTerminal(sid, tab)
		}
		b := ui.ButtonBase(c.Key("term-sel:"+tab.id)).Row().Grow(1).MinWidth(0).Height(26).Padding(0, 8, 0, 2).AlignItems(ui.Center).Label(name)
		press(b)
		focusRing(b, p)
		b.Children(func() {
			ui.Text(c, name).FontSize(12.5).FontWeight(weight).TextColor(fg).SingleLine().Ellipsis("…").MinWidth(0)
		})
		if b.Clicked() {
			a.selectTerminal(set, i)
		}
	})
}

// openTermFind is ⌘F in a terminal, which finds in its scrollback, as
// Ghostty's find does.
func (a *App) openTermFind(c *ui.Context) {
	a.term.find.on = true
	a.term.find.input.Focus(c)
}

func (a *App) closeTermFind() {
	if tab := a.activeTerm(); tab != nil {
		tab.t.EndSearch()
	}
	a.term.find = termFind{}
	a.term.focus = true
}

// termFindStep moves to the match above (older output, 1) or below (-1).
func (a *App) termFindStep(up int) {
	tab := a.activeTerm()
	if tab == nil || !a.term.find.on {
		return
	}
	if up > 0 {
		tab.t.SearchNext()
	} else {
		tab.t.SearchPrevious()
	}
}

// termFindBar floats at the top right of the terminal, as the find bar of
// the conversation floats over it.
func (a *App) termFindBar(c *ui.Context, tab *termTab) {
	f := &a.term.find
	if !f.on {
		return
	}
	if f.query != f.asked {
		f.asked = f.query
		tab.t.Search(f.query)
	}
	st := tab.t.SearchStatus()
	p := a.p
	bar := ui.Row(c.Key("term-find")).Absolute().Top(8).Right(14).Width(300).Height(38).Padding(0, 4, 0, 12).Gap(6).Radius(10).Background(p.Card).AlignItems(ui.Center).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Scale: .98, Opacity: 0}, Duration: 160 * time.Millisecond})
	rim(bar, p)
	lift(bar, p)
	bar.Children(func() {
		sym(c, "search", 14, p.Mute)
		ui.TextInputBase(c, &f.query).Bind(&f.input).Label(locales.FindInTerminal()).Placeholder(locales.FindInTerminal()).PlaceholderColor(p.Mute).
			FontSize(13.5).TextColor(p.Fg).Grow(1).MinWidth(0).SingleLine().OnTextKey(func(k ui.TextKey) bool {
			f.query = k.Text()
			switch {
			case k.Key == ui.KeyEnter && k.Mods == 0, k.Key == ui.KeyUp && k.Mods == 0:
				a.termFindStep(1)
			case k.Key == ui.KeyEnter && k.Mods == ui.Shift, k.Key == ui.KeyDown && k.Mods == 0:
				a.termFindStep(-1)
			default:
				return false
			}
			return true
		})
		if strings.TrimSpace(f.query) != "" {
			col := p.Mute
			if st.Total == 0 && !st.Searching {
				col = p.Minus
			}
			ui.Text(c, fmt.Sprintf("%d/%d", st.Selected, st.Total)).FontSize(12.5).TextColor(col).SingleLine().Shrink(0).FontFeatures("tnum")
		}
		if a.iconPill(c, "term-find-up", "chevron-up", locales.FindPrevious()).Disabled(st.Total == 0).Clicked() {
			a.termFindStep(1)
		}
		if a.iconPill(c, "term-find-down", "chevron-down", locales.FindNext()).Disabled(st.Total == 0).Clicked() {
			a.termFindStep(-1)
		}
		if a.iconPill(c, "term-find-close", "x", locales.CloseFind()).Clicked() {
			a.closeTermFind()
		}
	})
}

// terminateDialog asks before a tab whose shell runs a job closes.
func (a *App) terminateDialog(c *ui.Context) {
	if a.term.closing == "" {
		return
	}
	open := true
	p := a.p
	body := locales.TerminateBodyAny()
	if a.term.job != "" {
		body = locales.TerminateBody(a.term.job)
	}
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		scrim := ui.RGBA(12, 12, 14, .18)
		if p.Dark {
			scrim = ui.RGBA(0, 0, 0, .42)
		}
		backdrop.Background(scrim)
		panel.Width(380).Padding(18, 18, 14, 18).Radius(14).Background(p.Card).Label(locales.TerminateTitle())
		lift(panel, p).Children(func() {
			ui.Text(c, locales.TerminateTitle()).FontSize(15).FontWeight(600).TextColor(p.Fg)
			ui.Text(c, body).FontSize(13.5).LineHeight(1.5).TextColor(p.Fg2).Margin(8, 0, 16, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, "terminate-cancel", locales.Cancel(), "esc", false).Clicked() {
					open = false
				}
				if a.button(c, "terminate-ok", locales.Terminate(), "", true).Background(p.Minus).Clicked() {
					a.dropTerminal(a.term.closingSession, a.term.closing)
					open = false
				}
			})
		})
	})
	if !open {
		a.term.closingSession, a.term.closing, a.term.job = "", "", ""
		a.term.focus = true
	}
}

// foregroundJob reports what runs in front in the shell of the tab id,
// from ps, as Terminal.app asks before it ends running processes. The
// app's shells are its children that lead a session on a terminal of
// their own; they started in the order of live, the live tabs, and their
// process IDs grow in that order. A job holds the shell's terminal when
// the terminal's foreground process group is not the shell's, as shells'
// background helpers (gitstatusd) do not. What ps cannot tell counts as
// busy.
func foregroundJob(live []string, id string) (name string, busy bool) {
	out, err := exec.Command("ps", "-A", "-ww", "-o", "pid=,ppid=,pgid=,tpgid=,tty=,command=").Output()
	if err != nil {
		return "", true
	}
	type proc struct {
		pid, ppid, pgid, tpgid int
		tty, cmd               string
	}
	var procs []proc
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		f := strings.Fields(string(line))
		if len(f) < 6 {
			continue
		}
		var n [4]int
		ok := true
		for i := range n {
			if n[i], err = strconv.Atoi(f[i]); err != nil {
				ok = false
			}
		}
		if ok {
			procs = append(procs, proc{n[0], n[1], n[2], n[3], f[4], f[5]})
		}
	}
	self, own := os.Getpid(), ""
	for _, p := range procs {
		if p.pid == self {
			own = p.tty
		}
	}
	var shells []proc
	for _, p := range procs {
		if p.ppid == self && p.pid == p.pgid && p.tty != "??" && p.tty != own {
			shells = append(shells, p)
		}
	}
	slices.SortFunc(shells, func(x, y proc) int { return x.pid - y.pid })
	at := slices.Index(live, id)
	if at < 0 || len(shells) != len(live) {
		return "", true
	}
	sh := shells[at]
	if sh.tpgid <= 0 || sh.tpgid == sh.pgid {
		return "", false
	}
	for _, p := range procs {
		if p.pgid == sh.tpgid {
			return strings.TrimPrefix(filepath.Base(p.cmd), "-"), true
		}
	}
	return "", true
}
