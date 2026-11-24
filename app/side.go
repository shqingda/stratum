package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/locales"
)

func (a *App) openSideAsk() {
	if a.C.Current == nil {
		return
	}
	a.rv.maximized = false
	a.btwOpen = true
	a.btwFocus = true
}

func (a *App) sendSideAsk(q string) {
	q = strings.TrimSpace(q)
	if q == "" || a.C.Current == nil {
		return
	}
	a.rv.maximized = false
	a.btwOpen = true
	a.do(a.C.SideAsk(q))
}

func (a *App) sideAskCard(c *ui.Context) {
	if !a.btwOpen {
		return
	}
	p := a.p
	top := float32(sheetBar + 4)
	if a.find.on {
		top += 46
	}
	card := ui.Column(c.Key("side-ask")).Absolute().Top(top).Right(20).Width(388).MaxWidthPercent(92).ZIndex(18).
		Radius(12).Background(p.Card).Clip().Label(locales.SideAsk()).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Scale: .98, Opacity: 0}, Duration: 160 * time.Millisecond})
	rim(card, p)
	lift(card, p)
	card.Children(func() {
		ui.Row(c).Height(38).Padding(0, 10, 0, 14).Gap(7).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
			sym(c, "message", 14, p.Mute)
			ui.Text(c, locales.SideAsk()).FontSize(13.5).FontWeight(600).TextColor(p.Fg).SingleLine()
			ui.Row(c).Height(20).Padding(0, 8).Radius(6).Background(p.Chip).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.SideAskBadge()).FontSize(11).FontWeight(500).TextColor(p.Mute).SingleLine()
			})
			ui.Spacer(c)
			if a.iconButtonSized(c, "side-close", "x", locales.Close(), 13).KeyShortcuts(ui.Cmd, ui.KeyW).Clicked() {
				a.btwOpen = false
			}
		})

		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
			inp := ui.TextInputBase(c.Key("side-input"), &a.btwDraft).Bind(&a.btwInput).
				Label(locales.SideAsk()).Placeholder(locales.SideAskPlaceholder()).PlaceholderColor(p.Mute).
				FontSize(13.5).TextColor(p.Fg).Grow(1).MinWidth(0).SingleLine().
				OnTextKey(func(k ui.TextKey) bool {
					if (k.Key == ui.KeyEscape && k.Mods == 0) || (k.Key == ui.KeyW && k.Mods == ui.Cmd) {
						a.btwOpen = false
						return true
					}
					return false
				})
			if a.btwFocus {
				a.btwFocus = false
				a.btwInput.Focus(c)
			}
			if inp.Submitted() && strings.TrimSpace(a.btwDraft) != "" {
				q := a.btwDraft
				a.btwDraft = ""
				a.sendSideAsk(q)
			}
			sendBtn := ui.ButtonBase(c.Key("side-send")).Size(26, 26).Radius(7).Center().Label(locales.Send())
			ready := strings.TrimSpace(a.btwDraft) != "" && a.C.Current != nil && a.C.CanAct()
			focusRing(sendBtn, p)
			if ready {
				sendBtn.Background(p.Acc).Children(func() { sym(c, "arrow-up", 13, p.OnAcc) })
				if sendBtn.Clicked() {
					q := a.btwDraft
					a.btwDraft = ""
					a.sendSideAsk(q)
				}
			} else {
				sendBtn.Background(p.Acc.Alpha(idleInk)).Children(func() { sym(c, "arrow-up", 13, p.OnAcc) })
			}
		})

		curID := ""
		if a.C.Current != nil {
			curID = a.C.Current.ID
		}
		if a.C.SideQuestion != "" && a.C.SideSession == curID {
			ui.Scroll(c).MaxHeight(280).Padding(10, 14, 12, 14).Children(func() {
				ui.Text(c, a.C.SideQuestion).FontSize(13).FontWeight(600).TextColor(p.Fg2).Margin(0, 0, 8, 0)
				switch {
				case a.C.SidePending:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						spinner(c, p, 14)
						ui.Text(c, locales.Reading()).FontSize(13).TextColor(p.Mute)
					})
				case a.C.SideErr != "":
					ui.Text(c, a.C.SideErr).FontSize(13).TextColor(p.Minus).Selectable()
				case a.C.SideAnswer != "":
					a.md.Theme = mdTheme(p, c.Theme())
					a.md.CodeBlock = a.codeBlock
					markdown.View(c, a.C.SideAnswer, &a.md)
					ui.Row(c).Margin(10, 0, 0, 0).Justify(ui.End).Children(func() {
						if a.textButton(c, "side-copy", locales.Copy()).Clicked() {
							c.WriteClipboard(a.C.SideAnswer)
							a.flash("check", locales.Copy())
						}
					})
				}
			})
		} else {
			ui.Row(c).Padding(10, 14, 12, 14).Children(func() {
				ui.Text(c, locales.SideAskNote()).FontSize(12.5).TextColor(p.Mute)
			})
		}
	})
}
