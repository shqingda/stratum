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
	a.fileTreeListNodes(c, cwd, nodes)
}

func (a *App) fileTreeListNodes(c *ui.Context, cwd string, nodes []treeNode) {
	p := a.p
	status := a.gitStatusMap(cwd)
	st := &a.rv.treeList
	st.Key = func(i int) any { return nodes[i].rel }
	ui.List(c, st, len(nodes), func(i int) {
		n := nodes[i]
		row := ui.ButtonBase(c.Key("ft:"+n.rel)).Row().Height(26).Padding(0, 8, 0, float32(8+n.depth*14)).
			Gap(6).Radius(6).AlignItems(ui.Center).Justify(ui.Start).Label(n.name)
		press(row)
		focusRing(row, p)
		if row.Hovered() {
			row.Background(p.Hover)
		}
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
				disclosureChevron(c, "ft:"+n.rel, n.expanded, 10, p.Mute)
				sym(c, "folder", 14, p.Mute)
			} else {
				ui.Box(c).Width(12).Shrink(0)
				sym(c, "file-text", 14, p.Mute)
			}
			ui.Text(c, n.name).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			if !n.isDir {
				if ch, ok := status[n.rel]; ok {
					col := p.Mute
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
	}).Grow(1).MinHeight(0).Padding(4, 8, 12, 8)
}

func (a *App) filePreviewView(c *ui.Context, cwd string, split bool) {
	p := a.p
	rel := a.rv.previewPath
	abs := filepath.Join(cwd, rel)
	lower := strings.ToLower(rel)
	isMD := strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
	ui.Row(c).Height(34).Shrink(0).Padding(0, 8).Gap(4).AlignItems(ui.Center).
		BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		if !split {
			back := ui.ButtonBase(c.Key("fp-back")).Row().Height(24).Padding(0, 6).Gap(4).Radius(6).
				AlignItems(ui.Center).Label(locales.Close())
			press(back)
			focusRing(back, p)
			if back.Hovered() {
				back.Background(p.Hover)
			}
			back.Children(func() {
				sym(c, "arrow-back-up", 13, p.Fg2)
				ui.Text(c, locales.Files()).FontSize(12.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
			})
			if back.Clicked() {
				a.rv.previewPath = ""
				return
			}
		}
		crumb := strings.ReplaceAll(rel, string(filepath.Separator), "  ›  ")
		ui.Text(c, crumb).Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().TruncateMiddle().Grow(1).MinWidth(0)
		if isMD {
			seg := ui.Row(c.Key("fp-md-seg")).Height(24).Padding(2, 2, 2, 2).Radius(7).Background(p.Chip).AlignItems(ui.Center).Shrink(0)
			seg.Children(func() {
				for _, opt := range []struct {
					raw   bool
					label string
				}{{false, locales.Preview()}, {true, locales.Raw()}} {
					on := a.rv.previewRaw == opt.raw
					btn := ui.ButtonBase(c.Key(fmt.Sprint("fp-md:", opt.raw))).Height(20).Padding(0, 8).Radius(5).Center().Label(opt.label)
					press(btn)
					focusRing(btn, p)
					fg, weight := p.Fg2, 500
					if on {
						btn.Background(p.Card)
						rim(btn, p)
						fg, weight = p.Fg, 600
					} else if btn.Hovered() {
						btn.Background(p.Hover)
						fg = p.Fg
					}
					btn.Children(func() { ui.Text(c, opt.label).FontSize(11.5).FontWeight(weight).TextColor(fg).SingleLine() })
					if btn.Clicked() {
						a.rv.previewRaw = opt.raw
					}
				}
			})
		}
		if a.iconPill(c, "fp-copy", "copy", locales.CopyPath()).Size(24, 24).Clicked() {
			c.WriteClipboard(rel)
			a.flash("check", locales.CopyPath())
		}
		if a.iconPill(c, "fp-finder", "folder", locales.RevealInFinder()).Size(24, 24).Clicked() {
			_ = exec.Command("open", "-R", abs).Start()
		}
		if a.openIn != nil && a.iconPill(c, "fp-open", "external-link", locales.OpenInEditor()).Size(24, 24).Clicked() {
			edLabel, edApp := a.preferredEditor()
			if err := a.openIn(edApp, abs); err != nil {
				if edLabel == "" {
					edLabel = rel
				}
				a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
			}
		}
		if split && a.iconPill(c, "fp-tree-toggle", "layout-sidebar-right", locales.ToggleWorkspaceTree()).Size(24, 24).Clicked() {
			a.rv.treeHidden = !a.rv.treeHidden
		}
	})
	switch {
	case a.rv.previewLoading:
		a.reviewNote(c, locales.Reading())
	case a.rv.previewErr != "":
		emptyState(c, p, "file-text", a.rv.previewErr, "", nil)
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
					Font("monospace").FontSize(11.5).FixedLineHeight(20).TextColor(p.Faint).TextAlign(ui.End)
				ui.RichText(c, a.codeSpans(c, strings.TrimRight(line, "\r\n"), toks)...).
					SingleLine().PreserveSpaces().Grow(1).MinWidth(0)
			})
		}).Grow(1).MinHeight(0).Padding(6, 0, 12, 0)
	}
}
