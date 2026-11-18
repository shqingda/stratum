package app

import (
	"fmt"
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
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, 17, a.p.Fg2) })
	return b
}

func (a *App) sidebar(c *ui.Context, vibrant bool) {
	p := a.p
	side := ui.Column(c).Width(sidebarWidth).Shrink(0)
	topGap := float32(barHeight)
	if vibrant {
		// Tinted Liquid Glass: higher opacity and contrast than Clear so
		// sidebar items stay legible above any desktop wallpaper.
		side.Width(sidebarWidth-12).Margin(8, 4, 8, 8).Radius(16).
			Material(tintedGlass(p)).Border(.5, p.Edge).Clip()
		rim(side, p)
		topGap = barHeight - 8
	} else {
		side.Background(p.Side)
	}
	side.Children(func() {
		ui.Box(c).Height(topGap).Shrink(0).DragWindow()
		ui.Row(c).Padding(4, 20, 14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
			brandMark(c, 22, p.Fg)
			ui.Text(c, "Talos").FontSize(16).FontWeight(600).TextColor(p.Fg)
		})
		ui.Column(c).Padding(0, 12, 20, 12).Gap(2).Children(func() {
			if a.navItem(c, "new-top", "edit", locales.NewSession(), "⌘ N").KeyShortcuts(ui.Cmd, ui.KeyN).Clicked() {
				a.do(a.C.NewSession())
			}
			if a.navItem(c, "search", "search", locales.Search(), "⌘ K").Clicked() {
				a.openPalette(c)
			}
			if a.navItem(c, "skills", "puzzle", locales.SettingsMcp(), "").Clicked() {
				a.openMCP()
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

// sideToggle shows or hides the sidebar. It stays beside the window
// controls whether the sidebar is out or not, so it is always where the
// pointer left it.
func (a *App) sideToggle(c *ui.Context) {
	tip := locales.HideSidebar()
	if a.noSide {
		tip = locales.ShowSidebar()
	}
	ui.Box(c).Absolute().Left(toggleLeft(c)).Top((barHeight-30)/2).Size(30, 30).ZIndex(10).Children(func() {
		b := ui.ButtonBase(c.Key("side")).Size(30, 30).Radius(15).Center().
			Material(glassButton(a.p)).Label(tip).Tooltip(tip)
		press(b)
		color := a.p.Fg2
		if b.Hovered() {
			color = a.p.Fg
		}
		b.Children(func() { sym(c, "layout-sidebar", 16, color) })
		if b.Clicked() {
			a.toggleSide()
		}
	})
}

// toggleLeft is where the sidebar's toggle sits: past the window controls.
func toggleLeft(c *ui.Context) float32 { return max(c.TitleBar().Left+10, 14) }

func (a *App) navItem(c *ui.Context, key, symbol, label, kbd string) ui.Element {
	p := a.p
	it := ui.ButtonBase(c.Key(key)).Row().Height(36).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(label)
	press(it)
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

// sessions lists the sessions: the ones waiting on the user first, then
// the rest by workspace, each newest first.
func (a *App) sessions(c *ui.Context) {
	a.visibleSessions = a.visibleSessions[:0]
	var waiting, pinned, archived []proto.Session
	var folders []string
	byFolder := map[string][]proto.Session{}
	for _, s := range a.C.Sessions {
		switch {
		case a.state(s) == "wait":
			waiting = append(waiting, s)
			continue
		case s.Archived:
			archived = append(archived, s)
			continue
		case s.Pinned:
			pinned = append(pinned, s)
			continue
		}
		f := filepath.Base(s.Cwd)
		if _, ok := byFolder[f]; !ok {
			folders = append(folders, f)
		}
		byFolder[f] = append(byFolder[f], s)
	}
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
		a.group(c, "folder", f, 0)
		for _, s := range byFolder[f] {
			a.sessionRow(c, s)
		}
	}
	if len(archived) > 0 {
		p := a.p
		b := ui.ButtonBase(c.Key("archived")).Row().Height(24).Margin(16, 0, 4, 0).Padding(0, 10).Gap(7).AlignItems(ui.Center).
			Label(locales.Archived()).Expanded(a.showArchived)
		if b.Clicked() {
			a.showArchived = !a.showArchived
		}
		b.Children(func() {
			sym(c, "archive", 13, p.Mute)
			ui.Text(c, locales.Archived()).FontSize(12.5).FontWeight(600).TextColor(p.Mute).SingleLine()
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[a.showArchived], 11, p.Mute)
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
		ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(p.Mute).SingleLine()
		if n > 0 {
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(n)).FontSize(12.5).FontWeight(500).TextColor(p.Mute)
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
	row := ui.ButtonBase(c.Key("s:"+s.ID)).Row().Height(30).Padding(0, 10).Gap(9).Radius(8).AlignItems(ui.Center).Label(title(s))
	row.Transition(ui.ElementTransition{Position: true, Colors: true, Shadows: true, Duration: 180 * time.Millisecond})
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
			if curAcc.Subscription && sub == "" {
				sub = locales.Subscription()
			}
		}
		profile := ui.ButtonBase(c.Key("profile-card")).Row().Grow(1).MinWidth(0).Height(38).Padding(0, 6).Gap(9).Radius(9).
			AlignItems(ui.Center).Justify(ui.Start).Label(name)
		press(profile)
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
