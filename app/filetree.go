package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

type dirItem struct {
	name  string
	rel   string
	isDir bool
}

type treeNode struct {
	rel      string
	name     string
	isDir    bool
	expanded bool
	depth    int
}

const (
	maxTreeNodes   = 3000
	maxPreviewSize = 512 * 1024
)

func (a *App) ensureTreeCwd(cwd string) {
	if a.rv.treeCwd == cwd {
		return
	}
	a.rv.treeCwd = cwd
	a.rv.treeOpen = map[string]bool{}
	a.rv.treeCache = map[string][]dirItem{}
	a.rv.previewPath = ""
	a.rv.previewLines = nil
	a.rv.previewTok = nil
	a.rv.previewErr = ""
	a.rv.previewLoading = false
}

func (a *App) readDirCached(cwd, rel string) []dirItem {
	a.ensureTreeCwd(cwd)
	if items, ok := a.rv.treeCache[rel]; ok {
		return items
	}
	dir := cwd
	if rel != "" {
		dir = filepath.Join(cwd, rel)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.rv.treeCache[rel] = nil
		return nil
	}
	var items []dirItem
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == ".DS_Store" || name == "node_modules" {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = filepath.Join(rel, name)
		}
		items = append(items, dirItem{
			name:  name,
			rel:   childRel,
			isDir: e.IsDir(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].isDir != items[j].isDir {
			return items[i].isDir
		}
		return strings.ToLower(items[i].name) < strings.ToLower(items[j].name)
	})
	a.rv.treeCache[rel] = items
	return items
}

func (a *App) buildTreeNodes(cwd string) []treeNode {
	a.ensureTreeCwd(cwd)
	var out []treeNode
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		for _, it := range a.readDirCached(cwd, rel) {
			if len(out) >= maxTreeNodes {
				return
			}
			exp := it.isDir && a.rv.treeOpen[it.rel]
			out = append(out, treeNode{
				rel:      it.rel,
				name:     it.name,
				isDir:    it.isDir,
				expanded: exp,
				depth:    depth,
			})
			if exp {
				walk(it.rel, depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

func (a *App) gitStatusMap(cwd string) map[string]byte {
	out := map[string]byte{}
	for _, t := range a.C.Changes.Turns {
		for _, f := range t.Files {
			rel := f.Path
			if r, err := filepath.Rel(cwd, f.Path); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
			st := byte('M')
			if d := a.fileDiff(client.DiffKey(t.TurnSeq, f.Path)); d != nil && d.status != 0 {
				st = d.status
			}
			out[rel] = st
		}
	}
	return out
}

func (a *App) openFilePreview(cwd, rel string) {
	a.ensureTreeCwd(cwd)
	a.rv.previewPath = rel
	a.rv.previewRaw = false
	a.rv.previewLoading = true
	a.rv.previewErr = ""
	a.rv.previewLines = nil
	a.rv.previewTok = nil
	a.rv.previewList = ui.ListState{}

	load := func() (lines []string, toks [][]editor.Token, errMsg string) {
		abs := filepath.Join(cwd, rel)
		info, err := os.Stat(abs)
		if err != nil {
			return nil, nil, err.Error()
		}
		if info.IsDir() {
			return nil, nil, locales.EmptyDir()
		}
		if info.Size() > maxPreviewSize {
			return nil, nil, fmt.Sprintf("File too large to preview (%d KB)", info.Size()/1024)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, nil, err.Error()
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, nil, "Binary file"
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		toks = tokenize(rel, lines)
		return lines, toks, ""
	}

	if a.update == nil {
		lines, toks, errMsg := load()
		a.rv.previewLoading = false
		a.rv.previewLines = lines
		a.rv.previewTok = toks
		a.rv.previewErr = errMsg
		return
	}
	go func() {
		lines, toks, errMsg := load()
		a.update(func() {
			if a.rv.previewPath != rel {
				return
			}
			a.rv.previewLoading = false
			a.rv.previewLines = lines
			a.rv.previewTok = toks
			a.rv.previewErr = errMsg
		})
	}()
}

func (a *App) fileTreeList(c *ui.Context, cwd string) {
	nodes := a.buildTreeNodes(cwd)
	if len(nodes) == 0 {
		a.reviewNote(c, locales.NoFilesFound())
		return
	}
	a.fileTreeListNodes(c, cwd, nodes, 4)
}

func (a *App) fileTreeListNodes(c *ui.Context, cwd string, nodes []treeNode, topPad float32) {
	p := a.p
	status := a.gitStatusMap(cwd)
	st := &a.rv.treeList
	st.Key = func(i int) any { return nodes[i].rel }
	ui.List(c, st, len(nodes), func(i int) {
		n := nodes[i]
		row := ui.ButtonBase(c.Key("ft:"+n.rel)).Row().Height(28).Padding(0, 8, 0, float32(8+n.depth*14)).
			Gap(6).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(n.name)
		m3Focus(row, p)
		m3State(row, p, ui.Transparent, false)
		if n.isDir {
			row.Expanded(n.expanded)
		}
		abs := filepath.Join(cwd, n.rel)
		row.ContextMenu(func(m *ui.Menu) {
			if !n.isDir {
				if m.Item(locales.Preview()).Chosen() {
					a.openFilePreview(cwd, n.rel)
				}
				if a.openIn != nil && m.Item(locales.OpenInEditor()).Chosen() {
					edLabel, edApp := a.preferredEditor()
					if err := a.openIn(edApp, abs); err != nil {
						if edLabel == "" {
							edLabel = n.name
						}
						a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
					}
				}
			}
			if n.isDir && m.Item(locales.OpenInTerminal()).Chosen() {
				a.openTerminalIn(abs)
			}
			if m.Item(locales.RevealInFinder()).Chosen() {
				_ = exec.Command("open", "-R", abs).Start()
			}
			if m.Item(locales.CopyPath()).Chosen() {
				c.WriteClipboard(n.rel)
				a.flash("check", locales.CopyPath())
			}
		})
		row.Children(func() {
			if n.isDir {
				disclosureChevron(c, "ft:"+n.rel, n.expanded, 10, p.OnSurfaceVariant)
				sym(c, "folder", 14, p.OnSurfaceVariant)
			} else {
				ui.Box(c).Width(12).Shrink(0)
				sym(c, "file-text", 14, p.OnSurfaceVariant)
			}
			ui.Text(c, n.name).Font("monospace").FontSize(13).TextColor(p.OnSurface).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			if !n.isDir {
				if ch, ok := status[n.rel]; ok {
					col := p.OnSurfaceVariant
					if ch == 'A' {
						col = p.Plus
					} else if ch == 'D' {
						col = p.Minus
					}
					ui.Text(c, string(ch)).FontSize(11.5).FontWeight(600).TextColor(col).Shrink(0)
				}
			}
		})
		if row.Clicked() {
			if n.isDir {
				a.rv.treeOpen[n.rel] = !n.expanded
			} else {
				a.openFilePreview(cwd, n.rel)
			}
		}
	}).Grow(1).MinHeight(0).Padding(topPad, 8, 12, 8)
}

func (a *App) filePreviewView(c *ui.Context, cwd string, split bool) {
	p := a.p
	rel := a.rv.previewPath
	abs := filepath.Join(cwd, rel)
	lower := strings.ToLower(rel)
	isMD := strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
	const headerH = 40
	ui.Column(c).Grow(1).MinHeight(0).Clip().Children(func() {
		ui.Row(c).Height(headerH).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center).
			Background(p.SurfaceContainerLow).Children(func() {
			if !split {
				back := ui.ButtonBase(c.Key("fp-back")).Row().Height(28).Padding(0, 8).Gap(4).Radius(8).
					AlignItems(ui.Center).Label(locales.Close())
				m3Focus(back, p)
				m3State(back, p, ui.Color{}, false)
				back.Children(func() {
					sym(c, "arrow-back-up", 14, p.OnSurfaceVariant)
					ui.Text(c, locales.Files()).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).SingleLine()
				})
				if back.Clicked() {
					a.rv.previewPath = ""
					return
				}
			}
			crumb := strings.ReplaceAll(rel, string(filepath.Separator), "  ›  ")
			ui.Text(c, crumb).Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().TruncateMiddle().Grow(1).MinWidth(0)
			if isMD {
				seg := ui.Row(c.Key("fp-md-seg")).Height(28).Padding(2).Radius(14).
					Background(p.SurfaceContainerHigh).Gap(2).AlignItems(ui.Center).Shrink(0)
				seg.Children(func() {
					opts := []struct {
						raw   bool
						label string
					}{{false, locales.Preview()}, {true, locales.Raw()}}
					for _, opt := range opts {
						on := a.rv.previewRaw == opt.raw
						btn := ui.ButtonBase(c.Key(fmt.Sprint("fp-md:", opt.raw))).Row().Height(24).Padding(0, 10).Gap(4).Radius(12).
							AlignItems(ui.Center).Label(opt.label)
						m3Focus(btn, p)
						fg, weight := p.OnSurfaceVariant, 500
						if on {
							m3State(btn, p, p.SecondaryContainer, true)
							fg, weight = p.OnSecondaryContainer, 600
						} else {
							m3State(btn, p, ui.Color{}, false)
						}
						btn.Children(func() {
							if on {
								sym(c, "check", 12, fg)
							}
							ui.Text(c, opt.label).FontSize(11.5).FontWeight(weight).TextColor(fg).SingleLine()
						})
						if btn.Clicked() {
							a.rv.previewRaw = opt.raw
						}
					}
				})
			}
			if a.iconPill(c, "fp-copy", "copy", locales.CopyPath()).Size(28, 28).Clicked() {
				c.WriteClipboard(rel)
				a.flash("check", locales.CopyPath())
			}
			if a.iconPill(c, "fp-finder", "folder", locales.RevealInFinder()).Size(28, 28).Clicked() {
				_ = exec.Command("open", "-R", abs).Start()
			}
			if a.openIn != nil && a.iconPill(c, "fp-open", "external-link", locales.OpenInEditor()).Size(28, 28).Clicked() {
				edLabel, edApp := a.preferredEditor()
				if err := a.openIn(edApp, abs); err != nil {
					if edLabel == "" {
						edLabel = rel
					}
					a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
				}
			}
			if split && a.iconPill(c, "fp-tree-toggle", "layout-sidebar-right", locales.ToggleWorkspaceTree()).Size(28, 28).Clicked() {
				a.rv.treeHidden = !a.rv.treeHidden
			}
		})
		switch {
		case a.rv.previewLoading:
			ui.Column(c).Grow(1).MinHeight(0).Padding(16, 0, 0, 0).Children(func() {
				a.reviewNote(c, locales.Reading())
			})
		case a.rv.previewErr != "":
			ui.Column(c).Grow(1).MinHeight(0).Children(func() {
				emptyState(c, p, "file-text", a.rv.previewErr, "", nil)
			})
		case isMD && !a.rv.previewRaw:
			a.md.Theme = mdTheme(p, c.Theme())
			a.md.CodeBlock = a.codeBlock
			ui.Scroll(c).Grow(1).MinHeight(0).Padding(14, 18).Children(func() {
				markdown.View(c, strings.Join(a.rv.previewLines, "\n"), &a.md)
			})
		default:
			st := &a.rv.previewList
			st.Key = func(i int) any { return i }
			ui.List(c, st, len(a.rv.previewLines), func(i int) {
				line := a.rv.previewLines[i]
				var toks []editor.Token
				if i < len(a.rv.previewTok) {
					toks = a.rv.previewTok[i]
				}
				ui.Row(c).Height(20).Padding(0, 10, 0, 0).AlignItems(ui.Center).Children(func() {
					ui.Text(c, strconv.Itoa(i+1)).Width(40).Shrink(0).Padding(0, 8, 0, 4).
						Font("monospace").FontSize(11.5).FixedLineHeight(20).TextColor(p.OnSurfaceVariant).TextAlign(ui.End)
					ui.RichText(c, a.codeSpans(c, strings.TrimRight(line, "\r\n"), toks)...).
						SingleLine().PreserveSpaces().Grow(1).MinWidth(0)
				})
			}).Grow(1).MinHeight(0).Padding(6, 0, 12, 0)
		}
	})
}
