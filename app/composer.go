package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
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
	seen := map[string]int{}
	for _, q := range a.C.Queued {
		seen[q]++ // the same words queued twice are two rows
		ui.Row(c.Key(struct {
			q string
			n int
		}{q, seen[q]})).Height(34).Padding(0, 6, 0, 12).Margin(0, 20, -1, 20).Gap(8).AlignItems(ui.Center).
			Radius(12, 12, 0, 0).Background(p.Card).Border(.5, p.Edge).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 12, Opacity: 0}, Exit: &ui.Motion{Y: -24, Opacity: 0}}).Children(func() {
			sym(c, "corner-down-right", 14, p.Mute)
			ui.Text(c, q).FontSize(13.5).TextColor(p.Fg2).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
	}
	a.box(c)
	switch {
	case a.err != "":
		ui.Text(c, a.err).FontSize(12.5).TextColor(p.Minus).Margin(8, 0, 0, 0).Center()
	case !a.helloShown():
		a.status(c)
	}
}

func (a *App) box(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	box := ui.Column(c).Radius(18).Background(p.Card).Padding(12, 10, 8, 16)
	lift(box, p)
	if files := box.DroppedFiles(); len(files) > 0 {
		a.attachPaths(files)
	}
	if box.FileDragOver() {
		box.Border(1, p.Fg2)
	}
	svc := c.Services()
	box.Children(func() {
		a.attachments(c)
		ph := locales.ComposerPlaceholder()
		if a.C.Busy {
			ph = locales.ComposerQueuePlaceholder()
		}
		if a.questionFirst() {
			ph = locales.ComposerAnswerPlaceholder()
		}
		ui.TextAreaBase(c, &a.draft).Lines(2, 10).Placeholder(ph).PlaceholderColor(p.Mute).TextColor(p.Fg).
			LineHeight(1.45).AutoFocus().OnTextKey(a.composerKey).OnPaste(func(text string) bool { return a.paste(svc) })
		if a.noImages() {
			ui.Text(c, locales.NoImageSupport()).FontSize(12.5).TextColor(p.Mute).Margin(4, 0, 0, 0)
		}
		ui.Row(c).Gap(2).Margin(6, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			if a.iconPill(c, "attach", "plus", locales.AttachAdd()).Clicked() {
				a.choose()
			}
			if cur != nil {
				a.modelPill(c, cur.Model)
				a.effortPill(c, effortLabel(cur.Effort))
				a.modePill(c, modeName(cur.Mode))
			}
			ui.Spacer(c)
			a.contextRing(c)
			a.goButton(c)
		})
	})
}

// ready is a draft or attachments to send.
func (a *App) ready() bool {
	return strings.TrimSpace(a.draft) != "" || len(a.attached) > 0
}

func (a *App) submit() {
	if !a.ready() {
		return
	}
	text, images := a.prompt()
	cmds, err := a.C.Prompt(text, images...)
	a.do(cmds, err)
	if err == nil {
		a.draft, a.attached = "", nil
	}
}

// iconPill is a composer control that is only a symbol.
func (a *App) iconPill(c *ui.Context, key, symbol, tip string) ui.Element {
	b := ui.ButtonBase(c.Key("pill:"+key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(a.p.Hover)
	}
	b.Children(func() { sym(c, symbol, 16, a.p.Fg2) })
	return b
}

func (a *App) pill(c *ui.Context, key, symbol, label string, chevron bool) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key("pill:"+key)).Row().Height(28).Padding(0, 8).Gap(5).Radius(7).AlignItems(ui.Center).Label(label)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		if symbol != "" {
			sym(c, symbol, 15, p.Fg2)
		}
		ui.Text(c, label).FontSize(13.5).TextColor(p.Fg2).SingleLine()
		if chevron {
			sym(c, "chevron-down", 12, p.Mute)
		}
	})
	return b
}

// goButton sends, or stops the running turn.
func (a *App) goButton(c *ui.Context) {
	p := a.p
	b := ui.ButtonBase(c.Key("go")).Size(30, 30).Radius(15).Center().Margin(0, 0, 0, 4).
		Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
	press(b)
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
		b.Label(locales.Stop()).Background(p.Acc).Children(func() {
			glyph(true, p.OnAcc)
			// A ring round it goes while the turn runs.
			ui.Box(c).Absolute().Left(-3).Top(-3).Size(36, 36).Children(func() { spinner(c, p, 36) })
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

func (a *App) status(c *ui.Context) {
	p := a.p
	ui.Row(c).Gap(14).Justify(ui.Center).Margin(8, 0, 0, 0).Children(func() {
		item := func(symbol, label string) {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				sym(c, symbol, 13, p.Mute)
				ui.Text(c, label).FontSize(12.5).TextColor(p.Mute).SingleLine()
			})
		}
		item("device-laptop", locales.Local())
		if b := a.C.Workspace.Branch; b != "" {
			item("git-branch", b)
		}
		if a.C.Phase != client.Ready {
			item("refresh", phaseText(a.C.Phase))
		}
		item("lock", locales.SandboxOn())
	})
}
