package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

// reviewTabs renders Material Design 3 Primary Tabs with an animated bottom Primary indicator.
func (a *App) reviewTabs(c *ui.Context, files int) {
	p := a.p
	tabs := []struct{ id, label string }{
		{"", locales.Changes()},
		{"files", locales.Files()},
		{"preview", locales.Browser()},
		{"term", locales.Terminal()},
	}
	if a.latestPlan() != nil || a.rv.tab == "plan" {
		tabs = append(tabs, struct{ id, label string }{"plan", locales.Plan()})
	}
	activeIdx := 0
	for i, t := range tabs {
		if a.rv.tab == t.id {
			activeIdx = i
			break
		}
	}
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(p.OutlineVariant).DragWindow().Children(func() {
		track := ui.Row(c.Key("rv-tabs")).FillHeight().AlignItems(ui.Center)
		if a.rv.maximized {
			track.Width(440).Shrink(1)
		} else {
			track.Grow(1).MinWidth(0)
		}
		trackW := track.Bounds().W
		pos := track.AnimateTo("tab-pos", float32(activeIdx), animStandard)
		track.Children(func() {
			if trackW > 8 {
				segW := trackW / float32(len(tabs))
				indW := min(float32(36), max(float32(18), segW*0.55))
				ui.Box(c.Key("rv-ind")).Absolute().Bottom(0).Left(pos*segW + (segW-indW)/2).
					Size(indW, 3).Radius(3, 3, 0, 0).Background(p.Primary).PassThrough().ZIndex(2)
			}
			for _, t := range tabs {
				on := a.rv.tab == t.id
				b := ui.ButtonBase(c.Key("tab:"+t.label)).Row().Grow(1).Basis(0).MinWidth(0).Justify(ui.Center).
					Height(32).Padding(0, 8).Gap(5).Radius(16).AlignItems(ui.Center).Label(t.label).ZIndex(1)
				m3Focus(b, p)
				m3State(b, p, ui.Transparent, false)
				fg, weight := p.OnSurfaceVariant, 500
				if on {
					fg, weight = p.Primary, 600
				} else if b.Hovered() {
					fg = p.OnSurface
				}
				b.Children(func() {
					ui.Text(c, t.label).FontSize(13.5).FontWeight(weight).TextColor(fg).SingleLine()
					if t.id == "" && files > 0 {
						ui.Box(c).Height(18).Padding(0, 6).Radius(9).Background(p.SurfaceContainerHigh).Center().Children(func() {
							ui.Text(c, fmt.Sprint(files)).FontSize(11).FontWeight(600).TextColor(p.OnSurfaceVariant).FontFeatures("tnum").SingleLine()
						})
					}
					if trackW <= 8 && on {
						ui.Box(c).Absolute().Bottom(0).Left(12).Right(12).Height(3).Radius(3, 3, 0, 0).Background(p.Primary)
					}
				})
				if b.Clicked() {
					if t.id == "term" && a.rv.tab != "term" {
						a.showTerminal()
					} else {
						a.rv.tab = t.id
					}
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
		if a.iconButtonSized(c, "rv-maximize", icon, tip, 18).Clicked() {
			a.rv.maximized = !a.rv.maximized
		}
		if a.iconButtonSized(c, "rv-close", "x", locales.HideChanges(), 18).Clicked() {
			a.rv.open = false
			a.rv.maximized = false
		}
	})
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
	ui.Column(c).Grow(1).MinHeight(0).Clip().Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(16, 18).Children(func() {
			title := pl.Title
			if title == "" {
				title = locales.Plan()
			}
			ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(p.OnSurface).Margin(0, 0, 4, 0)
			ui.Text(c, locales.PlanProgress(len(pl.Steps), done)).FontSize(12.5).TextColor(p.OnSurfaceVariant).Margin(0, 0, 14, 0)
			for _, s := range pl.Steps {
				ui.Row(c).Gap(10).Padding(8, 0).Children(func() {
					box := ui.Box(c).Size(18, 18).Radius(6).Shrink(0).Margin(1, 0, 0, 0).Center()
					fg := p.OnSurface
					switch s.Status {
					case "done":
						fg = p.OnSurfaceVariant
						box.Background(p.Primary).Children(func() { sym(c, "check", 12, p.OnPrimary) })
					case "in_progress":
						box.Background(p.PrimaryContainer).Border(1.5, p.Primary).Children(func() { ui.Box(c).Size(6, 6).Radius(2).Background(p.Primary) })
					default:
						box.Border(1.5, p.Outline)
					}
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, s.Step).FontSize(14).LineHeight(1.45).TextColor(fg)
						if s.Note != "" {
							ui.Text(c, s.Note).FontSize(13).LineHeight(1.45).TextColor(p.OnSurfaceVariant)
						}
					})
				})
			}
		})
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
		ui.Column(c).Grow(1).MinHeight(0).Clip().Children(func() {
			ui.Column(c).Shrink(0).Padding(10, 12, 8, 12).Children(func() {
				ui.SearchField(c, &a.rv.fileQuery).Label(locales.Search()).Placeholder(locales.FilterFiles())
			})
			rendered := false
			if q == "" {
				if nodes := a.buildTreeNodes(cur.Cwd); len(nodes) > 0 {
					a.fileTreeListNodes(c, cur.Cwd, nodes, 6)
					rendered = true
				}
			}
			if !rendered {
				if key := cur.Cwd + "\x00" + q; a.filesKey != key && a.C.Phase == client.Ready {
					if cmds := a.C.FindFiles(q); cmds != nil {
						a.filesKey = key
						a.quiet(cmds, nil)
					}
				}
				if len(a.C.Files) == 0 {
					ui.Column(c).Grow(1).MinHeight(0).Children(func() {
						a.reviewNote(c, locales.NoFilesFound())
					})
				} else {
					ui.Scroll(c).Grow(1).MinHeight(0).Padding(8, 8, 12, 8).Children(func() {
						for _, f := range a.C.Files {
							isDir := strings.HasSuffix(f, "/")
							icon := "file-text"
							if isDir {
								icon = "folder"
							}
							row := ui.ButtonBase(c.Key("rf:"+f)).Row().Height(32).Padding(0, 10).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(f)
							m3Focus(row, p)
							m3State(row, p, ui.Transparent, false)
							file := f
							row.ContextMenu(func(m *ui.Menu) {
								if !isDir && m.Item(locales.Preview()).Chosen() {
									a.openFilePreview(cur.Cwd, file)
								}
								if isDir && m.Item(locales.OpenInTerminal()).Chosen() {
									a.openTerminalIn(filepath.Join(cur.Cwd, file))
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
								sym(c, icon, 15, p.OnSurfaceVariant)
								ui.Text(c, f).Font("monospace").FontSize(13).TextColor(p.OnSurface).SingleLine().TruncateMiddle().MinWidth(0)
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
				ui.Row(c).Height(38).Shrink(0).Padding(0, 10).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if a.iconPill(c, "fp-tree-toggle", "layout-sidebar-right", locales.ToggleWorkspaceTree()).Size(28, 28).Clicked() {
						a.rv.treeHidden = !a.rv.treeHidden
					}
				})
				emptyState(c, p, "file-text", locales.SelectFileToPreview(), "", nil)
			}
		})
		if !a.rv.treeHidden {
			ui.Column(c).Width(244).Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(p.OutlineVariant).Children(func() {
				renderTree()
			})
		}
	})
}
