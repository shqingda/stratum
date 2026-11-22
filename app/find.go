package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

// find is ⌘F: the rows of the session that hold some words, one at a
// time.
type find struct {
	on         bool
	query      string
	input      ui.Handle
	hits       []int // rows of the transcript, in order
	at         int   // the hit shown
	move       bool  // scroll to it next frame
	of         string
	idxSession string
	idx        map[int]findLower
}

type findLower struct {
	textLen, detailLen, outLen int
	text, detail, output       string
}

func (a *App) openFind(c *ui.Context) {
	a.find.on, a.find.move = true, true
	a.find.input.Focus(c)
}

func (a *App) closeFind() {
	a.find = find{}
}

// hit is the transcript row the find shows, -1 with none.
func (a *App) hit() int {
	f := &a.find
	if !f.on || len(f.hits) == 0 {
		return -1
	}
	return f.hits[f.at]
}

// findIn finds the query in the rows, and opens the folded work a hit is
// in so that it shows.
func (a *App) findIn(items []item) {
	f := &a.find
	q := strings.ToLower(strings.TrimSpace(f.query))
	key := a.sessionID() + "\x00" + q
	f.hits = f.hits[:0]
	if !f.on || q == "" {
		return
	}
	sid := a.sessionID()
	if f.idx == nil || f.idxSession != sid {
		f.idx = map[int]findLower{}
		f.idxSession = sid
	}
	lower := func(e *client.Entry) findLower {
		fl, ok := f.idx[e.Key]
		if ok && fl.textLen == len(e.Text) && fl.detailLen == len(e.Detail) && fl.outLen == len(e.Output) {
			return fl
		}
		fl = findLower{
			textLen:   len(e.Text),
			detailLen: len(e.Detail),
			outLen:    len(e.Output),
			text:      strings.ToLower(e.Text),
		}
		if e.Role == client.Tool {
			fl.detail = strings.ToLower(e.Detail)
			fl.output = strings.ToLower(e.Output)
		}
		f.idx[e.Key] = fl
		return fl
	}
	has := func(e *client.Entry) bool {
		if e == nil {
			return false
		}
		fl := lower(e)
		return strings.Contains(fl.text, q) ||
			e.Role == client.Tool && (strings.Contains(fl.detail, q) || strings.Contains(fl.output, q))
	}
	any := func(es []*client.Entry) bool {
		for _, e := range es {
			if has(e) {
				return true
			}
		}
		return false
	}
	for i, it := range items {
		if has(it.e) || any(it.work) || any(it.fold) {
			f.hits = append(f.hits, i)
		}
	}
	if key != f.of {
		f.of, f.at, f.move = key, 0, true
	}
	f.at = min(f.at, max(0, len(f.hits)-1))
	if i := a.hit(); i >= 0 && f.move {
		f.move = false
		switch it := items[i]; {
		case it.work != nil:
			a.opened[fmt.Sprintf("work:%d", it.work[0].Key)] = true
		case it.fold != nil:
			a.opened["fold:"+it.fold[0].CallID] = true
		}
		a.transcriptList().ScrollTo(i, ui.Center)
	}
}

func (a *App) sessionID() string {
	if a.C.Current == nil {
		return ""
	}
	return a.C.Current.ID
}

// step moves to the next hit, or back.
func (a *App) findStep(by int) {
	f := &a.find
	if n := len(f.hits); n > 0 {
		f.at, f.move = (f.at+by+n)%n, true
	}
}

// findBar floats at the top right of the transcript.
func (a *App) findBar(c *ui.Context) {
	f := &a.find
	if !f.on {
		return
	}
	p := a.p
	bar := ui.Row(c.Key("find-bar")).Absolute().Top(sheetBar+4).Right(20).Width(312).Height(38).Padding(0, 4, 0, 12).Gap(6).Radius(10).Background(p.Card).AlignItems(ui.Center).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Scale: .98, Opacity: 0}, Duration: 160 * time.Millisecond})
	rim(bar, p)
	lift(bar, p)
	bar.Children(func() {
		sym(c, "search", 14, p.Mute)
		ui.TextInputBase(c, &f.query).Bind(&f.input).Label(locales.FindInSession()).Placeholder(locales.FindInSession()).PlaceholderColor(p.Mute).
			FontSize(13.5).TextColor(p.Fg).Grow(1).MinWidth(0).SingleLine().AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			f.query = k.Text()
			switch {
			case k.Key == ui.KeyEnter && k.Mods == 0, k.Key == ui.KeyDown && k.Mods == 0:
				a.findStep(1)
			case k.Key == ui.KeyEnter && k.Mods == ui.Shift, k.Key == ui.KeyUp && k.Mods == 0:
				a.findStep(-1)
			default:
				return false
			}
			return true
		})
		if strings.TrimSpace(f.query) != "" {
			n := fmt.Sprintf("%d/%d", min(f.at+1, len(f.hits)), len(f.hits))
			col := p.Mute
			if len(f.hits) == 0 {
				col = p.Minus
			}
			ui.Text(c, n).FontSize(12.5).TextColor(col).SingleLine().Shrink(0)
		}
		if a.iconPill(c, "find-up", "chevron-up", locales.FindPrevious()).Disabled(len(f.hits) == 0).Clicked() {
			a.findStep(-1)
		}
		if a.iconPill(c, "find-down", "chevron-down", locales.FindNext()).Disabled(len(f.hits) == 0).Clicked() {
			a.findStep(1)
		}
		if a.iconPill(c, "find-close", "x", locales.CloseFind()).Clicked() {
			a.closeFind()
		}
	})
}

var (
	mdLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdMarks = regexp.MustCompile("(\\*\\*|__|`)")
	mdHead  = regexp.MustCompile(`(?m)^(#{1,6}|>)\s?`)
	mdFence = regexp.MustCompile("(?m)^```.*\\n?")
)

// plain is a reply's markdown as the words it shows: no marks, links as
// their text, code without its fences.
func plain(md string) string {
	s := mdFence.ReplaceAllString(md, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdHead.ReplaceAllString(s, "")
	return strings.TrimSpace(mdMarks.ReplaceAllString(s, ""))
}
