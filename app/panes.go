package app

import (
	"encoding/json"
	"fmt"
	"os"
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
	tabs := []struct{ id, label string }{
		{"", locales.Changes()},
		{"files", locales.Files()},
		{"preview", locales.Preview()},
		{"term", locales.Terminal()},
		{"plan", locales.Plan()},
	}
	activeIdx := 0
	for i, t := range tabs {
		if a.rv.tab == t.id {
			activeIdx = i
			break
		}
	}
	leftPad := float32(12)
	if a.rv.maximized {
		shut := toggleLeft(c) + 74 + 12
		leftPad = 12*a.sideOpen + shut*(1-a.sideOpen)
	}
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 12, 0, leftPad).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
		track := ui.Row(c.Key("rv-tabs")).Height(28).Padding(2, 2, 2, 2).Radius(8).Background(p.Chip).AlignItems(ui.Center)
		if a.rv.maximized {
			track.Width(400).Shrink(1)
		} else {
			track.Grow(1).MinWidth(0)
		}
		trackW := track.Bounds().W
		pos := track.Spring("tab-pos", float32(activeIdx), springDefault) // critically damped: a tap carries no momentum
		track.Children(func() {
			thumbBg := p.Card
			if p.Dark {
				thumbBg = ui.Hex("#2C2C31")
			}
			if trackW > 4 {
				segW := (trackW - 4) / float32(len(tabs))
				thumb := ui.Box(c.Key("rv-thumb")).Absolute().Top(2).Bottom(2).Left(2 + pos*segW).Width(segW).Radius(7).Background(thumbBg).PassThrough().ZIndex(0)
				if p.Dark {
					thumb.Border(.5, ui.RGBA(255, 255, 255, .14))
				}
				rim(thumb, p)
				raise(thumb, p)
			}
			for _, t := range tabs {
				on := a.rv.tab == t.id
				b := ui.ButtonBase(c.Key("tab:"+t.label)).Row().Grow(1).Basis(0).MinWidth(0).Justify(ui.Center).
					Height(24).Padding(0, 8).Gap(4).Radius(6).AlignItems(ui.Center).Label(t.label).ZIndex(1)
				press(b)
				focusRing(b, p)
				fg, weight := p.Fg2, 500
				if on {
					if trackW <= 4 {
						b.Background(thumbBg)
						if p.Dark {
							b.Border(.5, ui.RGBA(255, 255, 255, .14))
						}
						rim(b, p)
						raise(b, p)
					}
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
		if a.rv.maximized {
			ui.Spacer(c)
		}
		tip := locales.MaximizePane()
		icon := "maximize-2"
		if a.rv.maximized {
			tip = locales.RestorePane()
			icon = "minimize-2"
		}
		if a.iconButton(c, "rv-maximize", icon, tip).Clicked() {
			a.rv.maximized = !a.rv.maximized
		}
		if a.iconButtonSized(c, "rv-close", "x", locales.HideChanges(), 14.5).Clicked() {
			a.rv.open = false
			a.rv.maximized = false
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

// filesPane shows a collapsible workspace directory tree with in-pane file preview,
// and falls back to fuzzy search when a query is entered. When wide or maximized,
// it shows the code preview and workspace tree side-by-side.
func (a *App) filesPane(c *ui.Context) {
	p := a.p
	cur := a.C.Current
	if cur == nil {
		a.reviewNote(c, locales.NoOpenSession())
		return
	}
	if a.pierre.wv != nil {
		a.syncPierre("files", nil)
		ui.WebView(c, a.pierre.wv).Grow(1)
		return
	}
	w, _ := c.Size()
	split := (a.rv.maximized || a.reviewWidth() >= 560) && w >= 640
	if !split && a.rv.previewPath != "" {
		a.filePreviewView(c, cur.Cwd, false)
		return
	}
	renderTree := func() {
		q := strings.TrimSpace(a.rv.fileQuery)
		ui.Column(c).Padding(10, 12, 6, 12).Children(func() {
			ui.SearchField(c, &a.rv.fileQuery).Label(locales.Search()).Placeholder(locales.Search())
		})
		if q == "" {
			if nodes := a.buildTreeNodes(cur.Cwd); len(nodes) > 0 {
				a.fileTreeListNodes(c, cur.Cwd, nodes)
				return
			}
		}
		if key := cur.Cwd + "\x00" + q; a.filesKey != key && a.C.Phase == client.Ready {
			if cmds := a.C.FindFiles(q); cmds != nil {
				a.filesKey = key
				a.quiet(cmds, nil)
			}
		}
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
				focusRing(row, p)
				if row.Hovered() {
					row.Background(p.Hover)
				}
				file := f
				row.ContextMenu(func(m *ui.Menu) {
					if !isDir && m.Item(locales.Preview()).Chosen() {
						a.openFilePreview(cur.Cwd, file)
					}
					if !isDir && a.openIn != nil {
						if m.Item(locales.OpenInEditor()).Chosen() {
							edLabel, edApp := a.preferredEditor()
							if err := a.openIn(edApp, filepath.Join(cur.Cwd, file)); err != nil {
								if edLabel == "" {
									edLabel = file
								}
								a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
							}
						}
					}
					if m.Item(locales.CopyPath()).Chosen() {
						c.WriteClipboard(file)
						a.flash("check", locales.CopyPath())
					}
				})
				row.Children(func() {
					sym(c, icon, 14, p.Mute)
					ui.Text(c, f).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().TruncateMiddle().MinWidth(0)
				})
				if row.Clicked() && !isDir {
					abs := filepath.Join(cur.Cwd, f)
					if _, err := os.Stat(abs); err == nil {
						a.openFilePreview(cur.Cwd, f)
					} else if a.openIn != nil {
						edLabel, edApp := a.preferredEditor()
						if err := a.openIn(edApp, abs); err != nil {
							if edLabel == "" {
								edLabel = f
							}
							a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
						}
					}
				}
			}
		})
	}
	if !split {
		renderTree()
		return
	}
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			if a.rv.previewPath != "" {
				a.filePreviewView(c, cur.Cwd, true)
			} else {
				ui.Row(c).Height(34).Shrink(0).Padding(0, 8).AlignItems(ui.Center).
					BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
					ui.Spacer(c)
					if a.iconPill(c, "fp-tree-toggle", "layout-sidebar-right", locales.ToggleWorkspaceTree()).Size(24, 24).Clicked() {
						a.rv.treeHidden = !a.rv.treeHidden
					}
				})
				emptyState(c, p, "file-text", locales.SelectFileToPreview(), "", nil)
			}
		})
		if !a.rv.treeHidden {
			ui.Column(c).Width(240).Shrink(0).BorderWidth(0, 0, 0, .5).BorderColor(p.Edge).Children(func() {
				renderTree()
			})
		}
	})
}
