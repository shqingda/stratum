package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

const (
	barHeight    = 52
	sheetBar     = 48 // the title bar on a sheet, under the gap above it
	sidebarWidth = 264
)

// sym draws an SF Symbol.
// iconButton is a 28-point square button holding a symbol.
func (a *App) iconButton(c *ui.Context, key, symbol, tip string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip)
	press(b)
	focusRing(b, a.p)
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, 17, a.p.Fg2) })
	return b
}

func (a *App) sidebar(c *ui.Context, vibrant bool) {
	p := a.p
	side := ui.Column(c).Width(a.sideWidth()).Shrink(0)
	topGap := float32(barHeight)
	if vibrant {
		// Window VibrancyGlass provides the single native glass layer; add a
		// Tinted frost wash and one right hairline so there are no nested frames.
		tint := ui.RGBA(244, 245, 248, .38)
		if p == dark {
			tint = ui.RGBA(18, 18, 20, .48)
		}
		side.Background(tint).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
	} else {
		side.Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
	}
	side.Children(func() {
		ui.Box(c).Height(topGap).Shrink(0).DragWindow()
		ui.Row(c).Padding(4, 20, 14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
			brandMark(c, 22, p.Fg)
			ui.Text(c, "Talos").FontSize(16).FontWeight(600).LetterSpacing(-.25).TextColor(p.Fg)
		})
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
	if a.noSide {
		tip = locales.ShowSidebar()
	}
	const pillW, pillH = float32(74), float32(32)
	leftClosed := toggleLeft(c)
	leftOpen := a.sideWidth() - 14 - pillW
	left := leftClosed + a.sideOpen*(leftOpen-leftClosed)
	bg := ui.RGBA(255, 255, 255, .94)
	shadow := ui.RGBA(0, 0, 0, .08)
	if p == dark {
		bg = ui.RGBA(38, 38, 42, .90)
		shadow = ui.RGBA(0, 0, 0, .36)
	}
	pill := ui.Row(c.Key("side-pill")).Absolute().Left(left).Top((barHeight-pillH)/2).
		Size(pillW, pillH).Radius(pillH/2).Padding(0, 5).Gap(4).AlignItems(ui.Center).Justify(ui.Center).
		Background(bg).Border(.5, p.Edge).Shadow(0, 2, 6, -1, shadow).ZIndex(10)
	pill.Children(func() {
		newBtn := ui.ButtonBase(c.Key("bar-new")).Size(30, 26).Radius(13).Center().
			Label(locales.NewSession()).Tooltip(locales.NewSession()).KeyShortcuts(ui.Cmd, ui.KeyN)
		press(newBtn)
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
		press(togBtn)
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
	it := ui.ButtonBase(c.Key(key)).Row().Height(36).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(label)
	press(it)
	focusRing(it, p)
	if it.Hovered() {
		it.Background(p.Hover)
	}
	it.Children(func() {
		ui.Box(c).Width(17).Center().Children(func() { sym(c, symbol, 17, p.Fg2) })
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
	waiting, pinned, archived []proto.Session
	folders                   []string
	byFolder                  map[string][]proto.Session
	cwdByFolder               map[string]string
}

func (a *App) groupSessions() sessionGroups {
	g := sessionGroups{byFolder: map[string][]proto.Session{}, cwdByFolder: map[string]string{}}
	for _, s := range a.C.Sessions {
		switch {
		case a.state(s) == "wait":
			g.waiting = append(g.waiting, s)
			continue
		case s.Archived:
			g.archived = append(g.archived, s)
			continue
		case s.Pinned:
			g.pinned = append(g.pinned, s)
			continue
		}
		f := filepath.Base(s.Cwd)
		if _, ok := g.byFolder[f]; !ok {
			g.folders = append(g.folders, f)
			g.cwdByFolder[f] = s.Cwd
		}
		g.byFolder[f] = append(g.byFolder[f], s)
	}
	return g
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
	add(g.waiting)
	add(g.pinned)
	for _, f := range g.folders {
		add(g.byFolder[f])
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
	waiting, pinned, archived, folders, byFolder, cwdByFolder := g.waiting, g.pinned, g.archived, g.folders, g.byFolder, g.cwdByFolder
	if len(waiting) > 0 {
		a.group(c, "", locales.NeedsYou(), len(waiting))
		for _, s := range waiting {
			a.sessionRow(c, s)
		}
	}
	if len(pinned) > 0 {
		a.group(c, "pin", locales.Pinned(), 0)
		for _, s := range pinned {
			a.sessionRow(c, s)
		}
	}
	for _, f := range folders {
		a.projectGroup(c, f, cwdByFolder[f], byFolder[f])
		for _, s := range byFolder[f] {
			a.sessionRow(c, s)
		}
	}
	if len(archived) > 0 {
		p := a.p
		b := ui.ButtonBase(c.Key("archived")).Row().Height(26).Margin(16, 0, 4, 0).Padding(0, 10).Gap(7).Radius(6).AlignItems(ui.Center).
			Label(locales.Archived()).Expanded(a.showArchived)
		press(b)
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
	row := ui.Row(c.Key("proj:"+cwd)).Height(28).Margin(14, 0, 4, 0).Padding(0, 6, 0, 10).Gap(8).Radius(7).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		sym(c, "folder", 16, p.Fg2)
		ui.Text(c, label).FontSize(13.5).FontWeight(600).TextColor(p.Fg2).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if row.Hovered() {
			more := ui.ButtonBase(c.Key("proj-more:"+cwd)).Size(22, 22).Radius(6).Center().Label(label)
			if more.Hovered() {
				more.Background(p.Sel)
			}
			more.Children(func() { sym(c, "dots", 14, p.Fg2) })
			more.PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(locales.NewSession()).Chosen() {
					a.do(a.C.NewSessionIn(cwd))
				}
				if cwd != "" && m.Item(locales.RevealInFinder()).Chosen() {
					_ = exec.Command("open", "-R", cwd).Start()
				}
				m.Separator()
				if m.Item(locales.RemoveProject()+"…").Danger().Chosen() {
					// It deletes every session in it, for good: ask first, as
					// Finder does before emptying the Trash.
					a.removing, a.removingName = a.removing[:0], label
					for _, s := range sessions {
						a.removing = append(a.removing, s.ID)
					}
				}
			}, a.menuOptions(nil))

			add := ui.ButtonBase(c.Key("proj-new:"+cwd)).Size(22, 22).Radius(6).Center().Label(locales.NewSession()).Tooltip(locales.NewSession())
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
	row := ui.ButtonBase(c.Key("s:"+s.ID)).Row().Height(32).Padding(0, 10).Gap(9).Radius(8).AlignItems(ui.Center).Label(title(s))
	row.Transition(ui.ElementTransition{Position: true, Colors: true, Shadows: true, Duration: 180 * time.Millisecond})
	focusRing(row, p)
	switch {
	case on && p == dark:
		row.Background(p.Sel)
	case on:
		// The open session is a pill of paper raised off the sidebar.
		row.Background(p.Card).Shadow(0, 0, 0, .5, p.Edge).Shadow(0, 1, 2, 0, ink.Alpha(.06))
	case row.Hovered():
		row.Background(p.Hover)
	}
	if row.Clicked() {
		a.do(a.C.Open(s.ID))
	}
	row.ContextMenu(func(m *ui.Menu) { a.sessionMenu(m, s) })
	press(row)
	if a.renaming == s.ID && a.renameIn == "side" {
		row.Children(func() {
			slot(c, p, st)
			a.renameInput(c, s).FontSize(14)
		})
		return
	}
	row.Children(func() {
		slot(c, p, st)
		fg := p.Fg2
		if on {
			fg = p.Fg
		}
		ui.Text(c, title(s)).FontSize(14).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		switch {
		case c.Modifiers() == ui.Cmd && n <= 9:
			ui.Text(c, fmt.Sprintf("⌘%d", n)).FontSize(12.5).TextColor(p.Mute).SingleLine()
		case st == "wait":
			badge := locales.BadgeApproval()
			if on && a.questionFirst() {
				badge = locales.BadgeQuestion()
			}
			ui.Text(c, badge).FontSize(12).FontWeight(600).TextColor(p.Live).Background(p.LiveBg).Padding(1, 6).Radius(5)
		default:
			ui.Text(c, ago(a.now(), s.Updated)).FontSize(12.5).TextColor(p.Mute).SingleLine()
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
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 10, 0, 10).Gap(6).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		name, sub := locales.NotSignedIn(), locales.AddAccountInSettings()
		curAcc := a.account()
		if curAcc != nil {
			name = curAcc.Name
			if name == "" {
				name = curAcc.ID
			}
			sub = curAcc.Email
			if qText, _ := formatQuota(a.now(), p, curAcc.Quota); qText != "" {
				sub = qText
			} else if curAcc.Subscription && sub == "" {
				sub = locales.Subscription()
			}
		}
		profile := ui.ButtonBase(c.Key("profile-card")).Row().Grow(1).MinWidth(0).Height(38).Padding(0, 6).Gap(9).Radius(9).
			AlignItems(ui.Center).Justify(ui.Start).Label(name)
		press(profile)
		focusRing(profile, p)
		if profile.Hovered() {
			profile.Background(p.Hover)
		}
		profile.Children(func() {
			ui.Box(c).Size(24, 24).Shrink(0).Radius(6).Background(p.Acc).Center().Children(func() {
				ui.Text(c, initials(name)).TextColor(p.OnAcc).FontSize(12).FontWeight(600).SingleLine()
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, name).FontSize(13.5).TextColor(p.Fg).SingleLine().Ellipsis("…")
				if sub != "" {
					ui.Text(c, sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
				}
			})
		}).PopoverMenuButton(func(m *ui.Menu) {
			if len(a.C.Accounts) > 0 {
				for _, acc := range a.C.Accounts {
					accName := acc.Name
					if accName == "" {
						accName = acc.ID
					}
					id := acc.ID
					checked := curAcc != nil && curAcc.ID == id
					item := m.Item(accName).Checked(checked)
					if a.C.Current != nil && item.Chosen() {
						a.do(a.C.Update(proto.SessionUpdate{Account: &id}))
						a.flash("sparkles", accName)
					}
				}
				m.Separator()
			}
			if m.Item(locales.SettingsAccounts()).Chosen() {
				a.openSettingsPage("accounts")
			}
			if m.Item(locales.SettingsMcp()).Chosen() {
				a.openMCP()
			}
			if m.Item(locales.Settings()).Chosen() {
				a.openSettings()
			}
		}, a.menuOptions(nil))
		if a.iconButton(c, "settings", "settings", locales.Settings()).Clicked() {
			a.openSettings()
		}
	})
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
