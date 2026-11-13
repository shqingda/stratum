package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

const (
	barHeight    = 52
	sidebarWidth = 252
)

// sym draws an SF Symbol.
// iconButton is a 28-point square button holding a symbol.
func (a *App) iconButton(c *ui.Context, key, symbol, tip string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, 17, a.p.Fg2) })
	return b
}

func (a *App) sidebar(c *ui.Context, vibrant bool) {
	p := a.p
	side := ui.Column(c).Width(sidebarWidth).Shrink(0).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
	if !vibrant {
		side.Background(p.Side)
	}
	side.Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Height(barHeight).Shrink(0).AlignItems(ui.Center).Padding(0, 12, 0, bar.Left+8).Gap(2).DragWindow().Children(func() {
			ui.Spacer(c)
			if a.iconButton(c, "side", "layout-sidebar-left-collapse", "隐藏边栏").Clicked() {
				a.noSide = true
			}
			if a.iconButton(c, "new-top", "edit", "新会话").Clicked() {
				a.do(a.C.NewSession())
			}
		})
		a.search(c)
		ui.Column(c).Padding(2, 8, 6, 8).Children(func() {
			if a.navItem(c, "new", "message-plus", "新会话", "⌘N").KeyShortcuts(ui.Cmd, ui.KeyN).Clicked() {
				a.do(a.C.NewSession())
			}
			a.navItem(c, "cron", "clock-play", "定时任务", "")
			a.navItem(c, "skills", "puzzle", "技能与 MCP", "")
		})
		list := ui.Scroll(c).Grow(1).Padding(0, 8, 12, 8).Children(func() { a.sessions(c) })
		// The list fades into the footer rather than being cut at it.
		if h := list.Bounds().H; h > 0 {
			black := ui.RGBA(0, 0, 0, 1)
			list.MaskLinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: black, At: 0}, {Color: black, At: 1 - 36/h}, {Color: ui.Transparent, At: 1}}})
		}
		a.footer(c)
	})
}

func (a *App) search(c *ui.Context) {
	p := a.p
	b := ui.ButtonBase(c.Key("search")).Row().Height(30).Margin(0, 10, 8, 10).Padding(0, 9).Gap(7).Radius(8).Background(p.Chip).AlignItems(ui.Center).Label("搜索")
	if b.Clicked() {
		a.openPalette(c)
	}
	b.Children(func() {
		sym(c, "search", 15, p.Mute)
		ui.Text(c, "搜索").FontSize(13).TextColor(p.Mute).Grow(1)
		ui.Text(c, "⌘K").FontSize(11).TextColor(p.Mute)
	})
}

func (a *App) navItem(c *ui.Context, key, symbol, label, kbd string) ui.Element {
	p := a.p
	it := ui.ButtonBase(c.Key(key)).Row().Height(30).Padding(0, 10).Gap(9).Radius(7).AlignItems(ui.Center).Label(label)
	if it.Hovered() {
		it.Background(p.Hover)
	}
	it.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, symbol, 16, p.Fg2) })
		ui.Text(c, label).FontSize(13).TextColor(p.Fg).SingleLine()
		ui.Spacer(c)
		if kbd != "" {
			ui.Text(c, kbd).FontSize(11).TextColor(p.Mute)
		}
	})
	return it
}

// sessions lists the sessions: the ones waiting on the user first, then
// the rest by workspace, each newest first.
func (a *App) sessions(c *ui.Context) {
	a.visibleSessions = a.visibleSessions[:0]
	var waiting []proto.Session
	var folders []string
	byFolder := map[string][]proto.Session{}
	for _, s := range a.C.Sessions {
		if a.state(s) == "wait" {
			waiting = append(waiting, s)
			continue
		}
		f := filepath.Base(s.Cwd)
		if _, ok := byFolder[f]; !ok {
			folders = append(folders, f)
		}
		byFolder[f] = append(byFolder[f], s)
	}
	if len(waiting) > 0 {
		a.group(c, "", "需要你", len(waiting))
		for _, s := range waiting {
			a.sessionRow(c, s)
		}
	}
	for _, f := range folders {
		a.group(c, "folder", f, 0)
		for _, s := range byFolder[f] {
			a.sessionRow(c, s)
		}
	}
}

func (a *App) group(c *ui.Context, symbol, label string, n int) {
	p := a.p
	ui.Row(c).Height(28).Margin(10, 0, 0, 0).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		if symbol != "" {
			sym(c, symbol, 13, p.Mute)
		}
		ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(p.Mute).SingleLine()
		if n > 0 {
			ui.Spacer(c)
			ui.Text(c, fmt.Sprint(n)).FontSize(11.5).FontWeight(500).TextColor(p.Mute)
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
	row := ui.ButtonBase(c.Key("s:"+s.ID)).Row().Height(30).Padding(0, 10).Gap(9).Radius(7).AlignItems(ui.Center).Label(title(s))
	switch {
	case on:
		row.Background(p.Sel)
	case row.Hovered():
		row.Background(p.Hover)
	}
	if row.Clicked() {
		a.do(a.C.Open(s.ID))
	}
	row.Children(func() {
		slot(c, p, st)
		fg := p.Fg2
		if on {
			fg = p.Fg
		}
		ui.Text(c, title(s)).FontSize(13).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		switch {
		case c.Modifiers() == ui.Cmd && n <= 9:
			ui.Text(c, fmt.Sprintf("⌘%d", n)).FontSize(11.5).TextColor(p.Mute).SingleLine()
		case st == "wait":
			badge := "审批"
			if on && a.questionFirst() {
				badge = "提问"
			}
			ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(p.Live).Background(p.LiveBg).Padding(1, 6).Radius(5)
		default:
			ui.Text(c, ago(a.now(), s.Updated)).FontSize(11.5).TextColor(p.Mute).SingleLine()
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
			ui.Box(c).Size(2, 12).Background(p.Live)
		case "unread":
			ui.Box(c).Size(6, 6).Radius(3).Background(p.Fg2)
		case "fail":
			ui.Box(c).Size(7, 2).Background(p.Minus)
		}
	})
}

func (a *App) footer(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 14).Gap(9).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		name, sub := "未登录", "在设置里添加账号"
		if acc := a.account(); acc != nil {
			name = acc.Name
			if name == "" {
				name = acc.ID
			}
			sub = acc.Email
			if acc.Subscription && sub == "" {
				sub = "订阅"
			}
		}
		ui.Box(c).Size(24, 24).Shrink(0).Radius(6).Background(p.Acc).Center().Children(func() {
			ui.Text(c, initials(name)).TextColor(p.OnAcc).FontSize(11).FontWeight(600).SingleLine()
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).FontSize(12.5).TextColor(p.Fg).SingleLine().Ellipsis("…")
			if sub != "" {
				ui.Text(c, sub).FontSize(11).TextColor(p.Mute).SingleLine().Ellipsis("…")
			}
		})
		if a.iconButton(c, "settings", "settings", "设置").Clicked() {
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
	return "新会话"
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
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分", int(d.Minutes()))
	case sameDay:
		return fmt.Sprintf("%d 时", int(d.Hours()))
	case now.AddDate(0, 0, -1).Day() == d2 && d < 48*time.Hour:
		return "昨天"
	case d < 6*24*time.Hour:
		return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[t.Weekday()]
	case y1 == y2:
		return fmt.Sprintf("%d 月", int(m2))
	}
	return fmt.Sprintf("%d 年", y2)
}
