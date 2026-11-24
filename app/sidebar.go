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
	barHeight    = 52
	sheetBar     = 52 // matches barHeight so all three panes share one top bar baseline
	sidebarWidth = 264
)

// sym draws an SF Symbol.
// iconButton is a 28-point square button holding a symbol.
func (a *App) iconButton(c *ui.Context, key, symbol, tip string) ui.Element {
	return a.iconButtonSized(c, key, symbol, tip, 17)
}

func (a *App) iconButtonSized(c *ui.Context, key, symbol, tip string, size float32) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip)
	focusRing(b, a.p)
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, size, a.p.Fg2) })
	return b
}

func (a *App) sidebar(c *ui.Context, vibrant bool) {
	p := a.p
	side := ui.Column(c).Width(a.sideWidth()).Shrink(0)
	topGap := float32(barHeight)
	if vibrant {
		// Window VibrancyGlass provides the single native glass layer; add a
		// Tinted frost wash and one right hairline so there are no nested frames.
		side.Background(sideTint(p)).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
	} else {
		side.Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
	}
	side.Children(func() {
		ui.Box(c).Height(topGap + 6).Shrink(0).DragWindow()
		ui.Column(c).Padding(0, 12, 20, 12).Gap(2).Children(func() {
			if a.navItem(c, "search", "search", locales.Search(), "⌘ K").Clicked() {
				a.openPalette(c)
			}
			if a.navItem(c, "skills", "puzzle", locales.SettingsMcp(), "").Clicked() {
				a.openMCP()
			}
			if a.navItem(c, "automations", "clock-play", locales.Automations(), "").Clicked() {
				a.openAutomations()
			}
		})
		list := ui.Scroll(c).Grow(1).Padding(0, 8, 12, 8).Children(func() { a.sessions(c) })
		// The list fades into the footer rather than being cut at it.
		if h := list.Bounds().H; h > 0 {
			black := ui.RGBA(0, 0, 0, 1)
			list.MaskLinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: black, At: 0}, {Color: black, At: 1 - 36/h}, {Color: ui.Transparent, At: 1}}})
		}
		a.archivedToast(c)
		a.footer(c)
	})
	a.deleteDialog(c)
}

// sideToggle is the Safari-style floating Liquid Glass pill holding New Session
// and Sidebar Toggle. It sits at the top-right of the open sidebar and glides
// smoothly beside the window traffic lights as the sidebar collapses.
func (a *App) sideToggle(c *ui.Context) {
	p := a.p
	tip := locales.HideSidebar()
	if a.side.noSide {
		tip = locales.ShowSidebar()
	}
	const pillW, pillH = float32(74), float32(32)
	leftClosed := toggleLeft(c)
	leftOpen := a.sideWidth() - 14 - pillW
	left := leftClosed + a.side.sideOpen*(leftOpen-leftClosed)
	bg := ui.RGBA(255, 255, 255, .94)
	shadow := ui.RGBA(0, 0, 0, .08)
	if p.Dark {
		bg = ui.RGBA(38, 38, 42, .90)
		shadow = ui.RGBA(0, 0, 0, .36)
	}
	pill := ui.Row(c.Key("side-pill")).Absolute().Left(left).Top((barHeight-pillH)/2).
		Size(pillW, pillH).Radius(pillH/2).Padding(0, 5).Gap(4).AlignItems(ui.Center).Justify(ui.Center).
		Background(bg).Border(.5, p.Edge).Shadow(0, 2, 6, -1, shadow).ZIndex(10)
	pill.Children(func() {
		newBtn := ui.ButtonBase(c.Key("bar-new")).Size(30, 26).Radius(13).Center().
			Label(locales.NewSession()).Tooltip(locales.NewSession()).KeyShortcuts(ui.Cmd, ui.KeyN)
		focusRing(newBtn, p)
		newCol := p.Fg
		if newBtn.Hovered() {
			newBtn.Background(p.Hover)
		}
		newBtn.Children(func() { sym(c, "edit", 17, newCol) })
		if newBtn.Clicked() {
			a.do(a.C.NewSession())
		}

		togBtn := ui.ButtonBase(c.Key("side")).Size(30, 26).Radius(13).Center().
			Label(tip).Tooltip(tip)
		focusRing(togBtn, p)
		togCol := p.Fg
		if togBtn.Hovered() {
			togBtn.Background(p.Hover)
		}
		togBtn.Children(func() { sym(c, "layout-sidebar", 18, togCol) })
		if togBtn.Clicked() {
			a.toggleSide()
		}
	})
}

// toggleLeft is where the sidebar pill rests when collapsed: past the traffic lights.
func toggleLeft(c *ui.Context) float32 { return max(c.TitleBar().Left+10, 14) }

func (a *App) navItem(c *ui.Context, key, symbol, label, kbd string) ui.Element {
	p := a.p
	it := ui.ButtonBase(c.Key(key)).Row().Height(32).Padding(0, 9).Gap(9).Radius(7).AlignItems(ui.Center).Label(label)
	focusRing(it, p)
	if it.Hovered() {
		it.Background(p.Hover)
	}
	it.Children(func() {
		ui.Box(c).Width(17).Center().Children(func() { sym(c, symbol, 16, p.Fg2) })
		ui.Text(c, label).FontSize(14).TextColor(p.Fg).SingleLine()
		ui.Spacer(c)
		if kbd != "" {
			ui.Text(c, kbd).FontSize(12).TextColor(p.Mute)
		}
	})
	return it
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
	for _, s := range a.C.Sessions {
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

// sessionOrder is the sessions in the order the sidebar shows them, for
// ⌘1–⌘9 and ⌘[ ⌘] to count by while the sidebar is hidden: the numbers
// stay those the sidebar would show, as Safari's tabs keep theirs.
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
	if a.showArchived {
		add(g.archived)
	}
	return ids
}

// sessions lists the sessions: the ones waiting on the user first, then
// the rest by workspace, each newest first.
func (a *App) sessions(c *ui.Context) {
	a.visibleSessions = a.visibleSessions[:0]
	if len(a.C.Sessions) == 0 {
		emptyState(c, a.p, "message-plus", locales.EmptySessionsTitle(), locales.EmptySessionsHint(), nil)
		return
	}
	g := a.groupSessions()
	pinned, archived, folders, byFolder, cwdByFolder := g.pinned, g.archived, g.folders, g.byFolder, g.cwdByFolder
	if len(pinned) > 0 {
		a.group(c, "pin", locales.Pinned(), 0)
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
	if len(archived) > 0 {
		p := a.p
		b := ui.ButtonBase(c.Key("archived")).Row().Height(26).Margin(16, 0, 4, 0).Padding(0, 10).Gap(7).Radius(6).AlignItems(ui.Center).
			Label(locales.Archived()).Expanded(a.showArchived)
		focusRing(b, p)
		if b.Hovered() {
			b.Background(p.Hover)
		}
		if b.Clicked() {
			a.showArchived = !a.showArchived
		}
		b.Children(func() {
			sym(c, "archive", 13, p.Mute)
			ui.Text(c, locales.Archived()).FontSize(12.5).FontWeight(600).LetterSpacing(.18).TextColor(p.Mute).SingleLine()
			disclosureChevron(c, "archived", a.showArchived, 11, p.Mute)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(len(archived))).FontSize(12.5).FontWeight(500).TextColor(p.Mute)
		})
		if a.showArchived {
			for _, s := range archived {
				a.sessionRow(c, s)
			}
		}
	}
}

func (a *App) group(c *ui.Context, symbol, label string, n int) {
	p := a.p
	ui.Row(c).Height(24).Margin(16, 0, 4, 0).Padding(0, 10).Gap(7).AlignItems(ui.Center).Children(func() {
		if symbol != "" {
			sym(c, symbol, 13, p.Mute)
		}
		ui.Text(c, label).FontSize(12.5).FontWeight(600).LetterSpacing(.18).TextColor(p.Mute).SingleLine()
		if n > 0 {
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(n)).FontSize(12.5).FontWeight(500).TextColor(p.Mute)
		}
	})
}

func (a *App) projectGroup(c *ui.Context, label, cwd string, sessions []proto.Session) {
	p := a.p
	collapsed := a.projectCollapsed(cwd)
	row := ui.Row(c.Key("proj:"+cwd)).Height(28).Margin(13, 0, 3, 0).Padding(0, 4, 0, 8).Gap(4).Radius(7).AlignItems(ui.Center)
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
			// It deletes every session in it, for good: ask first, as
			// Finder does before emptying the Trash.
			a.removing, a.removingName = a.removing[:0], label
			for _, s := range sessions {
				a.removing = append(a.removing, s.ID)
			}
		}
	}
	row.ContextMenu(fillMenu)
	row.Children(func() {
		toggle := ui.ButtonBase(c.Key("proj-toggle:" + cwd)).Row().Grow(1).MinWidth(0).Height(28).Gap(6).AlignItems(ui.Center).
			KeepFocus().Label(label).Expanded(!collapsed)
		focusRing(toggle, p)
		toggle.Children(func() {
			disclosureChevron(c, "proj:"+cwd, !collapsed, 11, p.Mute)
			sym(c, "folder", 15, p.Fg2)
			ui.Text(c, label).FontSize(13.5).FontWeight(600).TextColor(p.Fg2).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
		if toggle.Clicked() {
			a.toggleProjectCollapsed(cwd)
		}
		menuOpen := a.menu.projectMenu == cwd
		if row.Hovered() || menuOpen {
			more := ui.ButtonBase(c.Key("proj-more:"+cwd)).Size(24, 24).Radius(6).Center().KeepFocus().Label(label)
			if menuOpen || more.Hovered() {
				more.Background(p.Sel)
			}
			more.Children(func() { sym(c, "dots", 14, p.Fg2) })
			more.PopoverMenuButton(fillMenu, a.menuOptions(&menuOpen))
			if menuOpen {
				a.menu.projectMenu = cwd
			} else if a.menu.projectMenu == cwd {
				a.menu.projectMenu = ""
			}

			add := ui.ButtonBase(c.Key("proj-new:"+cwd)).Size(24, 24).Radius(6).Center().KeepFocus().Label(locales.NewSession()).Tooltip(locales.NewSession())
			if add.Hovered() {
				add.Background(p.Sel)
			}
			add.Children(func() { sym(c, "plus", 14, p.Fg2) })
			if add.Clicked() {
				a.do(a.C.NewSessionIn(cwd))
			}
		}
	})
}

// state is what the slot left of a session's title shows: "wait" for an
// approval, "run" while a turn runs, "unread" or "fail" for a turn that
// ended unseen, "" otherwise.
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
	n := len(a.visibleSessions) // its ⌘ number
	row := ui.ButtonBase(c.Key("s:"+s.ID)).Row().Height(32).Padding(0, 9).Gap(9).Radius(8).AlignItems(ui.Center).Label(title(s))
	if row.Hovered() && !s.Updated.IsZero() {
		row.Tooltip(ago(a.now(), s.Updated))
	}
	focusRing(row, p)
	switch {
	case on:
		row.Background(p.Sel)
	case row.Hovered():
		row.Background(p.Hover)
	}
	if row.Clicked() {
		a.do(a.C.Open(s.ID))
	}
	row.ContextMenu(func(m *ui.Menu) { a.sessionMenu(m, s) })
	leftSt := st
	if leftSt == "wait" {
		leftSt = ""
	}
	if a.renaming == s.ID && a.renameIn == "side" {
		row.Children(func() {
			slot(c, p, leftSt)
			a.renameInput(c, s).FontSize(14)
		})
		return
	}
	row.Children(func() {
		slot(c, p, leftSt)
		fg, weight := p.Fg2, 400
		if on {
			fg, weight = p.Fg, 500
		}
		ui.Text(c, title(s)).FontSize(14).FontWeight(weight).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		switch {
		case c.Modifiers()&(ui.Cmd|ui.Shift|ui.Alt|ui.Ctrl) == ui.Cmd && n <= 9:
			ui.Text(c, fmt.Sprintf("⌘%d", n)).FontSize(11.5).FontWeight(500).FontFeatures("tnum").TextColor(p.Fg2).Background(p.Chip).Padding(1, 5).Radius(4).SingleLine()
		case st == "wait":
			badge := locales.BadgeApproval()
			if on && a.questionFirst() {
				badge = locales.BadgeQuestion()
			}
			ui.Text(c, badge).FontSize(11.5).FontWeight(500).TextColor(p.Live).Background(p.LiveBg).Padding(1, 6).Radius(5)
		}
	})
}

// slot is the 12-point status mark of a session.
func slot(c *ui.Context, p *Palette, st string) {
	ui.Box(c).Size(12, 14).Shrink(0).Center().Children(func() {
		switch st {
		case "wait":
			ui.Box(c).Size(7, 7).Radius(3.5).Background(p.Live)
		case "run":
			spinner(c, p, 12)
		case "unread":
			ui.Box(c).Size(6, 6).Radius(3).Background(p.Fg2)
		case "fail":
			ui.Box(c).Size(7, 7).Radius(2).Background(p.Minus)
		}
	})
}

// spinner is a faint ring with a quarter of it lit, going round: work
// under way.
func spinner(c *ui.Context, p *Palette, size float32) ui.Element {
	ring := ui.Box(c).Size(size, size).Radius(size/2).Border(1.6, p.LiveBg.Alpha(.2))
	turn := ring.Loop("spin", 900*time.Millisecond, ui.Linear)
	black := ui.RGBA(0, 0, 0, 1)
	ring.Children(func() {
		ui.Box(c).Absolute().Fill().Radius(size/2).Border(1.6, p.Live).
			MaskConicGradient(ui.ConicGradient{Angle: turn * 360, Stops: []ui.GradientStop{{Color: black, At: 0}, {Color: black, At: .28}, {Color: ui.Transparent, At: .3}, {Color: ui.Transparent, At: 1}}})
	})
	return ring
}

func (a *App) footer(c *ui.Context) {
	p := a.p
	cur := a.account()
	var q *proto.AccountQuota
	if cur != nil {
		q = cur.Quota
	}
	left, _ := formatQuota(a.now(), p, q)
	ui.Column(c).Shrink(0).Padding(6, 8, 10, 8).Children(func() {
		label := "Stratum"
		if left != "" {
			label = locales.AccountQuota("Stratum", left)
		}
		b := ui.ButtonBase(c.Key("profile-card")).Row().Height(32).Padding(0, 8, 0, 10).Gap(8).Radius(8).
			AlignItems(ui.Center).Label(label)
		if left != "" {
			b.Tooltip(left)
		}
		focusRing(b, p)
		switch {
		case a.menu.accountMenuOpen:
			b.Background(p.Sel)
		case b.Hovered():
			b.Background(p.Hover)
		}
		if b.Clicked() {
			a.menu.accountMenuOpen = !a.menu.accountMenuOpen
		}
		b.Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
				brandMark(c, 16, p.Fg)
				ui.Text(c, "Stratum").FontSize(13.5).FontWeight(600).LetterSpacing(-.18).TextColor(p.Fg).SingleLine().Ellipsis("…")
			})
			if q != nil && q.RemainingPct >= 0 {
				low := q.RemainingPct < 15
				ink := p.Mute
				if low {
					ink = p.Minus
				}
				ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Image(c, ring(p, q.RemainingPct, low)).Size(14, 14).Shrink(0)
					ui.Text(c, fmt.Sprintf("%d%%", q.RemainingPct)).FontSize(12.5).TextColor(ink).FontFeatures("tnum").SingleLine()
				})
			}
			chevron := "chevron-up"
			if a.menu.accountMenuOpen {
				chevron = "chevron-down"
			}
			sym(c, chevron, 11, p.Mute)
		})
		a.footerPopover(c, b, q)
	})
}

// footerPopover shows the Scheme B brand/quota summary card and Settings action
// above the sidebar footer button.
func (a *App) footerPopover(c *ui.Context, anchor ui.Element, q *proto.AccountQuota) {
	p := a.p
	pop := ui.Popover(c, anchor, &a.menu.accountMenuOpen, func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Column(c).FillWidth().Background(p.Chip).Radius(8).Padding(10, 12).Gap(7).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						brandMark(c, 14, p.Fg)
						ui.Text(c, "Stratum").FontSize(13).FontWeight(600).LetterSpacing(-.15).TextColor(p.Fg).SingleLine()
					})
					ui.Spacer(c)
					if q != nil && q.RemainingPct >= 0 {
						pct := max(0, min(100, q.RemainingPct))
						pctCol := p.Fg
						if pct < 15 {
							pctCol = p.Minus
						}
						ui.Text(c, locales.QuotaRemaining(pct)).FontSize(12).FontWeight(600).TextColor(pctCol).FontFeatures("tnum").SingleLine()
					}
				})
				if q != nil && q.RemainingPct >= 0 {
					pct := max(0, min(100, q.RemainingPct))
					barCol := p.Fg
					if pct < 15 {
						barCol = p.Minus
					} else if pct < 35 {
						barCol = p.Live
					}
					ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.Edge2).Clip().Children(func() {
						ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(barCol)
					})
				}
				if reset := formatQuotaReset(a.now(), q); reset != "" {
					ui.Text(c, reset).FontSize(11.5).TextColor(p.Mute).SingleLine()
				}
			})

			row := ui.ButtonBase(c.Key("footer-settings")).Row().FillWidth().Height(32).Padding(0, 10).Gap(8).Radius(7).
				AlignItems(ui.Center).Label(locales.SettingsMenu())
			focusRing(row, p)
			if row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				sym(c, "settings", 15, p.Fg2)
				ui.Text(c, locales.SettingsMenu()).FontSize(13.5).TextColor(p.Fg).SingleLine()
				ui.Spacer(c)
				ui.Text(c, "⌘,").FontSize(12).TextColor(p.Mute).SingleLine()
			})
			if row.Clicked() {
				a.menu.accountMenuOpen = false
				a.openSettings()
			}
		})
	})
	pop.AttachTo(anchor, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).
		Width(248).Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(6).NoShadow()
	lift(pop, p)
	rim(pop, p)
}

// accountName is what an account is called: its name, or its ID.
func accountName(acc proto.Account) string {
	if acc.Name != "" {
		return acc.Name
	}
	return acc.ID
}

// account is the current session's account, or the first one.
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

// ago says how long ago t was, as briefly as the sidebar wants it.
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
