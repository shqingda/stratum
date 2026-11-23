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
	a.btwOpen = true
	a.btwFocus = true
}

func (a *App) sendSideAsk(q string) {
	q = strings.TrimSpace(q)
	if q == "" || a.C.Current == nil {
		return
	}
	a.btwOpen = true
	a.do(a.C.SideAsk(q))
}

func (a *App) sideAskDialog(c *ui.Context) {
	if !a.btwOpen {
		return
	}
	p := a.p
	ui.DialogBase(c, &a.btwOpen, func(backdrop, panel ui.Element) {
		scrim := ui.RGBA(18, 19, 26, .18)
		if p.Dark {
			scrim = ui.RGBA(6, 7, 10, .45)
		}
		backdrop.Background(scrim).Justify(ui.Start).Padding(96, 16, 16, 16)
		panel.Width(560).MaxWidthPercent(100).Radius(14).Background(p.Card).Clip().Label(locales.SideAsk()).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -8, Scale: .98, Opacity: 0}, Duration: 170 * time.Millisecond})
		if p.Dark {
			panel.Shadow(0, 0, 0, .5, ui.RGBA(0, 0, 0, .8)).Shadow(0, 18, 48, -8, ui.RGBA(0, 0, 0, .6)).InsetShadow(0, .5, 0, 0, ui.RGBA(255, 255, 255, .08))
		} else {
			panel.Shadow(0, 0, 0, .5, ink.Alpha(.14)).Shadow(0, 12, 32, -8, ink.Alpha(.22))
		}
		panel.Children(func() {
			ui.Row(c).Height(44).Padding(0, 14, 0, 16).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
				sym(c, "message", 15, p.Mute)
				ui.Text(c, locales.SideAsk()).FontSize(14).FontWeight(600).TextColor(p.Fg).SingleLine()
				ui.Row(c).Height(20).Padding(0, 8).Radius(6).Background(p.Chip).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.SideAskBadge()).FontSize(11).FontWeight(500).TextColor(p.Mute).SingleLine()
				})
				ui.Spacer(c)
				if a.iconButtonSized(c, "side-close", "x", locales.Close(), 14).KeyShortcuts(ui.Cmd, ui.KeyW).Clicked() {
					a.btwOpen = false
				}
			})

			ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
				inp := ui.TextInputBase(c.Key("side-input"), &a.btwDraft).Bind(&a.btwInput).
					Label(locales.SideAsk()).Placeholder(locales.SideAskPlaceholder()).PlaceholderColor(p.Mute).
					FontSize(14).TextColor(p.Fg).Grow(1).SingleLine().AutoFocus().
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
				sendBtn := ui.ButtonBase(c.Key("side-send")).Size(28, 28).Radius(7).Center().Label(locales.Send())
				ready := strings.TrimSpace(a.btwDraft) != "" && a.C.Current != nil && a.C.CanAct()
				press(sendBtn)
				focusRing(sendBtn, p)
				if ready {
					sendBtn.Background(p.Acc).Children(func() { sym(c, "arrow-up", 14, p.OnAcc) })
					if sendBtn.Clicked() {
						q := a.btwDraft
						a.btwDraft = ""
						a.sendSideAsk(q)
					}
				} else {
					sendBtn.Background(p.Acc.Alpha(idleInk)).Children(func() { sym(c, "arrow-up", 14, p.OnAcc) })
				}
			})

			curID := ""
			if a.C.Current != nil {
				curID = a.C.Current.ID
			}
			if a.C.SideQuestion != "" && a.C.SideSession == curID {
				ui.Scroll(c).MaxHeight(360).Padding(12, 16, 14, 16).Children(func() {
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
				ui.Row(c).Padding(12, 16, 14, 16).Children(func() {
					ui.Text(c, locales.SideAskNote()).FontSize(13).TextColor(p.Mute)
				})
			}
		})
	})
}
