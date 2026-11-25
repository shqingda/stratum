package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// item is a row of the transcript: an entry, a fold of steps that only
// looked around, or an approval.
type item struct {
	e    *client.Entry
	fold []*client.Entry
	ap   *proto.Approval
	live bool
	// A finished turn shows its work folded, then its last words, then
	// what it changed: work holds the steps, done the turn's index.
	work    []*client.Entry
	done    int
	turnSeq int64 // of the turn done is, for a key that survives older turns loading
	sum     bool
	older   bool // the row that loads what came before
}

func (it item) key() any {
	switch {
	case it.older:
		return "older"
	case it.ap != nil:
		return "ask:" + it.ap.ID
	case it.fold != nil:
		return "fold:" + it.fold[0].CallID
	case it.work != nil:
		return fmt.Sprintf("work:%d", it.work[0].Key)
	case it.sum:
		return fmt.Sprintf("sum:%d", it.turnSeq)
	}
	return it.e.Key
}

// browsing are the tools that only look around: runs of them fold into
// one line.
var browsing = map[string]bool{"read_file": true, "list_dir": true, "search": true, "git_status": true, "git_diff": true}

// items are the rows the transcript shows: a finished turn's steps fold
// into one line, runs of two or more finished browsing steps fold, and a
// step waiting on an approval shows only as the approval.
func (a *App) items() []item {
	es := a.C.Entries
	live := a.liveFrom()
	var asked map[*client.Entry]bool
	if len(a.C.Approvals) > 0 {
		asked = make(map[*client.Entry]bool, len(a.C.Approvals))
		for _, ap := range a.C.Approvals {
			for i := len(es) - 1; i >= 0; i-- {
				e := es[i]
				if e.Role == client.Tool && !e.Done && e.Text == ap.Tool && !asked[e] &&
					(len(e.Args) == 0 || compact(e.Args) == compact(ap.Args)) {
					asked[e] = true
					break
				}
			}
		}
	}
	out := a.itemsBuf[:0]
	for i := 0; i < len(es); {
		e := es[i]
		if t := e.Turn; e.Role == client.User && t >= 0 && t < len(a.C.Turns) && a.C.Turns[t].Done {
			j := i + 1
			for j < len(es) && es[j].Turn == t && es[j].Role != client.User {
				j++
			}
			out = append(out, item{e: e})
			out = a.finished(out, es[i+1:j], t)
			i = j
			continue
		}
		var it item
		var ok bool
		it, ok, i = a.row(es, i, live, asked)
		if ok {
			out = append(out, it)
		}
	}
	for i := range a.C.Approvals {
		out = append(out, item{ap: &a.C.Approvals[i]})
	}
	a.itemsBuf = out
	return out
}

// row is the row that starts at es[i], a fold of browsing steps or an
// entry, and the index after it.
func (a *App) row(es []*client.Entry, i, live int, asked map[*client.Entry]bool) (item, bool, int) {
	if asked[es[i]] {
		return item{}, false, i + 1
	}
	j := i
	for j < len(es) && browse(es[j]) {
		j++
	}
	if j-i >= 2 && a.settings().Detail != "all" {
		return item{fold: es[i:j], live: live >= 0 && i >= live}, true, j
	}
	return item{e: es[i], live: live >= 0 && i >= live}, true, i + 1
}

func browse(e *client.Entry) bool {
	return e.Role == client.Tool && e.Done && !e.Failed && browsing[e.Text]
}

// finished appends the rows of a finished turn after its prompt: the steps
// folded, the last thing it said, the notes after it, and its summary.
func (a *App) finished(out []item, body []*client.Entry, t int) []item {
	tail := len(body)
	for tail > 0 && body[tail-1].Role == client.Notice {
		tail--
	}
	last := -1
	if tail > 0 && body[tail-1].Role == client.Assistant {
		last = tail - 1
	}
	work := body[:tail]
	if last >= 0 {
		work = body[:last]
	}
	if a.settings().Detail == "" && slices.ContainsFunc(work, func(e *client.Entry) bool { return e.Role == client.Tool }) {
		out = append(out, item{work: work, done: t})
	} else {
		for i := 0; i < len(work); {
			var it item
			var ok bool
			it, ok, i = a.row(work, i, -1, nil)
			if ok {
				out = append(out, it)
			}
		}
	}
	if last >= 0 {
		out = append(out, item{e: body[last]})
	}
	for _, e := range body[tail:] {
		out = append(out, item{e: e})
	}
	return append(out, item{sum: true, done: t, turnSeq: a.C.Turns[t].Seq})
}

// fold is a run of browsing steps as one line, "浏览了 4 个文件，搜索 1
// 次", that opens to the steps.
func (a *App) fold(c *ui.Context, steps []*client.Entry) {
	p := a.p
	count := map[string]int{}
	for _, e := range steps {
		count[e.Text]++
	}
	var parts []string
	add := func(n int, say func(int) string) {
		if n > 0 {
			parts = append(parts, say(n))
		}
	}
	add(count["read_file"], locales.BrowsedFiles)
	add(count["list_dir"], locales.ListedDirs)
	add(count["search"], locales.SearchedTimes)
	add(count["git_status"]+count["git_diff"], locales.GitLooks)
	key := "fold:" + steps[0].CallID
	open := a.opened[key]
	if a.inWork && len(count) == 1 && count["read_file"] > 0 {
		// In a finished turn's work, reads are a step naming the files.
		var names []string
		for _, e := range steps {
			names = append(names, e.Detail)
		}
		row := ui.ButtonBase(c.Key(key)).Row().Height(30).Padding(0, 8).Margin(0, -8).Gap(8).Radius(8).AlignItems(ui.Center).Label(locales.Browse() + " " + strings.Join(names, " ")).Expanded(open)
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, "file-search", 15, p.OnSurfaceVariant) })
			ui.Text(c, locales.Browse()).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
			ui.Text(c, strings.Join(names, " ")).Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().PreserveSpaces().Ellipsis("…").MinWidth(0)
			ui.Spacer(c)
			if row.Hovered() || row.FocusVisible() || open {
				disclosureChevron(c, key, open, 10, p.OnSurfaceVariant)
			}
		})
	} else {
		row := ui.ButtonBase(c.Key(key)).Row().Height(30).Padding(0, 8).Margin(0, -8).Gap(7).Radius(8).AlignItems(ui.Center).Label(locales.ExpandSteps()).Expanded(open)
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
		if row.Clicked() {
			a.transcriptList().HoldPlace()
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			disclosureChevron(c, key, open, 12, p.OnSurfaceVariant)
			ui.Text(c, strings.Join(parts, locales.ClauseSeparator())).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine()
			ui.Spacer(c)
		})
	}
	if open {
		ui.Column(c).Margin(2, 0, 8, 0).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 120 * time.Millisecond}).
			Children(func() {
				for _, e := range steps {
					ui.Column(c.Key(e.Key)).Children(func() { a.card(c, e) })
				}
			})
	}
}
