package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func effortLabel(effort string) string {
	switch effort {
	case "minimal":
		return locales.EffortMinimal()
	case "low":
		return locales.EffortLow()
	case "medium":
		return locales.EffortMedium()
	case "high":
		return locales.EffortHigh()
	case "xhigh":
		return locales.EffortXhigh()
	}
	return locales.EffortDefault()
}

// composer is a docked Material Design 3 Tonal container below the transcript
// (centered on the empty canvas when a workspace has nothing to show yet).
func (a *App) composer(c *ui.Context) {
	dock := ui.Row(c.Key("dock")).Shrink(0).Justify(ui.Center).Padding(4, 24, 14, 24)
	a.placeHero(c, dock)
	dock.Children(func() {
		ui.Column(c).Grow(1).MaxWidth(columnWidth).Children(func() { a.dock(c) })
	})
	if h := dock.Bounds().H; h > 0 {
		a.side.dockH = h
	}
}

func (a *App) dock(c *ui.Context) {
	p := a.p
	a.takeBack()
	a.rewindMenu(c)
	a.mentionMenu(c)
	a.slashMenu(c)
	if len(a.C.Queued) > 0 {
		ui.Column(c).Margin(0, 18, -1, 18).Radius(16, 16, 0, 0).Background(p.SurfaceContainer).
			Clip().
			Transition(ui.ElementTransition{Size: true, Duration: 180 * time.Millisecond, Ease: easeStandard}).
			Children(func() { a.queue(c) })
	}
	if a.err != "" {
		ui.Row(c.Key("composer-err")).Gap(6).Margin(0, 0, 8, 0).Padding(6, 12).Radius(10).
			Background(p.ErrorContainer).Justify(ui.Center).AlignItems(ui.Center).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 4, Opacity: 0}, Exit: &ui.Motion{Opacity: 0}, Duration: 160 * time.Millisecond}).
			Children(func() {
				sym(c, "alert-circle", 14, p.OnErrorContainer)
				ui.Text(c, a.err).FontSize(12.5).FontWeight(500).TextColor(p.OnErrorContainer).MinWidth(0).Selectable()
				if a.iconPill(c, "dismiss-error", "x", locales.Close()).Size(24, 24).Clicked() {
					a.err = ""
				}
			})
	}
	a.box(c)
}

func (a *App) queue(c *ui.Context) {
	p := a.p
	for i, q := range a.C.Queued {
		row := ui.Row(c.Key("q:"+q.ID)).Height(36).Padding(0, 6, 0, 12).Gap(8).AlignItems(ui.Center).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 10, Opacity: 0}, Exit: &ui.Motion{Y: -18, Opacity: 0}})
		if i > 0 {
			row.BorderWidth(1, 0, 0, 0).BorderColor(p.OutlineVariant)
		}
		row.Children(func() {
			if q.Steer {
				sym(c, "arrow-up", 14, p.Primary)
			} else {
				sym(c, "corner-down-right", 14, p.OnSurfaceVariant)
			}
			words := ui.ButtonBase(c.Key("edit:" + q.ID)).Row().Grow(1).MinWidth(0).Height(26).AlignItems(ui.Center).Justify(ui.Start).
				Label(client.QueuedLabel(q)).Disabled(q.Images > 0)
			if q.Images == 0 {
				words.Tooltip(locales.EditQueued())
			}
			words.Children(func() {
				col := p.OnSurfaceVariant
				if words.Hovered() && q.Images == 0 {
					col = p.OnSurface
				}
				ui.Text(c, client.QueuedLabel(q)).FontSize(13.5).TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
			})
			if words.Clicked() {
				cmds, err := a.C.Unqueue(q.ID)
				if a.do(cmds, err); err == nil {
					a.putBack(q.Text)
				}
			}
			if q.Steer {
				ui.Text(c, locales.SteerSoon()).FontSize(12).FontWeight(600).TextColor(p.Primary).SingleLine().Shrink(0)
			} else if a.textButton(c, "now:"+q.ID, locales.SendNow()).Clicked() {
				a.do(a.C.SendNow(q.ID))
			}
			if a.iconPill(c, "unq:"+q.ID, "x", locales.Unqueue()).Clicked() {
				a.do(a.C.Unqueue(q.ID))
			}
		})
	}
}

func (a *App) box(c *ui.Context) {
	p := a.p
	dockBg := p.SurfaceContainerLow
	if p.Dark {
		dockBg = p.SurfaceContainerHigh
	}
	box := ui.Column(c).Radius(22).Background(dockBg).
		Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
	if files := box.DroppedFiles(); len(files) > 0 {
		a.attachPaths(files)
	}
	dragOver := box.FileDragOver()
	if dragOver {
		box.InsetShadow(0, 0, 0, 1.5, p.Primary.Alpha(.55))
	}
	svc := c.Services()
	box.Children(func() {
		a.attachedRow(c)
		ui.Column(c).Padding(14, 16, 12, 16).Children(func() { a.words(c, dragOver, svc) })
	})
}

func (a *App) attachedRow(c *ui.Context) {
	p := a.p
	has := len(a.attached) > 0 || len(a.mentionSessions) > 0
	ui.Column(c.Key("attached")).Clip().
		Transition(ui.ElementTransition{Size: true, Animation: &animStandard}).
		Children(func() {
			if !has {
				return
			}
			ui.Row(c).Wrap().Gap(6).Padding(10, 16, 10, 16).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(p.OutlineVariant.Alpha(.20)).Children(func() {
				ui.Text(c, locales.Attached()).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine().Margin(0, 2, 0, 0)
				a.sessionTokens(c)
				a.attachmentTokens(c)
			})
		})
}

// token renders an M3 InputChip for an attached file or referenced session.
func (a *App) token(c *ui.Context, key, icon string, thumb ui.ImageSource, label string) ui.Element {
	p := a.p
	var rm ui.Element
	ui.Row(c.Key(key)).Height(26).Padding(0, 2, 0, 8).Gap(5).Radius(10).
		Background(p.SurfaceContainerHighest).AlignItems(ui.Center).MaxWidth(240).Children(func() {
		switch {
		case thumb != nil:
			ui.Box(c).Size(18, 18).Radius(4).Clip().Shrink(0).Children(func() {
				ui.Image(c, thumb).Size(18, 18).Fit(ui.Cover)
			})
		case icon != "":
			sym(c, icon, 13, p.OnSurfaceVariant)
		}
		ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		rm = ui.ButtonBase(c.Key(key+":x")).Size(22, 22).Radius(6).Center().KeepFocus().Label(locales.AttachRemove(label))
		m3Focus(rm, p)
		m3State(rm, p, ui.Transparent, false)
		rm.Children(func() { sym(c, "x", 11, p.OnSurfaceVariant) })
	})
	return rm
}

func (a *App) words(c *ui.Context, dragOver bool, svc ui.Services) {
	p := a.p
	if dragOver {
		ui.Row(c).Gap(6).Padding(2, 4, 8, 4).AlignItems(ui.Center).Children(func() {
			sym(c, "plus", 15, p.Primary)
			ui.Text(c, locales.DropToAttach()).FontSize(13).FontWeight(600).TextColor(p.Primary)
		})
	}
	if a.recall != "" {
		a.draft, a.recall, a.toEnd = a.recall, "", true
	}
	sug := a.activeSuggestion()
	ui.Row(c).AlignItems(ui.Start).Gap(8).Children(func() {
		area := ui.TextAreaBase(c, &a.draft).Bind(&a.composerInput)
		a.hero.words = area.LayoutBounds()
		if a.toEnd {
			n := len([]rune(a.draft))
			area.SetTextSelection(n, n)
			a.toEnd = false
		}
		if pl := a.mention.place; pl > 0 {
			area.SetTextSelection(pl-1, pl-1)
			a.mention.place = 0
		}
		if _, caret := area.TextSelection(); caret != a.mention.caret {
			a.mention.caret = caret
			c.Invalidate()
		}
		area.Lines(1, 10).Grow(1).MinWidth(0).Padding(2, 2, 10, 2).Placeholder(a.composerPlaceholder()).PlaceholderColor(p.OnSurfaceVariant).TextColor(p.OnSurface).
			FontSize(14).LineHeight(1.5).AutoFocus().OnTextKey(a.composerKey).OnPaste(func(text string) bool { return a.paste(svc) }).
			TextRanges(a.draftMarks()...)
		if sug != "" {
			sb := ui.ButtonBase(c.Key("accept-sug")).Height(24).Padding(0, 8).Margin(2, 0, 0, 0).Radius(8).
				Background(p.SurfaceContainerHigh).Center().Shrink(0).
				Label(locales.AcceptSuggestion()).Tooltip(locales.AcceptSuggestion())
			m3Focus(sb, p)
			m3State(sb, p, p.SurfaceContainerHigh, false)
			sb.Children(func() {
				ui.Text(c, "⇥ Tab").FontSize(11.5).FontWeight(600).TextColor(p.OnSurfaceVariant).SingleLine()
			})
			if sb.Clicked() {
				a.draft, a.toEnd, a.C.Suggestion = sug, true, ""
				a.composerInput.Focus(c)
			}
		}
	})
	if _, ok := a.command(); ok {
		ui.Row(c).Height(26).Gap(6).AlignItems(ui.Center).Padding(0, 2).Shrink(0).Children(func() {
			sym(c, "terminal-2", 14, p.Primary)
			ui.Text(c, locales.RunsInTerminal()).FontSize(12.5).FontWeight(500).TextColor(p.Primary).SingleLine()
		})
	}
	if a.noImages() {
		ui.Text(c, locales.NoImageSupport()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Margin(0, 0, 4, 2)
	}
	cur := a.C.Current
	ui.Row(c).Height(36).Gap(6).Padding(4, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		if a.iconPill(c, "attach", "plus", locales.AttachAdd()).Clicked() {
			a.choose()
		}
		if cur != nil {
			a.agentPill(c)
			a.modelPill(c, cur.Model)
		}
		ui.Spacer(c)
		if cur != nil {
			a.modePill(c, modeName(cur.Mode))
			a.contextRing(c)
		}
		a.goButton(c)
	})
}

func (a *App) activeSuggestion() string {
	if a.settings().NoSuggestions || a.C.Current == nil || a.C.Busy || len(a.C.Approvals) > 0 {
		return ""
	}
	if strings.TrimSpace(a.draft) != "" || len(a.attached) > 0 || len(a.mentionSessions) > 0 {
		return ""
	}
	return strings.TrimSpace(a.C.Suggestion)
}

func (a *App) composerPlaceholder() string {
	switch {
	case a.questionFirst():
		return locales.ComposerAnswerPlaceholder()
	case len(a.C.Approvals) > 0:
		return locales.ComposerDenyPlaceholder()
	case a.C.Busy:
		return locales.ComposerQueuePlaceholder()
	case a.activeSuggestion() != "":
		return a.activeSuggestion()
	default:
		return locales.ComposerPlaceholder()
	}
}

func (a *App) ready() bool {
	return strings.TrimSpace(a.draft) != "" || len(a.attached) > 0 || len(a.mentionSessions) > 0
}

func (a *App) submit() {
	trimmed := strings.TrimSpace(a.draft)
	if trimmed == "/btw" || strings.HasPrefix(trimmed, "/btw ") {
		q := strings.TrimSpace(strings.TrimPrefix(trimmed, "/btw"))
		a.draft = ""
		if q == "" {
			a.openSideAsk()
		} else {
			a.sendSideAsk(q)
		}
		return
	}
	if cmd, ok := a.command(); ok {
		if a.C.Busy {
			a.err = locales.ShellWhileBusy()
			return
		}
		cmds, err := a.C.Shell(cmd)
		if a.do(cmds, err); err == nil {
			a.draft = ""
		}
		return
	}
	a.send1(a.C.Prompt)
}

func (a *App) command() (string, bool) {
	cmd, ok := strings.CutPrefix(strings.TrimSpace(a.draft), "!")
	return strings.TrimSpace(cmd), ok && len(a.attached) == 0
}

func (a *App) steer() { a.send1(a.C.Steer) }

func (a *App) send1(send func(string, ...llm.Image) ([]proto.Command, error)) {
	if !a.ready() {
		return
	}
	text, images := a.prompt()
	cmds, err := send(text, images...)
	a.do(cmds, err)
	if err == nil {
		a.draft, a.attached, a.mentionSessions = "", nil, nil
	}
}

func (a *App) takeBack() {
	cur := a.C.Current
	if cur == nil || len(a.C.Returned[cur.ID]) == 0 {
		return
	}
	back := strings.Join(a.C.Returned[cur.ID], "\n\n")
	delete(a.C.Returned, cur.ID)
	a.putBack(back)
}

func (a *App) putBack(text string) {
	if strings.TrimSpace(a.draft) != "" {
		text += "\n\n" + a.draft
	}
	a.draft, a.toEnd = text, true
}

func (a *App) iconPill(c *ui.Context, key, symbol, tip string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key("pill:"+key)).Size(32, 32).Radius(10).Center().Label(tip).Tooltip(tip).KeepFocus()
	m3Focus(b, p)
	m3State(b, p, ui.Transparent, false)
	b.Children(func() { sym(c, symbol, 18, p.OnSurfaceVariant) })
	return b
}

func (a *App) pill(c *ui.Context, key, symbol, label string, chevron bool) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key("pill:"+key)).Row().Height(30).Padding(0, 10).Gap(5).Radius(10).
		AlignItems(ui.Center).Label(label)
	m3Focus(b, p)
	m3State(b, p, ui.Transparent, false)
	b.Children(func() {
		if symbol != "" {
			sym(c, symbol, 15, p.OnSurfaceVariant)
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine()
		if chevron {
			sym(c, "chevron-down", 12, p.OnSurfaceVariant)
		}
	})
	return b
}

// goButton renders a 36dp M3 Filled IconButton for sending or stopping a turn.
func (a *App) goButton(c *ui.Context) {
	p := a.p
	b := ui.ButtonBase(c.Key("go")).Size(36, 36).Radius(18).Center().Margin(0, 0, 0, 4).
		Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond}).KeepFocus()
	m3Focus(b, p)
	glyph := func(stop bool, fg ui.Color) {
		ui.Box(c).Absolute().Fill().Center().Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .6, Opacity: 0}, Exit: &ui.Motion{Scale: .6, Opacity: 0}}).
			Key(stop).Children(func() {
			if stop {
				ui.Box(c).Size(12, 12).Radius(3).Background(fg)
			} else {
				sym(c, "arrow-up", 19, fg)
			}
		})
	}
	switch {
	case a.C.Busy && !a.ready():
		bg := p.ErrorContainer
		if b.Pressed() {
			bg = bg.MixOklab(p.OnErrorContainer, .12)
		} else if b.Hovered() {
			bg = bg.MixOklab(p.OnErrorContainer, .08)
		}
		b.Label(locales.Stop()).Tooltip(locales.StopEsc()).Background(bg).Children(func() {
			glyph(true, p.OnErrorContainer)
			if len(a.C.Approvals) == 0 {
				ui.Box(c).Absolute().Left(-2).Top(-2).Size(40, 40).Children(func() { spinner(c, p, 40) })
			}
		})
		if b.Clicked() {
			a.do(a.C.Cancel())
		}
	case !a.ready() || !a.C.CanAct():
		b.Label(locales.Send()).Background(p.SurfaceContainerHighest).Children(func() {
			glyph(false, p.OnSurfaceVariant.Alpha(.55))
		})
	default:
		bg := p.Primary
		if b.Pressed() {
			bg = bg.MixOklab(p.OnPrimary, .14)
		} else if b.Hovered() {
			bg = bg.MixOklab(p.OnPrimary, .08)
		}
		b.Label(locales.Send()).Background(bg).Children(func() { glyph(false, p.OnPrimary) })
		if b.Clicked() {
			a.submit()
		}
	}
}

func (a *App) textButton(c *ui.Context, key, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(26).Padding(0, 10).Radius(8).AlignItems(ui.Center).Label(label).Shrink(0)
	m3Focus(b, p)
	m3State(b, p, ui.Transparent, false)
	b.Children(func() { ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.Primary).SingleLine() })
	return b
}
