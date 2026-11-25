package app

import (
	"strings"

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
	card := ui.Column(c.Key("side-ask")).Absolute().Top(top).Right(20).Width(400).MaxWidthPercent(92).ZIndex(18).
		Radius(18).Background(p.SurfaceContainerHigh).Clip().Label(locales.SideAsk()).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Scale: .98, Opacity: 0}, Animation: &animFast})
	elevation2(card, p)
	card.Children(func() {
		ui.Row(c).Height(42).Padding(0, 12, 0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			sym(c, "message", 15, p.OnSurfaceVariant)
			ui.Text(c, locales.SideAsk()).FontSize(14).FontWeight(600).TextColor(p.OnSurface).SingleLine()
			ui.Spacer(c)
			if a.iconButtonSized(c, "side-close", "x", locales.Close(), 14).KeyShortcuts(ui.Cmd, ui.KeyW).Clicked() {
				a.btwOpen = false
			}
		})

		ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			inp := ui.TextInputBase(c.Key("side-input"), &a.btwDraft).Bind(&a.btwInput).
				Label(locales.SideAsk()).Placeholder(locales.SideAskPlaceholder()).PlaceholderColor(p.OnSurfaceVariant).
				FontSize(13.5).TextColor(p.OnSurface).Grow(1).MinWidth(0).SingleLine().
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
			sendBtn := ui.ButtonBase(c.Key("side-send")).Size(32, 32).Radius(16).Center().Label(locales.Send())
			ready := strings.TrimSpace(a.btwDraft) != "" && a.C.Current != nil && a.C.CanAct()
			m3Focus(sendBtn, p)
			if ready {
				sendBtn.Background(p.Primary).Children(func() { sym(c, "arrow-up", 15, p.OnPrimary) })
				if sendBtn.Clicked() {
					q := a.btwDraft
					a.btwDraft = ""
					a.sendSideAsk(q)
				}
			} else {
				sendBtn.Background(p.SurfaceContainerHighest).Children(func() { sym(c, "arrow-up", 15, p.OnSurfaceVariant.Alpha(.38)) })
			}
		})

		curID := ""
		if a.C.Current != nil {
			curID = a.C.Current.ID
		}
		if a.C.SideQuestion != "" && a.C.SideSession == curID {
			ui.Box(c).FillWidth().Height(1).Background(p.OutlineVariant)
			ui.Scroll(c).MaxHeight(280).Padding(12, 16, 14, 16).Children(func() {
				ui.Text(c, a.C.SideQuestion).FontSize(13).FontWeight(600).TextColor(p.OnSurfaceVariant).Margin(0, 0, 8, 0)
				switch {
				case a.C.SidePending:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						spinner(c, p, 14)
						ui.Text(c, locales.Reading()).FontSize(13).TextColor(p.OnSurfaceVariant)
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
		}
	})
}
