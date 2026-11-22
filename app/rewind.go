package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

// rewind is the /rewind menu: pick a prompt, then what to put back.
// Claude Code offers the same three restores.
type rewind struct {
	open   bool
	step   string // prompts, or actions
	sel    int
	seq    int64
	prompt string
	bubble    bool // opened from a message, so Esc closes
	scrollSel bool // scroll selected row into view once after ↑/↓
}

type rewindPrompt struct {
	seq  int64
	text string
}

type rewindAct struct {
	action, label string
}

// rewindPrompts are the finished prompts of this session, oldest first.
func (a *App) rewindPrompts() []rewindPrompt {
	var out []rewindPrompt
	seen := map[int]bool{}
	for _, e := range a.C.Entries {
		// A message sent while the turn ran has no checkpoint of its own.
		if !a.rewindable(e) || seen[e.Turn] {
			continue
		}
		seen[e.Turn] = true
		text := e.Text
		if text == "" {
			text = locales.ImagesPlaceholder(len(e.Images))
		}
		out = append(out, rewindPrompt{a.C.Turns[e.Turn].Seq, text})
	}
	return out
}

func (a *App) rewindable(e *client.Entry) bool {
	if a.C.Busy || e == nil || e.Role != client.User || e.Typed {
		return false
	}
	if e.Turn < 0 || e.Turn >= len(a.C.Turns) || !a.C.Turns[e.Turn].Done || a.C.Turns[e.Turn].Seq <= 0 {
		return false
	}
	// The prompt is the first message of the turn. One sent while it ran
	// is not its own checkpoint.
	for _, other := range a.C.Entries {
		if other == e {
			break
		}
		if other.Role == client.User && !other.Typed && other.Turn == e.Turn && (other.Text != "" || len(other.Images) > 0) {
			return false
		}
	}
	return e.Text != "" || len(e.Images) > 0
}

// rewindActions are the restores for the prompt picked. Code restores
// show only when a later turn still has file changes.
func (a *App) rewindActions() []rewindAct {
	code := false
	for _, t := range a.C.Changes.Turns {
		if t.TurnSeq >= a.rewind.seq && len(t.Files) > 0 && !a.C.Changes.Reverted[t.TurnSeq] {
			code = true
			break
		}
	}
	var acts []rewindAct
	if code {
		acts = append(acts, rewindAct{"both", locales.RewindCodeAndChat()})
	}
	acts = append(acts, rewindAct{"conversation", locales.RewindChat()})
	if code {
		acts = append(acts, rewindAct{"code", locales.RewindCode()})
	}
	acts = append(acts, rewindAct{"summarize", locales.RewindSummarize()})
	if prompts := a.rewindPrompts(); len(prompts) > 0 && prompts[0].seq < a.rewind.seq {
		acts = append(acts, rewindAct{"summarize_before", locales.RewindSummarizeBefore()})
	}
	return acts
}

func (a *App) rewindShell(seq int64) bool {
	for _, e := range a.C.Entries {
		if e.Role != client.Tool || e.Text != "shell" {
			continue
		}
		if e.Typed && e.Seq >= seq {
			return true
		}
		if e.Turn >= 0 && e.Turn < len(a.C.Turns) && a.C.Turns[e.Turn].Seq >= seq {
			return true
		}
	}
	return false
}

func (a *App) openRewind() {
	a.rewind = rewind{open: true, step: "prompts"}
	if a.C.Current != nil && a.C.Changes.Stale && a.C.Phase == client.Ready {
		a.quiet(a.C.ListChanges())
	}
}

// rewindButton sits by a user message and opens the restores for it.
func (a *App) rewindButton(c *ui.Context, e *client.Entry, shown bool) {
	p := a.p
	seq := a.C.Turns[e.Turn].Seq
	b := ui.ButtonBase(c.Key("rewind-msg:"+strconv.FormatInt(seq, 10))).Size(26, 26).Radius(7).Center().Label(locales.RewindTo()).Tooltip(locales.RewindTo())
	press(b)
	focusRing(b, p)
	target := float32(0)
	if shown || b.Focused() || a.rewind.open && a.rewind.seq == seq {
		target = 1
	}
	b.Opacity(b.AnimateTo("shown", target, ui.Animation{Duration: 120 * time.Millisecond}))
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() { sym(c, "history", 14, p.Mute) })
	if b.Clicked() {
		text := e.Text
		if text == "" {
			text = locales.ImagesPlaceholder(len(e.Images))
		}
		a.openRewindAt(seq, text)
	}
}

func (a *App) openRewindAt(seq int64, prompt string) {
	a.openRewind()
	a.rewind.step, a.rewind.seq, a.rewind.prompt, a.rewind.bubble = "actions", seq, prompt, true
}

func (a *App) rewindCount() int {
	if !a.rewind.open {
		return 0
	}
	if a.rewind.step == "actions" {
		return len(a.rewindActions())
	}
	return len(a.rewindPrompts())
}

func (a *App) rewindPick() {
	if a.rewind.step == "actions" {
		acts := a.rewindActions()
		if len(acts) == 0 {
			return
		}
		a.rewindDo(acts[min(a.rewind.sel, len(acts)-1)].action)
		return
	}
	prompts := a.rewindPrompts()
	if len(prompts) == 0 {
		return
	}
	p := prompts[min(a.rewind.sel, len(prompts)-1)]
	a.rewind.step, a.rewind.seq, a.rewind.prompt, a.rewind.sel = "actions", p.seq, p.text, 0
}

func (a *App) rewindDo(action string) {
	if action == "both" || action == "conversation" {
		a.draft, a.toEnd = a.rewind.prompt, true
		a.drafts.kept, a.drafts.last = a.draft, a.draft
		a.drafts.since = time.Now()
		if a.now != nil {
			a.drafts.since = a.now()
		}
	}
	seq := a.rewind.seq
	a.rewind = rewind{}
	a.do(a.C.Rewind(seq, action))
}

// rewindMenu is the list over the composer.
func (a *App) rewindMenu(c *ui.Context) {
	w := &a.rewind
	if !w.open {
		return
	}
	p := a.p
	list := ui.Column(c.Key("rewind-menu")).Margin(0, 8, 6, 8).Padding(4).Radius(12).Background(p.Card).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 6, Scale: .985, Opacity: 0}, Duration: 150 * time.Millisecond})
	rim(list, p)
	lift(list, p)
	list.Children(func() {
		ui.Text(c, locales.Rewind()).FontSize(12).FontWeight(600).LetterSpacing(.18).TextColor(p.Mute).Margin(6, 10, 2, 10)
		ui.Scroll(c).MaxHeight(320).Children(func() {
			if w.step == "actions" {
				a.rewindActionRows(c)
				return
			}
			prompts := a.rewindPrompts()
			if len(prompts) == 0 {
				ui.Text(c, locales.RewindEmpty()).FontSize(13).TextColor(p.Mute).Padding(8, 10)
				return
			}
			w.sel = min(w.sel, len(prompts)-1)
			for i, pr := range prompts {
				row := ui.ButtonBase(c.Key("rewind:"+strings.TrimSpace(pr.text))).Row().Height(32).Padding(0, 10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(pr.text)
				press(row)
				focusRing(row, p)
				switch {
				case i == w.sel:
					row.Background(p.Sel)
					if w.scrollSel {
						row.ScrollIntoView()
						w.scrollSel = false
					}
				case row.Hovered():
					row.Background(p.Hover)
				}
				row.Children(func() {
					ui.Text(c, pr.text).FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
				})
				if row.Clicked() {
					a.rewind.step, a.rewind.seq, a.rewind.prompt, a.rewind.sel = "actions", pr.seq, pr.text, 0
				}
			}
		})
	})
}

func (a *App) rewindActionRows(c *ui.Context) {
	p := a.p
	w := &a.rewind
	acts := a.rewindActions()
	if len(acts) == 0 {
		return
	}
	w.sel = min(w.sel, len(acts)-1)
	ui.Text(c, w.prompt).FontSize(12.5).TextColor(p.Mute).Margin(2, 10, 4, 10).SingleLine().Ellipsis("…")
	for i, act := range acts {
		row := ui.ButtonBase(c.Key("rewind-act:"+act.action)).Row().Height(32).Padding(0, 10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(act.label)
		press(row)
		focusRing(row, p)
		switch {
		case i == w.sel:
			row.Background(p.Sel)
			if w.scrollSel {
				row.ScrollIntoView()
				w.scrollSel = false
			}
		case row.Hovered():
			row.Background(p.Hover)
		}
		row.Children(func() {
			ui.Text(c, act.label).FontSize(13).TextColor(p.Fg).SingleLine()
		})
		if row.Clicked() {
			a.rewindDo(act.action)
		}
	}
	if a.rewindShell(w.seq) && len(acts) > 1 {
		ui.Text(c, locales.RewindShell()).FontSize(12).TextColor(p.Mute).Margin(4, 10, 4, 10)
	}
}

// rewindKey moves in the menu. Other keys are held until it closes.
func (a *App) rewindKey(k ui.TextKey) bool {
	if !a.rewind.open || k.Mods != 0 {
		return false
	}
	n := a.rewindCount()
	switch k.Key {
	case ui.KeyUp:
		if n > 0 {
			a.rewind.sel = (a.rewind.sel + n - 1) % n
			a.rewind.scrollSel = true
		}
	case ui.KeyDown:
		if n > 0 {
			a.rewind.sel = (a.rewind.sel + 1) % n
			a.rewind.scrollSel = true
		}
	case ui.KeyEnter:
		a.rewindPick()
	case ui.KeyEscape:
		if a.rewind.step == "actions" && !a.rewind.bubble {
			a.rewind.step, a.rewind.sel = "prompts", 0
		} else {
			a.rewind = rewind{}
		}
	}
	return true
}
