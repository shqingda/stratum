package app

import (
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
)

// MenuBar is the app's own commands for the menu bar, each with the
// shortcut the window already answers to: a Session menu, Settings,
// and the sidebar / inspector toggles for the View menu.
func (a *App) MenuBar() (session []*mygo.MenuItem, settings *mygo.MenuItem, view []*mygo.MenuItem) {
	item := func(label, acc string, run func(c *ui.Context)) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: acc, Click: func(*mygo.MenuItem, *mygo.Window) { a.fromMenu(run) }}
	}
	session = []*mygo.MenuItem{
		item(locales.NewSession(), "CmdOrCtrl+N", func(*ui.Context) { a.do(a.C.NewSession()) }),
		item(locales.Search(), "CmdOrCtrl+K", a.openPalette),
		mygo.Separator(),
		item(locales.FindInSession(), "CmdOrCtrl+F", func(c *ui.Context) {
			if a.C.Current != nil && len(a.C.Entries) > 0 {
				a.openFind(c)
			}
		}),
		item(locales.NextWaiting(), "CmdOrCtrl+J", func(*ui.Context) { a.nextWaiting() }),
		item(locales.PreviousSession(), "CmdOrCtrl+[", func(*ui.Context) { a.stepSession(-1) }),
		item(locales.NextSession(), "CmdOrCtrl+]", func(*ui.Context) { a.stepSession(1) }),
		mygo.Separator(),
		item(locales.FocusComposer(), "CmdOrCtrl+L", func(c *ui.Context) { a.composerInput.Focus(c) }),
		item(locales.CopyLastReply(), "CmdOrCtrl+Alt+C", func(c *ui.Context) { a.copyLastReply(c) }),
		// ⌘. stops what runs, as it does in Terminal and Xcode.
		item(locales.Stop(), "CmdOrCtrl+.", func(*ui.Context) { a.stopTurn() }),
		item(locales.Archive(), "CmdOrCtrl+Shift+A", func(*ui.Context) {
			if a.C.Current != nil && !a.settingsOpen && !a.automationsOpen {
				s := *a.C.Current
				a.archive(s, !s.Archived)
			}
		}),
		mygo.Separator(),
		item(locales.Close(), "CmdOrCtrl+W", func(*ui.Context) { a.closeOrDismiss() }),
	}
	settings = item(locales.Settings()+"…", "CmdOrCtrl+,", func(*ui.Context) {
		if !a.settingsOpen {
			a.openSettings()
		}
	})
	view = []*mygo.MenuItem{
		item(locales.ToggleSidebarMenu(), "CmdOrCtrl+\\", func(*ui.Context) { a.toggleSide() }),
		item(locales.ToggleInspector(), "CmdOrCtrl+Alt+I", func(*ui.Context) {
			if !a.settingsOpen && !a.automationsOpen {
				a.rv.open = !a.rv.open
			}
		}),
		mygo.Separator(),
		// The text sizes of Settings, a step at a time, as Safari's and
		// Mail's View menus zoom.
		item(locales.ActualSize(), "CmdOrCtrl+0", func(*ui.Context) { a.zoomText(0) }),
		item(locales.ZoomIn(), "CmdOrCtrl+Plus", func(*ui.Context) { a.zoomText(1) }),
		item(locales.ZoomOut(), "CmdOrCtrl+-", func(*ui.Context) { a.zoomText(-1) }),
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen},
	}
	return
}

// textSizes are the text sizes, smallest first: Settings' three.
var textSizes = []string{"small", "", "large"}

// zoomText steps the text size by step, or sets it back with 0.
func (a *App) zoomText(step int) {
	if a.C.Settings == nil {
		return
	}
	s := *a.C.Settings
	i := 1
	if step != 0 {
		i = max(slices.Index(textSizes, s.TextSize), 0) + step
	}
	i = min(max(i, 0), len(textSizes)-1)
	if textSizes[i] == s.TextSize {
		return
	}
	s.TextSize = textSizes[i]
	a.do(a.C.SaveSettings(s))
}

// stopTurn stops the running turn.
func (a *App) stopTurn() {
	if a.C.Busy {
		a.do(a.C.Cancel())
	}
}

// fromMenu runs a menu command in the next frame, where it has the
// window's context.
func (a *App) fromMenu(run func(c *ui.Context)) {
	if a.update == nil {
		return
	}
	a.update(func() { a.menuRuns = append(a.menuRuns, run) })
}

// runMenu runs the commands the menu bar sent since the last frame.
func (a *App) runMenu(c *ui.Context) {
	runs := a.menuRuns
	a.menuRuns = nil
	for _, run := range runs {
		run(c)
	}
}
