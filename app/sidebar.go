package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

const (
	barHeight    = 48
	sheetBar     = 48 // matches barHeight so all panes share one M3 Top App Bar baseline
	sidebarWidth = 276
)

// iconButton is a 32-point M3 tonal icon button holding a symbol.
func (a *App) iconButton(c *ui.Context, key, symbol, tip string) ui.Element {
	return a.iconButtonSized(c, key, symbol, tip, 18)
}

func (a *App) iconButtonSized(c *ui.Context, key, symbol, tip string, size float32) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Size(32, 32).Radius(10).Center().Label(tip).Tooltip(tip)
	m3Focus(b, p)
	m3State(b, p, ui.Transparent, false)
	b.Children(func() { sym(c, symbol, size, p.OnSurfaceVariant) })
	return b
}

func (a *App) sidebar(c *ui.Context) {
	p := a.p
	side := ui.Column(c).Width(a.sideWidth()).Shrink(0).Radius(18).
		Background(p.SurfaceContainerLow).Clip()
	side.Children(func() {
		// M3 Pane Header with FlClash-style in-bar expanding filter search.
		ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 10, 0, 16).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.sideSearchOpen {
				ui.Row(c).Grow(1).MinWidth(0).Height(32).Radius(16).Background(p.SurfaceContainerHigh).
					Padding(0, 6, 0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
					sym(c, "search", 14, p.OnSurfaceVariant)
					ui.TextInputBase(c.Key("side-search-in"), &a.sideQuery).Bind(&a.sideSearchInput).
						Label(locales.FilterSessions()).Placeholder(locales.FilterSessions()).PlaceholderColor(p.OnSurfaceVariant).
						FontSize(13).TextColor(p.OnSurface).Grow(1).MinWidth(0).SingleLine().AutoFocus().
						OnTextKey(func(k ui.TextKey) bool {
							a.sideQuery = k.Text()
							if k.Key == ui.KeyEscape {
								a.sideSearchOpen, a.sideQuery = false, ""
								return true
							}
							return false
						})
					if clr := a.iconButtonSized(c, "side-search-close", "x", locales.Close(), 13); clr.Clicked() {
						a.sideSearchOpen, a.sideQuery = false, ""
					}
				})
			} else {
				ui.Text(c, locales.Sessions()).FontSize(16).FontWeight(600).TextColor(p.OnSurface).SingleLine()
				ui.Spacer(c)
				if sb := a.iconButtonSized(c, "side-search-btn", "search", locales.Search(), 17); sb.Clicked() {
					a.sideSearchOpen = true
					a.sideSearchInput.Focus(c)
				}
			}
			if col := a.iconButtonSized(c, "side", "layout-sidebar-left-collapse", locales.HideSidebar(), 18); col.Clicked() {
				a.toggleSide()
			}
		})

		// Single-row borderless tonal filter track (fits English & Chinese without ellipsis)
		ui.Row(c).Shrink(0).Margin(0, 10, 8, 10).Padding(3).Height(34).Radius(17).
			Background(p.SurfaceContainerHigh).AlignItems(ui.Center).Gap(2).Children(func() {
			for _, f := range []struct{ id, label string }{
				{"", locales.FilterAll()},
				{"run", locales.FilterRunning()},
				{"wait", locales.FilterPending()},
				{"archived", locales.FilterArchived()},
			} {
				active := a.sideFilter == f.id
				tab := ui.ButtonBase(c.Key("sf:"+f.id)).Row().Grow(1).Shrink(0).Height(28).Padding(0, 6).Radius(14).
					Center().Label(f.label).Checked(active)
				m3Focus(tab, p)
				col, weight := p.OnSurfaceVariant, 500
				if active {
					selBg := p.Surface
					if p.Dark {
						selBg = p.SurfaceContainerHighest
					}
					tab.Background(selBg)
					col, weight = p.OnSurface, 600
				} else {
					m3State(tab, p, ui.Transparent, false)
				}
				if tab.Clicked() {
					a.sideFilter = f.id
					a.showArchived = f.id == "archived"
				}
				tab.Children(func() {
					ui.Text(c, f.label).FontSize(12).FontWeight(weight).TextColor(col).SingleLine()
				})
			}
		})

		ui.Scroll(c).Grow(1).Padding(2, 8, 12, 8).Children(func() { a.sessions(c) })
		a.archivedToast(c)
		a.footer(c)
	})
	a.deleteDialog(c)
}

// sessionGroups are the sessions as the sidebar groups them.
type sessionGroups struct {
	pinned, archived []proto.Session
	folders          []string
	byFolder         map[string][]proto.Session
	cwdByFolder      map[string]string
}

func (a *App) groupSessions() *sessionGroups {
	g := &a.sideGroups
	g.pinned = g.pinned[:0]
	g.archived = g.archived[:0]
	g.folders = g.folders[:0]
	if g.byFolder == nil {
		g.byFolder = map[string][]proto.Session{}
		g.cwdByFolder = map[string]string{}
	} else {
		for k, s := range g.byFolder {
			g.byFolder[k] = s[:0]
		}
		clear(g.cwdByFolder)
	}
	q := strings.ToLower(strings.TrimSpace(a.sideQuery))
	for _, s := range a.C.Sessions {
		if q != "" {
			t := strings.ToLower(title(s) + " " + filepath.Base(s.Cwd))
			if !strings.Contains(t, q) {
				continue
			}
		}
		st := a.state(s)
		switch a.sideFilter {
		case "run":
			if s.Archived || st != "run" {
				continue
			}
		case "wait":
			if s.Archived || (st != "wait" && st != "unread" && st != "fail") {
				continue
			}
		case "archived":
			if !s.Archived {
				continue
			}
		}
		switch {
		case s.Archived:
			g.archived = append(g.archived, s)
			continue
		case s.Pinned:
			g.pinned = append(g.pinned, s)
			continue
		}
		f := filepath.Base(s.Cwd)
		if _, ok := g.cwdByFolder[f]; !ok {
			g.folders = append(g.folders, f)
			g.cwdByFolder[f] = s.Cwd
		}
		g.byFolder[f] = append(g.byFolder[f], s)
	}
	return g
}

func (a *App) projectCollapsed(cwd string) bool {
	return cwd != "" && slices.Contains(a.settings().CollapsedProjects, cwd)
}

func (a *App) toggleProjectCollapsed(cwd string) {
	if cwd == "" {
		return
	}
	s := a.settings()
	list := slices.Clone(s.CollapsedProjects)
	if idx := slices.Index(list, cwd); idx >= 0 {
		list = append(list[:idx], list[idx+1:]...)
	} else {
		list = append(list, cwd)
	}
	s.CollapsedProjects = list
	a.do(a.C.SaveSettings(s))
}

// sessionOrder is the sessions in the order the sidebar shows them, for ⌘1–⌘9 and ⌘[ ⌘].
func (a *App) sessionOrder() []string {
	g := a.groupSessions()
	ids := a.visibleSessions[:0]
	add := func(ss []proto.Session) {
		for _, s := range ss {
			ids = append(ids, s.ID)
		}
	}
	add(g.pinned)
	for _, f := range g.folders {
		if !a.projectCollapsed(g.cwdByFolder[f]) {
			add(g.byFolder[f])
		}
	}
	if a.showArchived || a.sideFilter == "archived" {
		add(g.archived)
	}
	return ids
}

// sessions lists the sessions: pinned first, then grouped by workspace, then archived.
func (a *App) sessions(c *ui.Context) {
	a.visibleSessions = a.visibleSessions[:0]
	if len(a.C.Sessions) == 0 {
		emptyState(c, a.p, "message-plus", locales.EmptySessionsTitle(), locales.EmptySessionsHint(), nil)
		return
	}
	g := a.groupSessions()
	pinned, archived, folders, byFolder, cwdByFolder := g.pinned, g.archived, g.folders, g.byFolder, g.cwdByFolder
	if len(pinned) == 0 && len(folders) == 0 && len(archived) == 0 {
		emptyState(c, a.p, "search", locales.PaletteNoMatches(), "", nil)
		return
	}
	if len(pinned) > 0 {
		a.group(c, "pin", locales.Pinned(), len(pinned))
		for _, s := range pinned {
			a.sessionRow(c, s)
		}
	}
	for _, f := range folders {
		cwd := cwdByFolder[f]
		a.projectGroup(c, f, cwd, byFolder[f])
		if !a.projectCollapsed(cwd) {
			for _, s := range byFolder[f] {
				a.sessionRow(c, s)
			}
		}
	}
	if len(archived) > 0 && (a.sideFilter == "" || a.sideFilter == "archived") {
		p := a.p
		open := a.showArchived || a.sideFilter == "archived"
		b := ui.ButtonBase(c.Key("archived")).Row().Height(30).Margin(12, 0, 4, 0).Padding(0, 10).Gap(8).Radius(10).AlignItems(ui.Center).
			Label(locales.Archived()).Expanded(open)
		m3Focus(b, p)
		m3State(b, p, ui.Transparent, false)
		if b.Clicked() {
			a.showArchived = !a.showArchived
		}
		b.Children(func() {
			sym(c, "archive", 14, p.OnSurfaceVariant)
			ui.Text(c, locales.Archived()).FontSize(12.5).FontWeight(600).TextColor(p.OnSurfaceVariant).SingleLine()
			disclosureChevron(c, "archived", open, 12, p.OnSurfaceVariant)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(len(archived))).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).FontFeatures("tnum")
		})
		if open {
			for _, s := range archived {
				a.sessionRow(c, s)
			}
		}
	}
}

func (a *App) group(c *ui.Context, symbol, label string, n int) {
	p := a.p
	ui.Row(c).Height(26).Margin(10, 0, 4, 0).Padding(0, 10).Gap(7).AlignItems(ui.Center).Children(func() {
		if symbol != "" {
			sym(c, symbol, 13, p.OnSurfaceVariant)
		}
		ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(p.OnSurfaceVariant).SingleLine()
		if n > 0 {
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(n)).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).FontFeatures("tnum")
		}
	})
}

func (a *App) projectGroup(c *ui.Context, label, cwd string, sessions []proto.Session) {
	p := a.p
	collapsed := a.projectCollapsed(cwd)
	row := ui.Row(c.Key("proj:"+cwd)).Height(32).Margin(10, 0, 2, 0).Padding(0, 4, 0, 8).Gap(6).Radius(10).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	fillMenu := func(m *ui.Menu) {
		if m.Item(locales.NewSession()).Chosen() {
			a.do(a.C.NewSessionIn(cwd))
		}
		if cwd != "" && m.Item(locales.RevealInFinder()).Chosen() {
			_ = exec.Command("open", "-R", cwd).Start()
		}
		if cwd != "" && a.C.Current != nil && m.Item(locales.OpenInTerminal()).Chosen() {
			a.openTerminalIn(cwd)
		}
		m.Separator()
		if m.Item(locales.RemoveProject() + "…").Danger().Chosen() {
			a.removing, a.removingName = a.removing[:0], label
			for _, s := range sessions {
				a.removing = append(a.removing, s.ID)
			}
		}
	}
	row.ContextMenu(fillMenu)
	row.Children(func() {
		toggle := ui.ButtonBase(c.Key("proj-toggle:" + cwd)).Row().Grow(1).MinWidth(0).Height(32).Gap(7).AlignItems(ui.Center).
			KeepFocus().Label(label).Expanded(!collapsed)
		m3Focus(toggle, p)
		toggle.Children(func() {
			disclosureChevron(c, "proj:"+cwd, !collapsed, 12, p.OnSurfaceVariant)
			sym(c, "folder", 15, p.OnSurfaceVariant)
			ui.Text(c, label).FontSize(13.5).FontWeight(600).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
		if toggle.Clicked() {
			a.toggleProjectCollapsed(cwd)
		}
		menuOpen := a.menu.projectMenu == cwd
		// Show row actions on hover, open menu, or keyboard focus within the row (modern-web-guidance: natural-interactions).
		if row.Hovered() || row.FocusWithin() || menuOpen {
			more := ui.ButtonBase(c.Key("proj-more:"+cwd)).Size(26, 26).Radius(8).Center().KeepFocus().Label(label)
			m3Focus(more, p)
			m3State(more, p, ui.Transparent, menuOpen)
			more.Children(func() { sym(c, "dots", 14, p.OnSurfaceVariant) })
			more.PopoverMenuButton(fillMenu, a.menuOptions(&menuOpen))
			if menuOpen {
				a.menu.projectMenu = cwd
			} else if a.menu.projectMenu == cwd {
				a.menu.projectMenu = ""
			}

			add := ui.ButtonBase(c.Key("proj-new:"+cwd)).Size(26, 26).Radius(8).Center().KeepFocus().Label(locales.NewSession()).Tooltip(locales.NewSession())
			m3Focus(add, p)
			m3State(add, p, ui.Transparent, false)
			add.Children(func() { sym(c, "plus", 14, p.OnSurfaceVariant) })
			if add.Clicked() {
				a.do(a.C.NewSessionIn(cwd))
			}
		}
	})
}

// state returns the session status mark: "wait", "run", "unread", "fail", or "".
func (a *App) state(s proto.Session) string {
	cur := a.C.Current
	if cur == nil || cur.ID != s.ID {
		return a.C.Marks[s.ID]
	}
	switch {
	case len(a.C.Approvals) > 0:
		return "wait"
	case a.C.Busy:
		return "run"
	}
	return ""
}

func (a *App) sessionRow(c *ui.Context, s proto.Session) {
	a.visibleSessions = append(a.visibleSessions, s.ID)
	p := a.p
	on := a.C.Current != nil && a.C.Current.ID == s.ID
	st := a.state(s)
	n := len(a.visibleSessions) // ⌘1..⌘9 number
	row := ui.ButtonBase(c.Key("s:"+s.ID)).Row().Height(36).Padding(0, 10).Gap(9).Radius(12).AlignItems(ui.Center).Label(title(s))
	if row.Hovered() && !s.Updated.IsZero() {
		row.Tooltip(ago(a.now(), s.Updated))
	}
	m3Focus(row, p)
	rowBg := ui.Transparent
	if on {
		rowBg = p.SurfaceContainerHigh
	}
	m3State(row, p, rowBg, false)
	if row.Clicked() {
		a.do(a.C.Open(s.ID))
	}
	row.ContextMenu(func(m *ui.Menu) { a.sessionMenu(m, s) })
	if a.renaming == s.ID && a.renameIn == "side" {
		row.Children(func() {
			slot(c, p, st)
			a.renameInput(c, s).FontSize(14)
		})
		return
	}
	row.Children(func() {
		slot(c, p, st)
		fg, weight := p.OnSurfaceVariant, 450
		if on {
			fg, weight = p.OnSurface, 600
		} else if row.Hovered() {
			fg = p.OnSurface
		}
		ui.Text(c, title(s)).FontSize(14).FontWeight(weight).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if c.Modifiers()&(ui.Cmd|ui.Shift|ui.Alt|ui.Ctrl) == ui.Cmd && n <= 9 {
			ui.Text(c, fmt.Sprintf("⌘%d", n)).FontSize(11.5).FontWeight(600).FontFeatures("tnum").
				TextColor(p.OnSurfaceVariant).Background(p.SurfaceContainerHigh).Padding(2, 6).Radius(6).SingleLine()
		}
	})
}

// slot draws a calm status dot or icon for the session row.
func slot(c *ui.Context, p *Palette, st string) {
	ui.Box(c).Size(16, 16).Shrink(0).Center().Children(func() {
		switch st {
		case "wait":
			ui.Box(c).Size(8, 8).Radius(4).Background(p.Primary).Label(locales.BadgeApproval())
		case "unread":
			ui.Box(c).Size(8, 8).Radius(4).Background(p.Primary)
		case "run":
			spinner(c, p, 14)
		case "fail":
			sym(c, "alert-circle", 14, p.Error)
		}
	})
}

// spinner renders an M3 primary circular progress arc.
func spinner(c *ui.Context, p *Palette, size float32) ui.Element {
	ring := ui.Box(c).Size(size, size).Radius(size/2).Border(1.8, p.PrimaryContainer)
	turn := ring.Loop("spin", 900*time.Millisecond, ui.Linear)
	black := ui.RGBA(0, 0, 0, 1)
	ring.Children(func() {
		ui.Box(c).Absolute().Fill().Radius(size/2).Border(1.8, p.Primary).
			MaskConicGradient(ui.ConicGradient{Angle: turn * 360, Stops: []ui.GradientStop{{Color: black, At: 0}, {Color: black, At: .3}, {Color: ui.Transparent, At: .32}, {Color: ui.Transparent, At: 1}}})
	})
	return ring
}

func (a *App) footer(c *ui.Context) {
	p := a.p
	ui.Row(c).Shrink(0).Height(42).Padding(0, 14, 4, 14).Gap(8).AlignItems(ui.Center).Children(func() {
		brandMark(c, 16, p.Primary)
		ui.Text(c, "Stratum").FontSize(13.5).FontWeight(600).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
	})
}

func accountName(acc proto.Account) string {
	if acc.Name != "" {
		return acc.Name
	}
	return acc.ID
}

func (a *App) account() *proto.Account {
	for i, acc := range a.C.Accounts {
		if a.C.Current != nil && acc.ID == a.C.Current.Account {
			return &a.C.Accounts[i]
		}
	}
	if len(a.C.Accounts) > 0 {
		return &a.C.Accounts[0]
	}
	return nil
}

func title(s proto.Session) string {
	if s.Title != "" {
		return s.Title
	}
	return locales.NewSession()
}

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		out = append(out, []rune(strings.ToUpper(w))[0])
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	sameDay := y1 == y2 && m1 == m2 && d1 == d2
	switch {
	case d < time.Minute:
		return locales.JustNow()
	case d < time.Hour:
		return locales.AgoMinutes(int(d.Minutes()))
	case sameDay:
		return locales.AgoHours(int(d.Hours()))
	case now.AddDate(0, 0, -1).Day() == d2 && d < 48*time.Hour:
		return locales.Yesterday()
	case d < 6*24*time.Hour:
		return i18n.Weekdays(i18n.Locale(), i18n.Abbreviated, true)[t.Weekday()]
	case y1 == y2:
		return i18n.Months(i18n.Locale(), i18n.Abbreviated, true)[m2-1]
	}
	return locales.YearOnly(y2)
}
