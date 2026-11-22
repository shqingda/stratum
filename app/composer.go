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

// effortLabel names a reasoning effort.
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

// composer is the box the user writes in, under the transcript: prompts
// waiting for the running turn above it, the session's settings in it and
// a status line under it.
// It floats over the foot of the transcript, which fades out above it.
func (a *App) composer(c *ui.Context) {
	dock := ui.Row(c).Absolute().Left(0).Right(0).Bottom(0).Justify(ui.Center).Padding(0, 28, 10, 28)
	a.placeHero(c, dock)
	dock.Children(func() {
		ui.Column(c).Grow(1).MaxWidth(columnWidth).Children(func() { a.dock(c) })
	})
	a.dockH = dock.Bounds().H
}

// dock is the composer with the prompts queued above it and the status
// or the last error under it, at the foot of a transcript or in the middle
// of an empty session.
func (a *App) dock(c *ui.Context) {
	p := a.p
	a.takeBack()
	a.rewindMenu(c)
	a.mentionMenu(c)
	a.slashMenu(c)
	if len(a.C.Queued) > 0 {
		// The waiting prompts are a tray tucked under the composer's top.
		ui.Column(c).Margin(0, 20, -1, 20).Radius(12, 12, 0, 0).Background(p.Card).Border(.5, p.Edge).Clip().
			Transition(ui.ElementTransition{Size: true, Duration: 180 * time.Millisecond}).Children(func() { a.queue(c) })
	}
	a.box(c)
	a.liveStatus(c)
	if a.err != "" {
		// An action's error stays until dismissed or the next action, with
		// its mark, as an inline error in a Mac form does.
		ui.Row(c).Gap(6).Margin(6, 0, 0, 0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			sym(c, "alert-circle", 13, p.Minus)
			ui.Text(c, a.err).FontSize(12.5).TextColor(p.Minus).MinWidth(0).Selectable()
			if a.iconPill(c, "dismiss-error", "x", locales.Close()).Size(24, 24).Clicked() {
				a.err = ""
			}
		})
	}
}

// queue lists the prompts waiting while a turn runs.
func (a *App) queue(c *ui.Context) {
	p := a.p
	for i, q := range a.C.Queued {
		row := ui.Row(c.Key("q:"+q.ID)).Height(34).Padding(0, 6, 0, 12).Gap(8).AlignItems(ui.Center).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 12, Opacity: 0}, Exit: &ui.Motion{Y: -24, Opacity: 0}})
		if i > 0 {
			row.BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge)
		}
		row.Children(func() {
			if q.Steer {
				// It joins the running turn at its next step.
				sym(c, "arrow-up", 14, p.Live)
			} else {
				sym(c, "corner-down-right", 14, p.Mute)
			}
			// Its words, clicked, come back to the composer to change.
			words := ui.ButtonBase(c.Key("edit:" + q.ID)).Row().Grow(1).MinWidth(0).Height(26).AlignItems(ui.Center).
				Label(client.QueuedLabel(q)).Disabled(q.Images > 0)
			if q.Images == 0 {
				words.Tooltip(locales.EditQueued())
			}
			words.Children(func() {
				col := p.Fg2
				if words.Hovered() && q.Images == 0 {
					col = p.Fg
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
				ui.Text(c, locales.SteerSoon()).FontSize(12.5).TextColor(p.Live).SingleLine().Shrink(0)
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
	box := ui.Column(c).Radius(20).Border(.5, p.Edge2).Padding(12, 12, 6, 12).
		Transition(ui.ElementTransition{Colors: true, Shadows: true, Duration: 160 * time.Millisecond})
	a.glass(box, composerGlass(p))
	if a.helloShown() {
		raise(box, p)
	} else {
		lift(box, p)
	}
	rim(box, p)
	if files := box.DroppedFiles(); len(files) > 0 {
		a.attachPaths(files)
	}
	dragOver := box.FileDragOver()
	switch {
	case dragOver:
		box.Border(1.5, p.Live).Background(p.Card.Mix(p.Live, .06))
	case box.FocusWithin():
		box.Border(.5, p.Live.Alpha(.45)).Shadow(0, 0, 0, 2.5, p.Live.Alpha(.14))
	}
	svc := c.Services()
	box.Children(func() {
		if dragOver {
			ui.Row(c).Gap(6).Padding(2, 4, 6, 4).AlignItems(ui.Center).Children(func() {
				sym(c, "plus", 14, p.Live)
				ui.Text(c, locales.DropToAttach()).FontSize(12.5).FontWeight(500).TextColor(p.Live)
			})
		}
		a.attachments(c)
		a.mentionChips(c)
		if a.recall != "" {
			a.draft, a.recall, a.toEnd = a.recall, "", true
		}
		area := ui.TextAreaBase(c, &a.draft).Bind(&a.composerInput)
		if a.toEnd {
			// A draft put in whole is written on from its end.
			n := len([]rune(a.draft))
			area.SetTextSelection(n, n)
			a.toEnd = false
		}
		if pl := a.mention.place; pl > 0 {
			area.SetTextSelection(pl-1, pl-1)
			a.mention.place = 0
		}
		if _, caret := area.TextSelection(); caret != a.mention.caret {
			// The @ word is found before the composer is built.
			a.mention.caret = caret
			c.Invalidate()
		}
		area.Lines(1, 10).Grow(1).MinWidth(0).Padding(2, 2, 8, 2).Placeholder(a.composerPlaceholder()).PlaceholderColor(p.Mute).TextColor(p.Fg).
			FontSize(14).LineHeight(1.45).AutoFocus().OnTextKey(a.composerKey).OnPaste(func(text string) bool { return a.paste(svc) })
		if _, ok := a.command(); ok {
			// A draft that starts with ! runs in the terminal.
			ui.Row(c).Height(26).Gap(5).AlignItems(ui.Center).Padding(0, 2).Shrink(0).Children(func() {
				sym(c, "terminal-2", 14, p.Live)
				ui.Text(c, locales.RunsInTerminal()).FontSize(12.5).TextColor(p.Live).SingleLine()
			})
		}
		if a.noImages() {
			ui.Text(c, locales.NoImageSupport()).FontSize(12.5).TextColor(p.Mute).Margin(0, 0, 4, 2)
		}
		cur := a.C.Current
		ui.Row(c).Height(28).Gap(4).Padding(2, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			if a.iconPill(c, "attach", "plus", locales.AttachAdd()).Clicked() {
				a.choose()
			}
			if cur != nil {
				a.modelPill(c, cur.Model)
			}
			ui.Spacer(c)
			if cur != nil {
				a.modePill(c, modeName(cur.Mode))
				a.contextRing(c)
			}
			a.goButton(c)
		})
	})
}

func (a *App) composerPlaceholder() string {
	switch {
	case a.questionFirst():
		return locales.ComposerAnswerPlaceholder()
	case len(a.C.Approvals) > 0:
		return locales.ComposerDenyPlaceholder()
	case a.C.Busy:
		return locales.ComposerQueuePlaceholder()
	default:
		return locales.ComposerPlaceholder()
	}
}

// ready is a draft, attachments, or session mentions to send.
func (a *App) ready() bool {
	return strings.TrimSpace(a.draft) != "" || len(a.attached) > 0 || len(a.mentionSessions) > 0
}

func (a *App) submit() {
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

// command is the command line a draft that starts with ! runs.
func (a *App) command() (string, bool) {
	cmd, ok := strings.CutPrefix(strings.TrimSpace(a.draft), "!")
	return strings.TrimSpace(cmd), ok && len(a.attached) == 0
}

// steer sends the draft into the running turn.
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

// takeBack puts the prompts a stopped turn gave back into the composer,
// before what is being written there.
func (a *App) takeBack() {
	cur := a.C.Current
	if cur == nil || len(a.C.Returned[cur.ID]) == 0 {
		return
	}
	back := strings.Join(a.C.Returned[cur.ID], "\n\n")
	delete(a.C.Returned, cur.ID)
	a.putBack(back)
}

// putBack puts words into the composer before what it holds.
func (a *App) putBack(text string) {
	if strings.TrimSpace(a.draft) != "" {
		text += "\n\n" + a.draft
	}
	a.draft, a.toEnd = text, true
}

// iconPill is a composer control that is only a symbol.
func (a *App) iconPill(c *ui.Context, key, symbol, tip string) ui.Element {
	b := ui.ButtonBase(c.Key("pill:"+key)).Size(28, 28).Radius(14).Center().Label(tip).Tooltip(tip).KeepFocus()
	press(b)
	focusRing(b, a.p)
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, 15, a.p.Fg2) })
	return b
}

func (a *App) pill(c *ui.Context, key, symbol, label string, chevron bool) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key("pill:"+key)).Row().Height(28).Padding(0, 9).Gap(5).Radius(14).AlignItems(ui.Center).Label(label)
	press(b)
	focusRing(b, p)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		if symbol != "" {
			sym(c, symbol, 14, p.Fg2)
		}
		ui.Text(c, label).FontSize(13).TextColor(p.Fg2).SingleLine()
		if chevron {
			sym(c, "chevron-down", 11, p.Mute)
		}
	})
	return b
}

// goButton sends, or stops the running turn.
func (a *App) goButton(c *ui.Context) {
	p := a.p
	b := ui.ButtonBase(c.Key("go")).Size(28, 28).Radius(14).Center().Margin(0, 0, 0, 2).
		Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond}).KeepFocus()
	press(b)
	focusRing(b, p)
	// The arrow and the stop square cross-fade where they are, so the
	// button turns into the other rather than being swapped.
	glyph := func(stop bool, fg ui.Color) {
		ui.Box(c).Absolute().Fill().Center().Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: .6, Opacity: 0}, Exit: &ui.Motion{Scale: .6, Opacity: 0}}).
			Key(stop).Children(func() {
			if stop {
				ui.Box(c).Size(10, 10).Radius(2).Background(fg)
			} else {
				sym(c, "arrow-up", 16, fg)
			}
		})
	}
	switch {
	case a.C.Busy && !a.ready():
		b.Label(locales.Stop()).Tooltip(locales.StopEsc()).Background(p.Acc).Children(func() {
			glyph(true, p.OnAcc)
			// A ring round it goes while the turn runs, and stops while
			// the turn waits on the user.
			if len(a.C.Approvals) == 0 {
				ui.Box(c).Absolute().Left(-3).Top(-3).Size(36, 36).Children(func() { spinner(c, p, 36) })
			}
		})
		if b.Clicked() {
			a.do(a.C.Cancel())
		}
	case !a.ready() || !a.C.CanAct():
		b.Label(locales.Send()).Background(p.Chip).Children(func() { glyph(false, p.Mute) })
	default:
		b.Label(locales.Send()).Background(p.Acc).Children(func() { glyph(false, p.OnAcc) })
		if b.Clicked() {
			a.submit()
		}
	}
}

// liveStatus sits under the composer card only when connecting or a turn is running.
func (a *App) liveStatus(c *ui.Context) {
	showPhase := a.C.Phase != client.Ready
	showRun := a.C.Busy && len(a.C.Approvals) == 0 && len(a.C.Turns) > 0 && !a.C.Turns[len(a.C.Turns)-1].Done && !a.C.Turns[len(a.C.Turns)-1].Start.IsZero()
	if !showPhase && !showRun {
		return
	}
	p := a.p
	ui.Row(c).Margin(6, 8, 0, 8).Height(20).Gap(8).AlignItems(ui.Center).Children(func() {
		if showPhase {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				sym(c, "refresh", 13, p.Fg2)
				ui.Text(c, phaseText(a.C.Phase)).FontSize(12.5).TextColor(p.Fg2).SingleLine()
			})
		}
		a.runningFor(c)
	})
}

// runningFor says how long the turn has run and that Esc stops it, as
// Xcode's toolbar tells a build's progress: the time ticks each second.
func (a *App) runningFor(c *ui.Context) {
	if !a.C.Busy || len(a.C.Approvals) > 0 || len(a.C.Turns) == 0 {
		return
	}
	t := a.C.Turns[len(a.C.Turns)-1]
	if t.Done || t.Start.IsZero() {
		return
	}
	p := a.p
	ui.Spacer(c)
	ui.Text(c, span(a.now().Sub(t.Start))+" · "+locales.EscToInterrupt()).FontSize(12.5).TextColor(p.Mute).SingleLine().FontFeatures("tnum")
	c.After(time.Second)
}

// textButton is a small button that is only words.
func (a *App) textButton(c *ui.Context, key, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center).Label(label).Shrink(0)
	press(b)
	focusRing(b, p)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.Fg2).SingleLine() })
	return b
}
