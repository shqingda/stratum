package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
)

// MenuBar is the app's own commands for the menu bar, each with the
// shortcut the window already answers to: a Session menu, Settings,
// and the sidebar / inspector toggles for the View menu.
func (a *App) MenuBar() (session []*mygo.MenuItem, settings, sidebar, inspector *mygo.MenuItem) {
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
	sidebar = item(locales.ToggleSidebarMenu(), "CmdOrCtrl+\\", func(*ui.Context) { a.toggleSide() })
	inspector = item(locales.ToggleInspector(), "CmdOrCtrl+Alt+I", func(*ui.Context) {
		if !a.settingsOpen && !a.automationsOpen {
			a.rv.open = !a.rv.open
		}
	})
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
