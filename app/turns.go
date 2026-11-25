package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// work renders a finished turn's folded steps as a FlClash-style tonal expansion card
// with discrete stat pills instead of middle-dot joined metadata.
func (a *App) work(c *ui.Context, steps []*client.Entry, t int) {
	p := a.p
	key := fmt.Sprintf("work:%d", steps[0].Key)
	open := a.opened[key]
	label := locales.Worked()
	if tu := a.C.Turns[t]; !tu.Start.IsZero() && !tu.End.IsZero() {
		label += " " + span(tu.End.Sub(tu.Start))
	}
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

	ui.Column(c.Key("work-card:"+key)).Margin(4, 0, 8, 0).Radius(16).Background(p.SurfaceContainerLow).
		Clip().Children(func() {
		row := ui.ButtonBase(c.Key(key)).Row().Height(36).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			Label(label).Expanded(open)
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
		if row.Clicked() {
			open = !open
			a.opened[key] = open
		}
		row.Children(func() {
			disclosureChevron(c, key, open, 12, p.OnSurfaceVariant)
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(p.OnSurface).SingleLine().Shrink(0)
			if len(files) > 0 {
				ui.Text(c, "·  "+locales.TurnFilesEdited(len(files))).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine()
			}
			if runs > 0 {
				ui.Text(c, "·  "+locales.TurnCommandsRan(runs)).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine()
			}
			ui.Spacer(c)
		})
		if !open {
			return
		}
		a.inWork = true
		defer func() { a.inWork = false }()
		ui.Column(c.Key("work-body:"+key)).Padding(4, 12, 10, 12).BorderWidth(1, 0, 0, 0).BorderColor(p.OutlineVariant.Alpha(.20)).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 140 * time.Millisecond}).
			Children(func() {
				for i := 0; i < len(steps); {
					var it item
					var ok bool
					it, ok, i = a.row(steps, i, -1, nil)
					if !ok {
						continue
					}
					ui.Column(c.Key(it.key())).Children(func() {
						if it.fold != nil {
							a.fold(c, it.fold)
						} else {
							a.body(c, it.e)
						}
					})
				}
			})
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

// summary closes a finished turn with an M3 tonal file-changes action bar.
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
		ui.Row(c).MinHeight(44).Padding(6, 8, 6, 14).Gap(8).Radius(16).
			Background(p.SurfaceContainerLow).
			Margin(8, 0, 6, 0).AlignItems(ui.Center).Children(func() {
			sym(c, "files", 16, p.Primary)
			what := locales.ChangedFiles(len(ch.Files))
			if reverted {
				what = locales.UndidFiles(len(ch.Files))
			}
			ui.Text(c, what).FontSize(13.5).FontWeight(500).TextColor(p.OnSurface).SingleLine()
			if add > 0 {
				ui.Text(c, fmt.Sprintf("+%d", add)).Font("monospace").FontSize(12.5).FontWeight(600).TextColor(p.Plus)
			}
			if del > 0 {
				ui.Text(c, fmt.Sprintf("−%d", del)).Font("monospace").FontSize(12.5).FontWeight(600).TextColor(p.Minus)
			}
			ui.Spacer(c)
			label, icon := locales.UndoAll(), "arrow-back-up"
			if reverted {
				label, icon = locales.Redo(), "arrow-forward-up"
			}
			undo := ui.ButtonBase(c.Key(fmt.Sprintf("undo:%d", ch.TurnSeq))).Row().Height(30).Padding(0, 10).Gap(6).Radius(8).
				AlignItems(ui.Center).Label(label).Disabled(a.C.Busy)
			m3Focus(undo, p)
			m3State(undo, p, ui.Transparent, false)
			if a.C.Busy {
				undo.Tooltip(locales.CantRestoreWhileRunning())
			}
			undo.Children(func() {
				sym(c, icon, 14, p.OnSurfaceVariant)
				ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine()
			})
			if undo.Clicked() {
				a.do(a.C.Revert(ch.TurnSeq, "", reverted))
			}
			revBtn := ui.ButtonBase(c.Key(fmt.Sprintf("review:%d", ch.TurnSeq))).Row().Height(30).Padding(0, 12).Gap(6).Radius(8).
				AlignItems(ui.Center).Label(locales.ViewChanges())
			m3Focus(revBtn, p)
			m3State(revBtn, p, p.SurfaceContainerHigh, false)
			revBtn.Children(func() {
				ui.Text(c, locales.ViewChanges()).FontSize(12.5).FontWeight(500).TextColor(p.OnSurface).SingleLine()
			})
			if revBtn.Clicked() {
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
	row := ui.ButtonBase(c.Key("line:" + e.CallID)).Row().Height(28).Padding(0, 6).Margin(0, -6).Gap(8).Radius(8).
		AlignItems(ui.Center).Label(t.verb + " " + path).Expanded(open)
	m3Focus(row, p)
	m3State(row, p, ui.Transparent, false)
	if row.Clicked() {
		open = !open
		a.opened[key] = open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, t.symbol, 15, p.OnSurfaceVariant) })
		ui.Text(c, t.verb).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
		ui.Text(c, path).Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Text(c, fmt.Sprintf("+%d", st[0])).Font("monospace").FontSize(12.5).TextColor(p.Plus).Shrink(0).Opacity(nonzero(st[0]))
		ui.Text(c, fmt.Sprintf("−%d", st[1])).Font("monospace").FontSize(12.5).TextColor(p.Minus).Shrink(0).Opacity(nonzero(st[1]))
		ui.Spacer(c)
		if row.Hovered() || row.FocusVisible() || open {
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.OnSurfaceVariant)
		}
	})
	return open
}

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
	row := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 6).Margin(0, -6).Gap(7).Radius(8).AlignItems(ui.Center).Label(label).Expanded(open)
	if a.inWork {
		row.Height(26).Gap(8)
	}
	m3Focus(row, p)
	m3State(row, p, ui.Transparent, false)
	if row.Clicked() {
		open = !open
		a.opened[key] = open
	}
	row.Children(func() {
		if a.inWork {
			ui.Box(c).Width(16).Center().Children(func() { sym(c, "bulb", 15, p.OnSurfaceVariant) })
			ui.Text(c, label).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine()
		} else {
			disclosureChevron(c, key, open, 12, p.OnSurfaceVariant)
			ui.Text(c, label).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine()
		}
		ui.Spacer(c)
	})
	if open {
		ui.Column(c.Key("thought-body:" + key)).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 120 * time.Millisecond}).
			Children(func() {
				ui.Text(c, strings.TrimSpace(e.Text)).FontSize(13.5).LineHeight(1.55).TextColor(p.OnSurfaceVariant).Padding(4, 0, 8, 22).Selectable()
			})
	}
}

var rings = map[ringKey]*ui.SVG{}

type ringKey struct {
	pct         int
	track, arc  uint32
}

func ring(p *Palette, pct int, alert bool) *ui.SVG {
	pct = max(0, min(100, pct))
	arc := p.Primary
	if alert {
		arc = p.Error
	}
	key := ringKey{pct, argbFromColor(p.OutlineVariant), argbFromColor(arc)}
	if svg, ok := rings[key]; ok {
		return svg
	}
	hex := func(col ui.Color) string {
		return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", col.R, col.G, col.B, float64(col.A)/255)
	}
	const circ = 2 * 3.14159265 * 6
	doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none">`+
		`<circle cx="8" cy="8" r="6" stroke="%s" stroke-width="1.8"/>`+
		`<circle cx="8" cy="8" r="6" stroke="%s" stroke-width="1.8" stroke-linecap="round" stroke-dasharray="%.2f %.2f" transform="rotate(-90 8 8)"/></svg>`,
		hex(p.OutlineVariant), hex(arc), circ*float64(pct)/100, circ)
	svg := ui.MustParseSVG([]byte(doc))
	if len(rings) > 128 {
		clear(rings)
	}
	rings[key] = svg
	return svg
}

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
	svg := ring(p, pct, pct >= 85)
	tip := fmt.Sprintf("%d%% — %s", pct, locales.ContextUsed(tokens(a.C.LastInput), tokens(window)))
	if total := a.turnCost(a.C.Usage); total != "" {
		tip += " (" + total + ")"
	}
	ui.Row(c).Height(30).Gap(5).Padding(0, 6).AlignItems(ui.Center).Tooltip(tip).Children(func() {
		ui.Image(c, svg).Size(15, 15)
		if pct >= 50 {
			ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine().FontFeatures("tnum")
		}
	})
}
