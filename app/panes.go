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
// commands the agent ran, and its plan.
func (a *App) reviewTabs(c *ui.Context, files int) {
	p := a.p
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 10).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
		for _, t := range []struct{ id, label string }{{"", locales.Changes()}, {"term", locales.Terminal()}, {"plan", locales.Plan()}} {
			on := a.rv.tab == t.id
			b := ui.ButtonBase(c.Key("tab:"+t.label)).Row().Height(28).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).Label(t.label)
			fg := p.Fg2
			if on {
				b.Background(p.Sel)
				fg = p.Fg
			} else if b.Hovered() {
				b.Background(p.Hover)
			}
			b.Children(func() {
				ui.Text(c, t.label).FontSize(13.5).FontWeight(500).TextColor(fg)
				if t.id == "" && files > 0 {
					ui.Text(c, fmt.Sprint(files)).FontSize(12).FontWeight(500).TextColor(p.Mute)
				}
			})
			if b.Clicked() {
				a.rv.tab = t.id
			}
		}
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
