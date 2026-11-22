package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

var (
	evPierreTheme = mygo.NewEvent[pierreThemePayload]("pierre:theme")
	evPierreMode  = mygo.NewEvent[pierreModePayload]("pierre:mode")
	evPierreDiffs = mygo.NewEvent[pierreDiffsPayload]("pierre:diffs")
	evPierreTree  = mygo.NewEvent[pierreTreePayload]("pierre:tree")
	evPierreFile  = mygo.NewEvent[pierreFilePayload]("pierre:file")
)

type pierreThemePayload struct {
	Dark  bool   `json:"dark"`
	Bg    string `json:"bg"`
	Sheet string `json:"sheet"`
	Card  string `json:"card"`
	Fg    string `json:"fg"`
	Fg2   string `json:"fg2"`
	Mute  string `json:"mute"`
	Edge  string `json:"edge"`
	Edge2 string `json:"edge2"`
	Sel   string `json:"sel"`
	Hover string `json:"hover"`
	Chip  string `json:"chip"`
	Live  string `json:"live"`
	Plus  string `json:"plus"`
	Minus string `json:"minus"`
}

type pierreModePayload struct {
	Mode       string `json:"mode"`
	DiffStyle  string `json:"diffStyle"`
	TreeHidden bool   `json:"treeHidden"`
}

type pierreDiffFile struct {
	Key      string  `json:"key"`
	Hash     string  `json:"hash"`
	TurnSeq  int64   `json:"turnSeq"`
	Path     string  `json:"path"`
	RelPath  string  `json:"relPath"`
	Status   string  `json:"status"`
	Added    int     `json:"added"`
	Removed  int     `json:"removed"`
	Before   *string `json:"before,omitempty"`
	After    *string `json:"after,omitempty"`
	Reverted bool    `json:"reverted"`
}

type pierreDiffsPayload struct {
	DiffStyle string           `json:"diffStyle"`
	Files     []pierreDiffFile `json:"files"`
}

type pierreGitStatusEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type pierreTreePayload struct {
	Cwd       string                 `json:"cwd"`
	Paths     []string               `json:"paths"`
	GitStatus []pierreGitStatusEntry `json:"gitStatus"`
	Hidden    bool                   `json:"hidden"`
}

type pierreFilePayload struct {
	Path       string   `json:"path"`
	Content    string   `json:"content"`
	IsMarkdown bool     `json:"isMarkdown"`
	Raw        bool     `json:"raw"`
	Tabs       []string `json:"tabs"`
	Err        string   `json:"err,omitempty"`
}

type pierreState struct {
	wv                *mygo.WebView
	ready             atomic.Bool
	diffStyle         string // "unified" or "split"
	openTabs          []string
	focusComposerNext bool
	fileContent       string
	fileLoadedFor     string
	lastThemeSig      string
	lastModeSig       string
	lastDiffsSig      string
	lastTreeSig       string
	lastFileSig       string
}

// SetWebView attaches the embedded Pierre WebView to the app.
func (a *App) SetWebView(wv *mygo.WebView) {
	a.pierre.wv = wv
}

// PierreBridge returns the Go service bound as "Pierre" for the WebView.
func (a *App) PierreBridge() *PierreBridge {
	return &PierreBridge{a: a}
}

// PierreBridge exposes actions callable from the Pierre WebView page.
type PierreBridge struct {
	a *App
}

func (a *App) runOnUI(fn func()) {
	if a.update != nil {
		a.update(fn)
		return
	}
	fn()
}

func (b *PierreBridge) Ready() {
	b.a.pierre.ready.Store(true)
	b.a.runOnUI(func() {
		b.a.pierre.lastThemeSig = ""
		b.a.pierre.lastModeSig = ""
		b.a.pierre.lastDiffsSig = ""
		b.a.pierre.lastTreeSig = ""
		b.a.pierre.lastFileSig = ""
	})
}

func (b *PierreBridge) SelectFile(relPath string) {
	if relPath == "" {
		return
	}
	b.a.runOnUI(func() {
		a := b.a
		if a.C.Current == nil {
			return
		}
		if !slices.Contains(a.pierre.openTabs, relPath) {
			a.pierre.openTabs = append(a.pierre.openTabs, relPath)
		}
		a.rv.previewPath = relPath
		a.rv.previewRaw = false
		a.pierre.lastFileSig = ""
		a.loadPierreFileAsync(a.C.Current.Cwd, relPath)
	})
}

func (b *PierreBridge) CloseTab(relPath string) {
	b.a.runOnUI(func() {
		a := b.a
		idx := slices.Index(a.pierre.openTabs, relPath)
		if idx < 0 {
			return
		}
		a.pierre.openTabs = slices.Delete(a.pierre.openTabs, idx, idx+1)
		if a.rv.previewPath == relPath {
			if len(a.pierre.openTabs) > 0 {
				next := a.pierre.openTabs[min(idx, len(a.pierre.openTabs)-1)]
				a.rv.previewPath = next
				a.rv.previewRaw = false
				if a.C.Current != nil {
					a.loadPierreFileAsync(a.C.Current.Cwd, next)
				}
			} else {
				a.rv.previewPath = ""
				a.pierre.fileContent = ""
				a.pierre.fileLoadedFor = ""
			}
		}
		a.pierre.lastFileSig = ""
	})
}

func (b *PierreBridge) ToggleTree() {
	b.a.runOnUI(func() {
		b.a.rv.treeHidden = !b.a.rv.treeHidden
		b.a.pierre.lastModeSig = ""
		b.a.pierre.lastTreeSig = ""
	})
}

func (b *PierreBridge) ToggleDiffStyle() {
	b.a.runOnUI(func() {
		if b.a.pierre.diffStyle == "split" {
			b.a.pierre.diffStyle = "unified"
		} else {
			b.a.pierre.diffStyle = "split"
		}
		b.a.pierre.lastModeSig = ""
		b.a.pierre.lastDiffsSig = ""
	})
}

func (b *PierreBridge) ToggleMarkdownRaw(raw bool) {
	b.a.runOnUI(func() {
		b.a.rv.previewRaw = raw
		b.a.pierre.lastFileSig = ""
	})
}

func (b *PierreBridge) RevertFile(turnSeq int64, path string) {
	b.a.runOnUI(func() {
		b.a.do(b.a.C.Revert(turnSeq, path, false))
	})
}

func (b *PierreBridge) AddRemark(path string, line int, side, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.a.runOnUI(func() {
		b.a.rv.remarks = append(b.a.rv.remarks, remark{
			path: path,
			line: line,
			old:  side == "old",
			text: text,
		})
	})
}

func (b *PierreBridge) OpenExternal(relPath string) {
	b.a.runOnUI(func() {
		a := b.a
		if a.C.Current == nil || a.openIn == nil {
			return
		}
		abs := filepath.Join(a.C.Current.Cwd, relPath)
		edLabel, edApp := a.preferredEditor()
		if err := a.openIn(edApp, abs); err != nil {
			if edLabel == "" {
				edLabel = filepath.Base(abs)
			}
			a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
		}
	})
}

func (b *PierreBridge) EscapeToComposer() {
	b.a.runOnUI(func() {
		b.a.pierre.focusComposerNext = true
	})
}

func (a *App) loadPierreFileAsync(cwd, relPath string) {
	abs := filepath.Join(cwd, relPath)
	a.rv.previewLoading = true
	a.rv.previewErr = ""
	load := func() {
		info, err := os.Stat(abs)
		var content, errMsg string
		if err != nil {
			errMsg = err.Error()
		} else if info.Size() > 1<<20 {
			errMsg = fmt.Sprintf("File too large to preview (%d KB)", info.Size()/1024)
		} else {
			data, rerr := os.ReadFile(abs)
			if rerr != nil {
				errMsg = rerr.Error()
			} else if bytes.IndexByte(data, 0) >= 0 {
				errMsg = "Binary file"
			} else {
				content = string(data)
			}
		}
		a.runOnUI(func() {
			if a.rv.previewPath != relPath {
				return
			}
			a.rv.previewLoading = false
			a.rv.previewErr = errMsg
			a.pierre.fileContent = content
			a.pierre.fileLoadedFor = cwd + "\x00" + relPath
			a.pierre.lastFileSig = ""
		})
	}
	if a.update != nil {
		go load()
	} else {
		load()
	}
}

func colorCSS(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float64(c.A)/255.0)
}

func (a *App) buildPierreTheme() pierreThemePayload {
	p := a.p
	if p == nil {
		p = light
	}
	return pierreThemePayload{
		Dark:  p.Dark,
		Bg:    colorCSS(p.Bg),
		Sheet: colorCSS(p.Sheet),
		Card:  colorCSS(p.Card),
		Fg:    colorCSS(p.Fg),
		Fg2:   colorCSS(p.Fg2),
		Mute:  colorCSS(p.Mute),
		Edge:  colorCSS(p.Edge),
		Edge2: colorCSS(p.Edge2),
		Sel:   colorCSS(p.Sel),
		Hover: colorCSS(p.Hover),
		Chip:  colorCSS(p.Chip),
		Live:  colorCSS(p.Live),
		Plus:  colorCSS(p.Plus),
		Minus: colorCSS(p.Minus),
	}
}

func (a *App) buildPierreDiffs(turns []proto.ChangeTurn) pierreDiffsPayload {
	style := a.pierre.diffStyle
	if style != "split" {
		style = "unified"
	}
	cwd := ""
	if a.C.Current != nil {
		cwd = a.C.Current.Cwd
	}
	var files []pierreDiffFile
	for _, t := range turns {
		for _, f := range t.Files {
			key := client.DiffKey(t.TurnSeq, f.Path)
			d, ok := a.diffSource(key)
			if !ok {
				continue
			}
			sum := diffHash(d)
			rel := f.Path
			if cwd != "" {
				if r, err := filepath.Rel(cwd, f.Path); err == nil && !strings.HasPrefix(r, "..") {
					rel = r
				}
			}
			status := "M"
			if d.Before == nil {
				status = "A"
			} else if d.After == nil {
				status = "D"
			}
			files = append(files, pierreDiffFile{
				Key:      key,
				Hash:     hex.EncodeToString(sum[:]),
				TurnSeq:  t.TurnSeq,
				Path:     f.Path,
				RelPath:  rel,
				Status:   status,
				Added:    f.Added,
				Removed:  f.Removed,
				Before:   d.Before,
				After:    d.After,
				Reverted: a.C.Changes.RevertedFiles[key],
			})
		}
	}
	return pierreDiffsPayload{DiffStyle: style, Files: files}
}

func (a *App) buildPierreTree(cwd string) pierreTreePayload {
	const maxPierreTreeFiles = 10000
	var paths []string
	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if path != cwd && (name == ".git" || name == ".DS_Store" || name == "node_modules") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == cwd {
			return nil
		}
		rel, rerr := filepath.Rel(cwd, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			paths = append(paths, rel+"/")
		} else {
			paths = append(paths, rel)
		}
		if len(paths) >= maxPierreTreeFiles {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(paths)

	var gitStatus []pierreGitStatusEntry
	for _, f := range a.C.Workspace.Files {
		rel := f.Path
		if r, err := filepath.Rel(cwd, f.Path); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		gitStatus = append(gitStatus, pierreGitStatusEntry{Path: rel, Status: "modified"})
	}
	return pierreTreePayload{
		Cwd:       cwd,
		Paths:     paths,
		GitStatus: gitStatus,
		Hidden:    a.rv.treeHidden,
	}
}

func (a *App) buildPierreFile(cwd string) pierreFilePayload {
	path := a.rv.previewPath
	isMd := strings.EqualFold(filepath.Ext(path), ".md")
	tabs := append([]string(nil), a.pierre.openTabs...)
	if path != "" && a.pierre.fileLoadedFor != cwd+"\x00"+path && !a.rv.previewLoading && a.rv.previewErr == "" {
		a.loadPierreFileAsync(cwd, path)
	}
	return pierreFilePayload{
		Path:       path,
		Content:    a.pierre.fileContent,
		IsMarkdown: isMd,
		Raw:        a.rv.previewRaw,
		Tabs:       tabs,
		Err:        a.rv.previewErr,
	}
}

func (a *App) syncPierre(mode string, turns []proto.ChangeTurn) {
	if a.pierre.wv == nil || !a.pierre.ready.Load() {
		return
	}
	page := a.pierre.wv.Page()
	if page == nil {
		return
	}
	theme := a.buildPierreTheme()
	themeSig := fmt.Sprintf("%v:%s:%s:%s:%s", theme.Dark, theme.Bg, theme.Sheet, theme.Fg, theme.Live)
	if themeSig != a.pierre.lastThemeSig {
		a.pierre.lastThemeSig = themeSig
		_ = evPierreTheme.EmitPage(page, theme)
	}

	style := a.pierre.diffStyle
	if style != "split" {
		style = "unified"
	}
	modeSig := fmt.Sprintf("%s:%s:%v", mode, style, a.rv.treeHidden)
	if modeSig != a.pierre.lastModeSig {
		a.pierre.lastModeSig = modeSig
		_ = evPierreMode.EmitPage(page, pierreModePayload{
			Mode:       mode,
			DiffStyle:  style,
			TreeHidden: a.rv.treeHidden,
		})
	}

	switch mode {
	case "diffs":
		diffs := a.buildPierreDiffs(turns)
		h := sha256.New()
		h.Write([]byte(diffs.DiffStyle))
		for _, f := range diffs.Files {
			h.Write([]byte(f.Key))
			h.Write([]byte(f.Hash))
			if f.Reverted {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		}
		sig := hex.EncodeToString(h.Sum(nil))
		if sig != a.pierre.lastDiffsSig {
			a.pierre.lastDiffsSig = sig
			_ = evPierreDiffs.EmitPage(page, diffs)
		}
	case "files":
		if a.C.Current == nil {
			return
		}
		cwd := a.C.Current.Cwd
		treeSig := fmt.Sprintf("%s:%d:%v:%d", cwd, a.C.Workspace.Read, a.rv.treeHidden, len(a.C.Workspace.Files))
		if treeSig != a.pierre.lastTreeSig {
			a.pierre.lastTreeSig = treeSig
			_ = evPierreTree.EmitPage(page, a.buildPierreTree(cwd))
		}
		file := a.buildPierreFile(cwd)
		fileSig := fmt.Sprintf("%s:%v:%d:%s:%s", file.Path, file.Raw, len(file.Content), strings.Join(file.Tabs, "|"), file.Err)
		if fileSig != a.pierre.lastFileSig {
			a.pierre.lastFileSig = fileSig
			_ = evPierreFile.EmitPage(page, file)
		}
	}
}
