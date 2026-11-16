package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
)

// MenuBar is the app's own commands for the menu bar, each with the
// shortcut the window already answers to: a Session menu, and Settings
// and the sidebar for the app and View menus.
func (a *App) MenuBar() (session []*mygo.MenuItem, settings, sidebar *mygo.MenuItem) {
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
	}
	settings = item(locales.Settings()+"…", "CmdOrCtrl+,", func(*ui.Context) {
		if !a.settingsOpen {
			a.openSettings()
		}
	})
	sidebar = item(locales.ToggleSidebarMenu(), "CmdOrCtrl+\\", func(*ui.Context) { a.toggleSide() })
	return
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
