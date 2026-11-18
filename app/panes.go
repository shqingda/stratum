package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
)

// reviewTabs switches the review panel between the changes, the
// commands the agent ran, its plan, and the workspace's files.
func (a *App) reviewTabs(c *ui.Context, files int) {
	p := a.p
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 12).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Row(c).Grow(1).Height(30).Padding(2, 2, 2, 2).Radius(9).Background(p.Chip).AlignItems(ui.Center).Children(func() {
			for _, t := range []struct{ id, label string }{
				{"", locales.Changes()},
				{"files", locales.Files()},
				{"preview", locales.Preview()},
				{"term", locales.Terminal()},
				{"plan", locales.Plan()},
			} {
				on := a.rv.tab == t.id
				b := ui.ButtonBase(c.Key("tab:"+t.label)).Row().Grow(1).Basis(0).MinWidth(0).Justify(ui.Center).
					Height(26).Padding(0, 8).Gap(4).Radius(7).AlignItems(ui.Center).Label(t.label)
				press(b)
				fg, weight := p.Fg2, 500
				if on {
					b.Background(p.Card)
					rim(b, p)
					raise(b, p)
					fg, weight = p.Fg, 600
				} else if b.Hovered() {
					b.Background(p.Hover)
					fg = p.Fg
				}
				b.Children(func() {
					ui.Text(c, t.label).FontSize(13).FontWeight(weight).TextColor(fg).SingleLine()
					if t.id == "" && files > 0 {
						ui.Text(c, fmt.Sprint(files)).FontSize(11.5).FontWeight(500).TextColor(p.Mute).SingleLine()
					}
				})
				if b.Clicked() {
					a.rv.tab = t.id
				}
			}
		})
	})
}

// terminalPane shows the commands the agent ran, output as it came.
func (a *App) terminalPane(c *ui.Context) {
	p := a.p
	var runs []*client.Entry
	for _, e := range a.C.Entries {
		if e.Role == client.Tool && e.Text == "shell" {
			runs = append(runs, e)
		}
	}
	if len(runs) == 0 {
		a.reviewNote(c, locales.NoCommandsYet())
		return
	}
	prompt := "~/" + filepath.Base(a.C.Current.Cwd) + " $"
	line := func(s string, col ui.Color) {
		ui.Text(c, s).Font("monospace").FontSize(13).LineHeight(1.65).TextColor(col).SingleLine()
	}
	ui.Scroll(c).Grow(1).Padding(6, 14, 14, 14).Children(func() {
		for i, e := range runs {
			top := float32(8)
			if i == 0 {
				top = 0
			}
			ui.Row(c).Gap(7).Margin(top, 0, 0, 0).Children(func() {
				line(prompt, p.Mute)
				line(e.Detail, p.Fg)
			})
			for _, l := range strings.Split(strings.TrimRight(e.Output, "\n"), "\n") {
				line(l, termColor(p, l))
			}
		}
	})
}

// termColor marks the lines test runners pass and fail on.
func termColor(p *Palette, l string) ui.Color {
	t := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(t, "--- PASS") || strings.HasPrefix(t, "ok "):
		return p.Plus
	case strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "FAIL\t") || strings.HasSuffix(t, ": error") || strings.Contains(t, "_test.go:"):
		return p.Minus
	}
	return p.Fg2
}

type plan struct {
	Title string `json:"title"`
	Steps []struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Note   string `json:"note"`
	} `json:"steps"`
}

// latestPlan is the last plan the agent sent.
func (a *App) latestPlan() *plan {
	for i := len(a.C.Entries) - 1; i >= 0; i-- {
		e := a.C.Entries[i]
		if e.Role != client.Tool || e.Text != "update_plan" {
			continue
		}
		var pl plan
		if json.Unmarshal(e.Args, &pl) == nil && len(pl.Steps) > 0 {
			return &pl
		}
	}
	return nil
}

func (a *App) planPane(c *ui.Context) {
	p := a.p
	pl := a.latestPlan()
	if pl == nil {
		a.reviewNote(c, locales.NoPlanYet())
		return
	}
	done := 0
	for _, s := range pl.Steps {
		if s.Status == "done" {
			done++
		}
	}
	ui.Scroll(c).Grow(1).Padding(14, 18).Children(func() {
		title := pl.Title
		if title == "" {
			title = locales.Plan()
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(p.Fg).Margin(0, 0, 2, 0)
		ui.Text(c, locales.PlanProgress(len(pl.Steps), done)).FontSize(13).TextColor(p.Mute).Margin(0, 0, 14, 0)
		for _, s := range pl.Steps {
			ui.Row(c).Gap(10).Padding(7, 0).Children(func() {
				box := ui.Box(c).Size(16, 16).Radius(5).Shrink(0).Margin(2, 0, 0, 0).Center()
				fg := p.Fg
				switch s.Status {
				case "done":
					fg = p.Mute
					box.Background(p.Fg2).Children(func() { sym(c, "check", 11, p.Bg) })
				case "in_progress":
					box.Border(1.5, p.Live).Children(func() { ui.Box(c).Size(6, 6).Radius(2).Background(p.Live) })
				default:
					box.Border(1.2, p.Faint)
				}
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, s.Step).FontSize(14).LineHeight(1.45).TextColor(fg)
					if s.Note != "" {
						ui.Text(c, s.Note).FontSize(13).LineHeight(1.45).TextColor(p.Mute)
					}
				})
			})
		}
	})
}

// filesPane lists matching workspace files and opens one in the default editor.
func (a *App) filesPane(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	if cur == nil {
		a.reviewNote(c, locales.NoOpenSession())
		return
	}
	q := strings.TrimSpace(a.rv.fileQuery)
	if key := cur.Cwd + "\x00" + q; a.filesKey != key && a.C.Phase == client.Ready {
		if cmds := a.C.FindFiles(q); cmds != nil {
			a.filesKey = key
			a.quiet(cmds, nil)
		}
	}
	ui.Column(c).Padding(10, 12, 6, 12).Children(func() {
		ui.SearchField(c, &a.rv.fileQuery).Label(locales.Search()).Placeholder(locales.Search())
	})
	if len(a.C.Files) == 0 {
		a.reviewNote(c, locales.NoFilesFound())
		return
	}
	ui.Scroll(c).Grow(1).Padding(4, 8, 12, 8).Children(func() {
		for _, f := range a.C.Files {
			isDir := strings.HasSuffix(f, "/")
			icon := "file-text"
			if isDir {
				icon = "folder"
			}
			row := ui.ButtonBase(c.Key("rf:"+f)).Row().Height(30).Padding(0, 10).Gap(8).Radius(6).AlignItems(ui.Center).Justify(ui.Start).Label(f)
			press(row)
			if row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				sym(c, icon, 14, p.Mute)
				ui.Text(c, f).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().TruncateMiddle().MinWidth(0)
			})
			if row.Clicked() && !isDir && a.openIn != nil {
				if err := a.openIn("", filepath.Join(cur.Cwd, f)); err != nil {
					a.err = locales.CouldntOpen(f, err.Error())
				}
			}
		}
	})
}
