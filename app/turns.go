package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// work is a finished turn's steps as one line, "工作了 38 秒", that
// opens to them.
func (a *App) work(c *ui.Context, steps []*client.Entry, t int) {
	p := a.p
	key := fmt.Sprintf("work:%d", steps[0].Key)
	open := a.opened[key]
	label := locales.Worked()
	if tu := a.C.Turns[t]; !tu.Start.IsZero() && !tu.End.IsZero() {
		label += " " + span(tu.End.Sub(tu.Start))
	}
	row := ui.ButtonBase(c.Key(key)).Row().Height(26).Gap(7).AlignItems(ui.Center).Label(label).Expanded(open)
	if row.Clicked() {
		open = !open
		a.opened[key] = open
	}
	// After how long it took, what it came to: the files it changed and
	// the commands it ran, the steps worth knowing without opening it.
	files, runs := map[string]bool{}, 0
	for _, e := range steps {
		switch {
		case e.Role != client.Tool || e.Failed:
		case e.Text == "edit_file" || e.Text == "write_file":
			files[e.Detail] = true
		case e.Text == "shell":
			runs++
		}
	}
	var did []string
	if len(files) > 0 {
		did = append(did, locales.EditedFiles(len(files)))
	}
	if runs > 0 {
		did = append(did, locales.RanCommands(runs))
	}
	row.Children(func() {
		sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 13, p.Mute)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Fg2).SingleLine().Shrink(0)
		if len(did) > 0 {
			ui.Text(c, "· "+strings.Join(did, locales.ClauseSeparator())).FontSize(13.5).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
		}
		ui.Spacer(c)
	})
	if !open {
		return
	}
	a.inWork = true
	defer func() { a.inWork = false }()
	ui.Column(c).Margin(2, 0, 8, 0).Children(func() {
		for i := 0; i < len(steps); {
			var run []item
			run, i = a.row(steps, i, -1, nil)
			for _, it := range run {
				ui.Column(c.Key(it.key())).Children(func() {
					if it.fold != nil {
						a.fold(c, it.fold)
					} else {
						a.body(c, it.e)
					}
				})
			}
		}
	})
}

// span says how long something took: "38 秒", "2 分 5 秒".
func span(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 60:
		return locales.DurationSeconds(max(s, 1))
	case s < 3600 && s%60 == 0:
		return locales.DurationMinutes(s / 60)
	case s < 3600:
		return locales.DurationMinutesSeconds(s/60, s%60)
	}
	return locales.DurationHoursMinutes(s/3600, s%3600/60)
}

// tokens shortens a count of tokens: 182, 4.1k, 1.2M.
func tokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1000000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1000)) + "k"
	}
	return trimZero(fmt.Sprintf("%.1f", float64(n)/1000000)) + "M"
}

func trimZero(s string) string {
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}

// summary closes a finished turn: what it changed in files, with taking
// it back and the review.
func (a *App) summary(c *ui.Context, t int) {
	p := a.p
	tu := a.C.Turns[t]
	var ch *proto.ChangeTurn
	for i, x := range a.C.Changes.Turns {
		if x.TurnSeq == tu.Seq && tu.Seq != 0 {
			ch = &a.C.Changes.Turns[i]
		}
	}
	if ch != nil && len(ch.Files) > 0 {
		add, del := a.totals([]proto.ChangeTurn{*ch})
		reverted := a.C.Changes.Reverted[ch.TurnSeq]
		ui.Row(c).Height(40).Padding(0, 8, 0, 12).Gap(10).Radius(10).InsetShadow(0, 0, 0, .5, p.Edge2).Margin(12, 0, 6, 0).AlignItems(ui.Center).Children(func() {
			sym(c, "files", 15, p.Fg2)
			what := locales.ChangedFiles(len(ch.Files))
			if reverted {
				what = locales.UndidFiles(len(ch.Files))
			}
			ui.Text(c, what).FontSize(13.5).TextColor(p.Fg).SingleLine()
			ui.Text(c, fmt.Sprintf("+%d", add)).Font("monospace").FontSize(13).TextColor(p.Plus).Opacity(nonzero(add))
			ui.Text(c, fmt.Sprintf("−%d", del)).Font("monospace").FontSize(13).TextColor(p.Minus).Opacity(nonzero(del))
			ui.Spacer(c)
			label, icon := locales.UndoAll(), "arrow-back-up"
			if reverted {
				label, icon = locales.Redo(), "arrow-forward-up"
			}
			undo := ui.ButtonBase(c.Key(fmt.Sprintf("undo:%d", ch.TurnSeq))).Row().Height(28).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).Label(label).Disabled(a.C.Busy)
			if undo.Hovered() {
				undo.Background(p.Hover)
			}
			if a.C.Busy {
				undo.Tooltip(locales.CantRestoreWhileRunning())
			}
			undo.Children(func() {
				sym(c, icon, 15, p.Fg2)
				ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
			})
			if undo.Clicked() {
				a.do(a.C.Revert(ch.TurnSeq, "", reverted))
			}
			if a.button(c, fmt.Sprintf("review:%d", ch.TurnSeq), locales.ViewChanges(), "", false).Clicked() {
				a.rv.open, a.rv.tab, a.rv.uncommitted, a.rv.branch, a.rv.turn = true, "", false, false, ch.TurnSeq
				key := client.DiffKey(ch.TurnSeq, ch.Files[0].Path)
				a.rv.sel, a.rv.jump = key, key
			}
		})
	}
}

func (a *App) modelInfo() (proto.Model, bool) {
	cur := a.C.Current
	if cur == nil || cur.Model == "" {
		return proto.Model{}, false
	}
	for _, m := range a.C.Models {
		if m.ID == cur.Model {
			return m, true
		}
	}
	provider, _, _ := strings.Cut(cur.Account, "/")
	if m, ok := llm.FindModel(provider, cur.Model); ok {
		return proto.Model{
			ID: m.ID, Name: m.Name, Context: m.Context, Reasoning: m.Reasoning, Images: m.Images,
			Input: m.Price.Input, Output: m.Price.Output, CacheRead: m.Price.CacheRead, CacheWrite: m.Price.CacheWrite,
		}, true
	}
	return proto.Model{}, false
}

func (a *App) turnCost(u llm.Usage) string {
	m, ok := a.modelInfo()
	if !ok {
		return ""
	}
	return formatCost(costOf(m, u))
}

func costOf(m proto.Model, u llm.Usage) float64 {
	fresh := max(0, u.Input-u.CacheRead-u.CacheWrite)
	return (float64(fresh)*m.Input + float64(u.Output)*m.Output +
		float64(u.CacheRead)*m.CacheRead + float64(u.CacheWrite)*m.CacheWrite) / 1e6
}

func formatCost(d float64) string {
	if d <= 0 {
		return ""
	}
	if d < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", d)
}

// editLine is an edit as one line, "编辑 add.go +1 −1", and reports
// whether its card shows under it.
func (a *App) editLine(c *ui.Context, e *client.Entry, path, oldText, newText string) bool {
	p := a.p
	if a.stats == nil {
		a.stats = map[string][2]int{}
	}
	st, ok := a.stats[e.CallID]
	if !ok {
		lines, _ := lineDiff(oldText, newText)
		for _, l := range lines {
			switch l.sign {
			case "+":
				st[0]++
			case "−":
				st[1]++
			}
		}
		a.stats[e.CallID] = st
	}
	t, _ := toolFor(e.Text)
	key := "card:" + e.CallID
	open := a.opened[key]
	row := ui.ButtonBase(c.Key("line:" + e.CallID)).Row().Height(25).Gap(8).AlignItems(ui.Center).Label(t.verb + " " + path).Expanded(open)
	if row.Clicked() {
		open = !open
		a.opened[key] = open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 15, p.Mute) })
		ui.Text(c, t.verb).FontSize(13.5).TextColor(p.Fg2).SingleLine().Shrink(0)
		ui.Text(c, path).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Text(c, fmt.Sprintf("+%d", st[0])).Font("monospace").FontSize(13).TextColor(p.Plus).Shrink(0).Opacity(nonzero(st[0]))
		ui.Text(c, fmt.Sprintf("−%d", st[1])).Font("monospace").FontSize(13).TextColor(p.Minus).Shrink(0).Opacity(nonzero(st[1]))
		ui.Spacer(c)
		if row.Hovered() {
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.Mute)
		}
	})
	return open
}

// thought is the model's reasoning: "思考中" while it streams, then a
// line, "思考了 6 秒", that opens to it.
func (a *App) thought(c *ui.Context, e *client.Entry) {
	p := a.p
	if !e.Done {
		a.running(c)
		return
	}
	label := locales.Thinking()
	if e.Took > 0 {
		label = locales.ThoughtFor(span(e.Took))
	}
	key := fmt.Sprintf("thought:%d", e.Key)
	open, set := a.opened[key]
	if !set {
		open = a.settings().Detail != ""
	}
	row := ui.ButtonBase(c.Key(key)).Row().Height(26).Gap(7).AlignItems(ui.Center).Label(label).Expanded(open)
	if a.inWork {
		row.Height(25).Gap(8)
	}
	if row.Clicked() {
		open = !open
		a.opened[key] = open
	}
	row.Children(func() {
		if a.inWork {
			// In a finished turn's work it is a step like the others.
			ui.Box(c).Width(16).Center().Children(func() { sym(c, "bulb", 15, p.Mute) })
			ui.Text(c, label).FontSize(13.5).TextColor(p.Fg2).SingleLine()
		} else {
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 13, p.Mute)
			ui.Text(c, label).FontSize(13.5).TextColor(p.Mute).SingleLine()
		}
		ui.Spacer(c)
	})
	if open {
		ui.Text(c, strings.TrimSpace(e.Text)).FontSize(13.5).LineHeight(1.55).TextColor(p.Mute).Padding(2, 0, 8, 20).Selectable()
	}
}

// rings are the context rings drawn, by percent and appearance.
var rings = map[[2]int]*ui.SVG{}

// contextRing says how much of the model's context the session fills,
// as of the latest request this window saw.
func (a *App) contextRing(c *ui.Context) {
	p := a.p
	if a.C.Current == nil || a.C.LastInput == 0 {
		return
	}
	m, ok := a.modelInfo()
	if !ok || m.Context == 0 {
		return
	}
	window := m.Context
	pct := min(100, (a.C.LastInput*100+window/2)/window)
	isDark := 0
	if p.Dark {
		isDark = 1
	}
	key := [2]int{pct, isDark}
	svg, ok := rings[key]
	if !ok {
		hex := func(col ui.Color) string {
			return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", col.R, col.G, col.B, float64(col.A)/255)
		}
		arc := p.Fg2
		if pct >= 85 {
			arc = p.Minus
		}
		// A circle of radius 6 on a 16 grid, its arc from the top, clockwise.
		const circ = 2 * 3.14159265 * 6
		doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none">`+
			`<circle cx="8" cy="8" r="6" stroke="%s" stroke-width="1.6"/>`+
			`<circle cx="8" cy="8" r="6" stroke="%s" stroke-width="1.6" stroke-linecap="round" stroke-dasharray="%.2f %.2f" transform="rotate(-90 8 8)"/></svg>`,
			hex(p.Edge2), hex(arc), circ*float64(pct)/100, circ)
		svg = ui.MustParseSVG([]byte(doc))
		rings[key] = svg
	}
	tip := fmt.Sprintf("%d%% · %s", pct, locales.ContextUsed(tokens(a.C.LastInput), tokens(window)))
	if total := a.turnCost(a.C.Usage); total != "" {
		tip += " · " + total
	}
	ui.Row(c).Height(28).Gap(5).Padding(0, 4).AlignItems(ui.Center).Tooltip(tip).Children(func() {
		ui.Image(c, svg).Size(14, 14)
		if pct >= 50 {
			ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).TextColor(p.Mute).SingleLine()
		}
	})
}
