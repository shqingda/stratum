package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
)

var efforts = map[string]string{"": "默认", "minimal": "最低", "low": "低", "medium": "中", "high": "高", "xhigh": "极高"}

// composer is the box the user writes in, under the transcript: prompts
// waiting for the running turn above it, the session's settings in it and
// a status line under it.
func (a *App) composer(c *ui.Context) {
	p := a.p
	ui.Row(c).Justify(ui.Center).Padding(0, 28, 12, 28).Shrink(0).Children(func() {
		ui.Column(c).Grow(1).MaxWidth(columnWidth).Children(func() {
			for i, q := range a.C.Queued {
				ui.Row(c.Key(i)).Height(34).Padding(0, 6, 0, 12).Margin(0, 10, -1, 10).Gap(8).AlignItems(ui.Center).
					Radius(10, 10, 0, 0).Background(p.Pane).Border(.5, p.Edge).Children(func() {
					sym(c, "arrow.turn.down.right", 12, p.Mute)
					ui.Text(c, q).FontSize(12.5).TextColor(p.Fg2).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				})
			}
			a.box(c)
			if a.err != "" {
				ui.Text(c, a.err).FontSize(11.5).TextColor(p.Minus).Margin(8, 0, 0, 0).Center()
			} else {
				a.status(c)
			}
		})
	})
}

func (a *App) box(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	ui.Column(c).Radius(14).Background(p.Card).Border(.5, p.Edge2).Padding(12, 10, 8, 14).
		Shadow(0, 8, 24, -12, ui.RGBA(0, 0, 0, .12)).Children(func() {
		ph := "让 Talos 做点什么"
		if a.C.Busy {
			ph = "运行中：回车排队"
		}
		ui.TextAreaBase(c, &a.draft).Lines(2, 10).Placeholder(ph).PlaceholderColor(p.Mute).TextColor(p.Fg).
			LineHeight(1.45).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods == 0:
				a.submit()
				return true
			case k.Key == ui.KeyEscape && a.C.Busy:
				a.do(a.C.Cancel())
				return true
			}
			return false
		})
		ui.Row(c).Gap(2).Margin(6, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			if cur != nil {
				a.pill(c, "model", "", cur.Model)
				a.pill(c, "effort", "brain", efforts[cur.Effort])
				a.pill(c, "mode", "shield.lefthalf.filled", modes[cur.Mode])
			}
			ui.Spacer(c)
			a.goButton(c)
		})
	})
}

func (a *App) submit() {
	if strings.TrimSpace(a.draft) == "" {
		return
	}
	cmds, err := a.C.Prompt(a.draft)
	a.do(cmds, err)
	if err == nil {
		a.draft = ""
	}
}

func (a *App) pill(c *ui.Context, key, symbol, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key("pill:"+key)).Row().Height(28).Padding(0, 8).Gap(5).Radius(7).AlignItems(ui.Center).Label(label)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		if symbol != "" {
			sym(c, symbol, 13, p.Fg2)
		}
		ui.Text(c, label).FontSize(12.5).TextColor(p.Fg2).SingleLine()
		sym(c, "chevron.down", 9, p.Mute)
	})
	return b
}

// goButton sends, or stops the running turn.
func (a *App) goButton(c *ui.Context) {
	p := a.p
	b := ui.ButtonBase(c.Key("go")).Size(30, 30).Radius(15).Center().Margin(0, 0, 0, 4)
	switch {
	case a.C.Busy && strings.TrimSpace(a.draft) == "":
		b.Label("停止").Background(p.Fg).Children(func() { ui.Box(c).Size(10, 10).Radius(2).Background(p.Bg) })
		if b.Clicked() {
			a.do(a.C.Cancel())
		}
	case strings.TrimSpace(a.draft) == "" || !a.C.CanAct():
		b.Label("发送").Background(p.Chip).Children(func() { sym(c, "arrow.up", 14, p.Mute).Weight(1) })
	default:
		b.Label("发送").Background(p.Acc).Children(func() { sym(c, "arrow.up", 14, p.OnAcc).Weight(1) })
		if b.Clicked() {
			a.submit()
		}
	}
}

func (a *App) status(c *ui.Context) {
	p := a.p
	ui.Row(c).Gap(14).Justify(ui.Center).Margin(8, 0, 0, 0).Children(func() {
		item := func(symbol, label string) {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				sym(c, symbol, 11, p.Mute)
				ui.Text(c, label).FontSize(11.5).TextColor(p.Mute).SingleLine()
			})
		}
		item("laptopcomputer", "本机")
		if a.C.Phase != client.Ready {
			item("bolt.horizontal", a.C.Phase.String())
		}
		item("lock", "沙箱开启")
	})
}
