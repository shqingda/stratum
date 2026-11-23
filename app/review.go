package app

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

const (
	reviewWidth   = 452
	maxDiffLines  = 2000 // lines a file's diff shows at most
	diffContext   = 3
	diffRowHeight = 20
)

// review is the state of the review panel: what the session's turns
// changed in files, and taking a turn's changes back.
type review struct {
	open      bool
	tab       string // "", "term", "plan", or "files"
	fileQuery string
	// uncommitted shows what git has not got, in place of a turn: turn,
	// or the last one when 0.
	uncommitted bool
	// branch shows what the branch changed since its base, as turn -1.
	branch bool
	turn   int64
	wsRead int
	sel    string // client.DiffKey of the file shown
	asked  map[string]bool
	diffs  *lruCache[string, *fileDiff]
	// computing are the large diffs being worked out off the UI thread.
	computing map[string]bool
	// The diffs show one under another, as a List of items: jump is the
	// file to scroll to, settle the frames its landing takes, lastFirst
	// the row at the top last frame.
	list      ui.ListState
	items     []diffItem
	jump      string
	settle    int
	lastFirst int
	folded    map[string]bool
	// remarks are the comments on lines waiting to go to the agent
	// together; writing is the line one is being written at.
	remarks     []remark
	writing     string
	remarkDraft string
	remarkInput ui.Handle

	// tree & in-pane file preview (Files tab)
	maximized      bool
	treeHidden     bool
	treeCwd        string
	treeOpen       map[string]bool
	treeCache      map[string][]dirItem
	treeList       ui.ListState
	previewPath    string
	previewRaw     bool
	previewLines   []string
	previewTok     [][]editor.Token
	previewErr     string
	previewLoading bool
	previewList    ui.ListState
}

// fileDiff is a file's change cut into hunks, computed once.
type fileDiff struct {
	hash           [32]byte
	status         byte // 'A', 'D' or 'M'
	hunks          []hunk
	old, new       []string
	oldTok, newTok [][]editor.Token // the lines' tokens, nil when the language is unknown
	added, removed int
	gaveUp         bool
}

func diffHash(d proto.ChangeDiff) [32]byte {
	h := sha256.New()
	if d.Before != nil {
		h.Write([]byte{1})
		h.Write([]byte(*d.Before))
	} else {
		h.Write([]byte{0})
	}
	if d.After != nil {
		h.Write([]byte{1})
		h.Write([]byte(*d.After))
	} else {
		h.Write([]byte{0})
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func newFileDiff(d proto.ChangeDiff) *fileDiff {
	f := &fileDiff{hash: diffHash(d), status: 'M'}
	switch {
	case d.Before == nil:
		f.status = 'A'
	case d.After == nil:
		f.status = 'D'
	}
	if d.Before != nil {
		f.old = diffLines(*d.Before)
	}
	if d.After != nil {
		f.new = diffLines(*d.After)
	}
	edits, ok := myers(f.old, f.new)
	f.gaveUp = !ok
	for _, e := range edits {
		switch e.op {
		case '+':
			f.added++
		case '-':
			f.removed++
		}
	}
	f.hunks = hunks(edits, diffContext)
	f.oldTok, f.newTok = tokenize(d.Path, f.old), tokenize(d.Path, f.new)
	return f
}

// tokenize highlights a file's lines as its language does.
func tokenize(path string, lines []string) [][]editor.Token {
	lang := editor.LanguageFor(path)
	if lang == nil || lang.Tokenizer == nil || len(lines) > 20000 {
		return nil
	}
	out := make([][]editor.Token, len(lines))
	var state uint64
	for i, l := range lines {
		out[i], state = lang.Tokenizer.Tokenize(strings.TrimRight(l, "\r\n"), state, nil)
	}
	return out
}

// syncReview asks for what the panel shows and the client lacks.
func (a *App) syncReview() {
	r := &a.rv
	if !a.C.CanAct() {
		return
	}
	if a.C.Workspace.BranchStale {
		a.quiet(a.C.ReadWorkspace())
	}
	ch := &a.C.Changes
	// The list is read while the panel is closed too: finished turns say
	// what they changed under them.
	if ch.Stale && !ch.Loading && (r.open || !a.C.Busy && slices.ContainsFunc(a.C.Turns, func(t client.Turn) bool { return t.Done })) {
		clear(r.asked)
		r.diffs.Clear()
		a.quiet(a.C.ListChanges())
		return
	}
	if !r.open {
		return
	}
	if ch.Loading {
		return
	}
	if r.wsRead != a.C.Workspace.Read {
		r.wsRead = a.C.Workspace.Read
		uncommittedPrefix := client.DiffKey(0, "")
		branchPrefix := client.DiffKey(-1, "")
		for _, prefix := range []string{uncommittedPrefix, branchPrefix} {
			maps.DeleteFunc(r.asked, func(k string, _ bool) bool { return strings.HasPrefix(k, prefix) })
		}
		liveUncommitted := make(map[string]bool, len(a.C.Workspace.Files))
		for _, f := range a.C.Workspace.Files {
			liveUncommitted[uncommittedPrefix+f.Path] = true
		}
		liveBranch := make(map[string]bool, len(a.C.Workspace.BaseFiles))
		for _, f := range a.C.Workspace.BaseFiles {
			liveBranch[branchPrefix+f.Path] = true
		}
		r.diffs.DeleteFunc(func(k string, _ *fileDiff) bool {
			if strings.HasPrefix(k, uncommittedPrefix) {
				return !liveUncommitted[k]
			}
			if strings.HasPrefix(k, branchPrefix) {
				return !liveBranch[k]
			}
			return false
		})
	}
	for _, t := range a.shownTurns() {
		for _, f := range t.Files {
			key := client.DiffKey(t.TurnSeq, f.Path)
			_, hasSrc := a.diffSource(key)
			if !r.asked[key] && !hasSrc {
				r.asked[key] = true
				switch t.TurnSeq {
				case 0:
					a.quiet(a.C.DiffUncommitted(f.Path))
				case -1:
					a.quiet(a.C.DiffBase(f.Path))
				default:
					a.quiet(a.C.DiffChange(t.TurnSeq, f.Path))
				}
			}
		}
	}
}

// shownTurns are what the panel lists: one turn, or the uncommitted
// files as turn 0.
func (a *App) shownTurns() []proto.ChangeTurn {
	if a.rv.branch {
		if len(a.C.Workspace.BaseFiles) == 0 {
			return nil
		}
		return []proto.ChangeTurn{{TurnSeq: -1, Files: a.C.Workspace.BaseFiles}}
	}
	if a.rv.uncommitted {
		if len(a.C.Workspace.Files) == 0 {
			return nil
		}
		return []proto.ChangeTurn{{Files: a.C.Workspace.Files}}
	}
	turns := a.C.Changes.Turns
	for _, t := range turns {
		if t.TurnSeq == a.rv.turn {
			return []proto.ChangeTurn{t}
		}
	}
	if len(turns) == 0 {
		return nil
	}
	return turns[len(turns)-1:]
}

// syncDiffBytes is how large a file's two sides can be, together, for
// its diff to be worked out on the UI thread: a small one shows in the
// frame that asks for it, and a large one comes a moment later rather
// than hold the window up.
const syncDiffBytes = 64 << 10

func (a *App) fileDiff(key string) *fileDiff {
	if a.rv.diffs == nil {
		a.rv.diffs = newLRU[string, *fileDiff](64)
	}
	d, hasSrc := a.diffSource(key)
	if f, cached := a.rv.diffs.Get(key); cached {
		if !hasSrc || f.hash == diffHash(d) {
			return f
		}
	}
	if !hasSrc {
		return nil
	}
	size := 0
	if d.Before != nil {
		size += len(*d.Before)
	}
	if d.After != nil {
		size += len(*d.After)
	}
	if size <= syncDiffBytes || a.update == nil {
		f := newFileDiff(d)
		a.rv.diffs.Put(key, f)
		return f
	}
	if a.rv.computing == nil {
		a.rv.computing = map[string]bool{}
	}
	if !a.rv.computing[key] {
		a.rv.computing[key] = true
		go func() {
			f := newFileDiff(d)
			a.update(func() {
				delete(a.rv.computing, key)
				// The file may have changed again meanwhile.
				if cur, ok := a.diffSource(key); ok && cur.Before == d.Before && cur.After == d.After {
					a.rv.diffs.Put(key, f)
				}
			})
		}()
	}
	return nil
}

// diffSource is the two sides of the file a diff key names.
func (a *App) diffSource(key string) (proto.ChangeDiff, bool) {
	if path, uncommitted := strings.CutPrefix(key, client.DiffKey(0, "")); uncommitted {
		d, ok := a.C.Workspace.Diffs[path]
		return d, ok
	}
	if path, branch := strings.CutPrefix(key, client.DiffKey(-1, "")); branch {
		d, ok := a.C.Workspace.BaseDiffs[path]
		return d, ok
	}
	d, ok := a.C.Changes.Diffs[key]
	return d, ok
}

// reviewToggle is the button in the bar that shows and hides the inspector.
func (a *App) reviewToggle(c *ui.Context) {
	p := a.p
	w := a.C.Workspace
	tip := locales.ShowChanges()
	if a.rv.open {
		tip = locales.HideChanges()
	}
	b := ui.ButtonBase(c.Key("review")).Row().Height(28).Padding(0, 10).Gap(6).Radius(8).AlignItems(ui.Center).
		Label(tip).Tooltip(tip + "  ⌘⌥I").Background(p.Chip)
	rim(b, p)
	press(b)
	focusRing(b, p)
	fg := p.Fg2
	if a.rv.open {
		b.Background(p.Sel)
		fg = p.Live
	} else if b.Hovered() {
		b.Background(p.Sel)
		fg = p.Fg
	}
	b.Children(func() {
		if w.Dirty || w.Added > 0 || w.Removed > 0 {
			if w.Added > 0 {
				ui.Text(c, fmt.Sprintf("+%d", w.Added)).Font("monospace").FontSize(12).TextColor(p.Plus).SingleLine()
			}
			if w.Removed > 0 {
				ui.Text(c, fmt.Sprintf("−%d", w.Removed)).Font("monospace").FontSize(12).TextColor(p.Minus).SingleLine()
			}
		}
		ui.Text(c, locales.Inspect()).FontSize(13).FontWeight(500).TextColor(fg).SingleLine()
		sym(c, "layout-sidebar-right", 15, fg)
	})
	if b.Clicked() {
		a.rv.open = !a.rv.open
	}
}

func (a *App) reviewPanel(c *ui.Context) {
	p := a.p
	turns := a.shownTurns()
	panel := ui.Column(c).Background(p.Sheet).Clip()
	if a.rv.maximized {
		panel.Grow(1).MinWidth(0)
		if a.sideOpen > 0.001 && !c.Vibrancy() {
			panel.BorderWidth(0, 0, 0, .5).BorderColor(p.Edge)
		}
	} else {
		panel.Width(a.reviewWidth()).Shrink(0).BorderWidth(0, 0, 0, .5).BorderColor(p.Edge)
	}
	panel.Children(func() {
		a.reviewTabs(c, fileCount(turns))
		switch a.rv.tab {
		case "term":
			a.terminalPane(c)
			return
		case "plan":
			a.planPane(c)
			return
		case "files":
			a.filesPane(c)
			return
		case "preview":
			a.browserPane(c)
			return
		}
		ui.Row(c).Height(40).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
			a.scope(c)
			ui.Spacer(c)
			if a.pierre.wv != nil {
				split := a.pierre.diffStyle == "split"
				label := "Unified"
				if split {
					label = "Split"
				}
				b := ui.ButtonBase(c.Key("rv-diff-style")).Row().Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center).Background(p.Chip).Label(label)
				press(b)
				focusRing(b, p)
				b.Children(func() {
					ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
				})
				if b.Clicked() {
					if split {
						a.pierre.diffStyle = "unified"
					} else {
						a.pierre.diffStyle = "split"
					}
					a.pierre.lastDiffsSig = ""
				}
			} else if fileCount(turns) > 1 {
				a.foldAll(c, turns)
			}
			add, del := a.totals(turns)
			ui.Row(c).Gap(6).Margin(0, 14, 0, 0).Children(func() {
				ui.Text(c, fmt.Sprintf("+%d", add)).Font("monospace").FontSize(13).TextColor(p.Plus)
				ui.Text(c, fmt.Sprintf("−%d", del)).Font("monospace").FontSize(13).TextColor(p.Minus)
			})
		})
		ch := a.C.Changes
		switch {
		case a.C.Current == nil:
			a.reviewNote(c, locales.NoOpenSession())
		case (a.rv.uncommitted || a.rv.branch) && a.C.Workspace.Branch == "":
			a.reviewNote(c, locales.NotGitRepo())
		case a.rv.uncommitted && len(turns) == 0:
			a.reviewNote(c, locales.NoUncommitted())
		case a.rv.branch && a.C.Workspace.Base == "":
			a.reviewNote(c, locales.NoBranchBase())
		case a.rv.branch && len(turns) == 0:
			a.reviewNote(c, locales.NoBranchChanges(a.C.Workspace.Base))
		case a.rv.uncommitted || a.rv.branch:
			if a.pierre.wv != nil {
				a.syncPierre("diffs", turns)
				ui.WebView(c, a.pierre.wv).Grow(1)
			} else {
				a.reviewFiles(c, turns)
				a.reviewDiff(c, turns)
			}
		case ch.Err != "" && len(ch.Turns) == 0:
			a.reviewNote(c, locales.ChangesLoadFailed(ch.Err))
		case len(ch.Turns) == 0 && (ch.Loading || ch.Stale):
			a.reviewNote(c, locales.Reading())
		case len(ch.Turns) == 0:
			a.reviewNote(c, locales.NoChangesYet())
		default:
			if a.pierre.wv != nil {
				a.syncPierre("diffs", turns)
				ui.WebView(c, a.pierre.wv).Grow(1)
			} else {
				a.reviewFiles(c, turns)
				a.reviewDiff(c, turns)
			}
		}
		if a.C.Current != nil {
			a.remarksBar(c)
			a.reviewFoot(c, turns)
		}
	})
}

func (a *App) reviewNote(c *ui.Context, s string) {
	ui.Column(c).Grow(1).Center().Children(func() {
		ui.Text(c, s).FontSize(13.5).TextColor(a.p.Mute)
	})
}

// scope is the segmented control between the last turn and all of them.
func (a *App) scope(c *ui.Context) {
	p := a.p
	ui.Row(c).Padding(2).Radius(8).Background(p.Chip).Margin(0, 12).Children(func() {
		base := locales.AgainstBranch()
		if b := a.C.Workspace.Base; b != "" {
			base = locales.AgainstBase(b)
		}
		for _, s := range []struct {
			label               string
			uncommitted, branch bool
		}{{locales.LastTurn(), false, false}, {locales.Uncommitted(), true, false}, {base, false, true}} {
			on := a.rv.uncommitted == s.uncommitted && a.rv.branch == s.branch
			b := ui.ButtonBase(c.Key(fmt.Sprintf("scope:%v:%v", s.uncommitted, s.branch))).Height(24).Padding(0, 10).Radius(6).Center().Label(s.label).Selected(on)
			col, weight := p.Fg2, 400
			if on {
				b.Background(p.Card).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .08)).Border(.5, p.Edge2)
				col, weight = p.Fg, 500
			}
			b.Children(func() { ui.Text(c, s.label).FontSize(13).FontWeight(weight).TextColor(col) })
			if b.Clicked() && !on {
				a.rv.uncommitted, a.rv.branch, a.rv.sel, a.rv.turn = s.uncommitted, s.branch, "", 0
				if s.uncommitted || s.branch {
					a.C.Workspace.BranchStale = true // read it as it is now
				}
			}
		}
	})
}

func fileCount(turns []proto.ChangeTurn) int {
	n := 0
	for _, t := range turns {
		n += len(t.Files)
	}
	return n
}

func (a *App) totals(turns []proto.ChangeTurn) (add, del int) {
	for _, t := range turns {
		for _, f := range t.Files {
			add, del = add+f.Added, del+f.Removed
		}
	}
	return
}

// turnLabel names a turn by the prompt that started it.
func (a *App) turnLabel(seq int64) string {
	for _, e := range a.C.Entries {
		if e.Role == client.User && e.Seq > seq {
			return e.Text
		}
	}
	return locales.ThisTurn()
}

func (a *App) reviewFiles(c *ui.Context, turns []proto.ChangeTurn) {
	p := a.p
	if a.rv.sel == "" || !slices.ContainsFunc(turns, func(t proto.ChangeTurn) bool {
		return slices.ContainsFunc(t.Files, func(f proto.ChangeFile) bool { return client.DiffKey(t.TurnSeq, f.Path) == a.rv.sel })
	}) {
		a.rv.sel = client.DiffKey(turns[0].TurnSeq, turns[0].Files[0].Path)
	}
	ui.Scroll(c).Shrink(0).MaxHeight(240).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		ui.Column(c).Padding(6, 8).Children(func() {
			for _, t := range turns {
				for _, f := range t.Files {
					a.fileRow(c, t.TurnSeq, f)
				}
			}
		})
	})
}

func (a *App) fileRow(c *ui.Context, turn int64, f proto.ChangeFile) {
	p := a.p
	key := client.DiffKey(turn, f.Path)
	on := a.rv.sel == key
	row := ui.ButtonBase(c.Key("file:"+key)).Row().Height(28).Padding(0, 8).Gap(8).Radius(6).AlignItems(ui.Center).Label(f.Path)
	focusRing(row, p)
	switch {
	case on:
		row.Background(p.Sel)
	case row.Hovered():
		row.Background(p.Hover)
	}
	if row.Clicked() {
		a.rv.sel, a.rv.jump = key, key
	}
	gone := false
	status, col := "M", p.Mute
	if d := a.fileDiff(key); d != nil {
		switch d.status {
		case 'A':
			status, col = "A", p.Plus
		case 'D':
			status, col = "D", p.Minus
			gone = true
		}
	}
	row.ContextMenu(func(m *ui.Menu) { a.fileContextMenu(c, m, f.Path, gone) })
	reverted := a.C.Changes.FileReverted(turn, f.Path)
	row.Children(func() {
		ui.Text(c, status).FontSize(11.5).FontWeight(600).TextColor(col).Width(12).TextAlign(ui.Center).Shrink(0)
		dir, base := a.splitPath(f.Path)
		ui.Row(c).Grow(1).MinWidth(0).Children(func() {
			if dir != "" {
				ui.Text(c, dir).FontSize(13.5).TextColor(p.Mute).SingleLine().TruncateMiddle().Shrink(1).MinWidth(0)
			}
			name := ui.Text(c, base).FontSize(13.5).TextColor(p.Fg).SingleLine().Shrink(0)
			if reverted {
				name.TextColor(p.Mute).Strikethrough()
			}
		})
		ui.Row(c).Gap(6).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf("+%d", f.Added)).Font("monospace").FontSize(12.5).TextColor(p.Plus).Opacity(nonzero(f.Added))
			ui.Text(c, fmt.Sprintf("−%d", f.Removed)).Font("monospace").FontSize(12.5).TextColor(p.Minus).Opacity(nonzero(f.Removed))
		})
	})
}

func nonzero(n int) float32 {
	if n == 0 {
		return 0
	}
	return 1
}

// splitPath shows a path relative to the workspace: its directory, with
// the trailing slash, and its name.
func (a *App) splitPath(path string) (dir, base string) {
	if cur := a.C.Current; cur != nil {
		if rel, err := filepath.Rel(cur.Cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	dir, base = filepath.Split(path)
	return dir, base
}

// diffItem is one row of the diffs pane: a file's head, a note, a hunk's
// head or a line. The pane is a List of them, so that only the rows in
// view are built, however long the diffs.
type diffItem struct {
	kind byte // 'f' file head, 'n' note, 'h' hunk head, 'l' line
	key  string
	turn int64
	file proto.ChangeFile
	d    *fileDiff
	hunk int
	e    edit
	note string
}

// diffItemID is a row's identity, for the List to keep its place by.
type diffItemID struct {
	key  string
	kind byte
	hunk int
	a, b int
}

func (it *diffItem) id() diffItemID {
	return diffItemID{it.key, it.kind, it.hunk, it.e.a, it.e.b}
}

// diffItems lays the files' diffs out as rows, at most maxDiffLines
// lines in all.
func (a *App) diffItems(turns []proto.ChangeTurn) []diffItem {
	items := a.rv.items[:0]
	shown := 0
	for _, t := range turns {
		for _, f := range t.Files {
			key := client.DiffKey(t.TurnSeq, f.Path)
			items = append(items, diffItem{kind: 'f', key: key, turn: t.TurnSeq, file: f})
			if a.rv.folded[key] {
				continue
			}
			note := func(s string, n int) {
				items = append(items, diffItem{kind: 'n', key: key, note: s, hunk: n})
			}
			d := a.fileDiff(key)
			switch {
			case d == nil:
				note(locales.Reading(), -1)
				continue
			case len(d.hunks) == 0:
				note(locales.NoContentChange(), -1)
				continue
			case shown >= maxDiffLines:
				note(locales.TooManyChanges(), -1)
				continue
			}
			if d.gaveUp {
				note(locales.TooManyChangesWhole(), -2)
			}
			for i, h := range d.hunks {
				if shown >= maxDiffLines {
					note(locales.MoreHunks(len(d.hunks)-i), i)
					break
				}
				items = append(items, diffItem{kind: 'h', key: key, turn: t.TurnSeq, file: f, d: d, hunk: i})
				for _, e := range h.edits {
					if shown >= maxDiffLines {
						break
					}
					items = append(items, diffItem{kind: 'l', key: key, turn: t.TurnSeq, file: f, d: d, hunk: i, e: e})
					shown++
				}
			}
		}
	}
	a.rv.items = items
	return items
}

// reviewDiff shows the files' diffs one under another; picking a file
// scrolls to it, and scrolling picks the file at the top.
func (a *App) reviewDiff(c *ui.Context, turns []proto.ChangeTurn) {
	if a.rv.folded == nil {
		a.rv.folded = map[string]bool{}
	}
	items := a.diffItems(turns)
	st := &a.rv.list
	st.Key = func(i int) any { return a.rv.items[i].id() }
	first, _ := st.Visible()
	switch {
	case a.rv.jump != "":
		for i := range items {
			if items[i].kind == 'f' && items[i].key == a.rv.jump {
				st.ScrollTo(i, ui.Start)
				break
			}
		}
		a.rv.jump, a.rv.settle = "", 2
	case a.rv.settle > 0:
		// The jump lands as the next frames are laid out.
		a.rv.settle--
		a.rv.lastFirst = first
	case first != a.rv.lastFirst && first >= 0 && first < len(items):
		a.rv.lastFirst = first
		a.rv.sel = items[first].key
	}
	ui.List(c, st, len(items), func(i int) { a.diffItemView(c, &a.rv.items[i]) }).Grow(1).MinHeight(0)
}

// diffItemView shows one row of the diffs pane.
func (a *App) diffItemView(c *ui.Context, it *diffItem) {
	p := a.p
	switch it.kind {
	case 'f':
		a.fileHead(c, it.key, it.file)
	case 'n':
		pad := float32(6)
		if it.hunk >= 0 {
			pad = 8
		}
		ui.Text(c, it.note).FontSize(13).TextColor(p.Mute).Padding(pad, 14)
	case 'h':
		h := it.d.hunks[it.hunk]
		ui.Row(c).Height(30).Padding(0, 8, 0, 14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, h.head()).Font("monospace").FontSize(12.5).TextColor(p.Mute).SingleLine().Grow(1).MinWidth(0)
			if it.turn > 0 && !a.C.Changes.FileReverted(it.turn, it.file.Path) {
				b := a.barButton(c, "hunk", "arrow-back-up", locales.UndoHunk(), func() {}).Height(24)
				b.Disabled(a.C.Busy).Tooltip(map[bool]string{true: locales.CantRestoreWhileRunning()}[a.C.Busy])
				if b.Clicked() {
					a.do(a.C.RevertHunk(it.turn, it.file.Path, h.oldStart, h.oldLen, h.newStart, h.newLen))
				}
			}
		})
	case 'l':
		a.diffRow(c, it.file.Path, it.d, it.e)
	}
}

// foldAll folds every file's diff, or opens them all when all are.
func (a *App) foldAll(c *ui.Context, turns []proto.ChangeTurn) {
	all := fileCount(turns) > 0
	for _, t := range turns {
		for _, f := range t.Files {
			all = all && a.rv.folded[client.DiffKey(t.TurnSeq, f.Path)]
		}
	}
	label, symbol := locales.CollapseAll(), "fold"
	if all {
		label, symbol = locales.ExpandAll(), "fold-down"
	}
	if a.iconPill(c, "fold-all", symbol, label).Clicked() {
		if a.rv.folded == nil {
			a.rv.folded = map[string]bool{}
		}
		for _, t := range turns {
			for _, f := range t.Files {
				a.rv.folded[client.DiffKey(t.TurnSeq, f.Path)] = !all
			}
		}
	}
}

// fileHead names a file over its diff: clicked, it folds the diff, and
// its name opens the file in the editor.
func (a *App) fileHead(c *ui.Context, key string, f proto.ChangeFile) {
	p := a.p
	folded := a.rv.folded[key]
	dir, base := a.splitPath(f.Path)
	head := ui.ButtonBase(c.Key("head:"+key)).Row().Height(32).Padding(0, 12, 0, 8).Gap(6).AlignItems(ui.Center).
		Label(dir+base).Expanded(!folded).Background(p.Sheet).BorderWidth(.5, 0).BorderColor(p.Edge)
	focusRing(head, p)
	if head.Hovered() {
		head.Background(p.Hover)
	}
	gone := false
	if d := a.fileDiff(key); d != nil && d.status == 'D' {
		gone = true
	}
	head.ContextMenu(func(m *ui.Menu) { a.fileContextMenu(c, m, f.Path, gone) })
	head.Children(func() {
		sym(c, map[bool]string{true: "chevron-right", false: "chevron-down"}[folded], 14, p.Mute)
		name := ui.ButtonBase(c.Key("name:" + key)).Row().MinWidth(0).Shrink(1).Label(locales.OpenInEditor()).Tooltip(locales.OpenInEditor()).Disabled(gone)
		hover := name.Hovered() && !gone
		name.Children(func() {
			if dir != "" {
				ui.Text(c, dir).FontSize(13).TextColor(p.Mute).SingleLine().TruncateMiddle().Shrink(1).MinWidth(0)
			}
			t := ui.Text(c, base).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine().Shrink(0)
			if hover {
				t.Underline()
			}
		})
		if name.Clicked() {
			edLabel, edApp := a.preferredEditor()
			if err := a.openIn(edApp, f.Path); err != nil {
				if edLabel == "" {
					edLabel = filepath.Base(f.Path)
				}
				a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
			}
		}
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf("+%d", f.Added)).Font("monospace").FontSize(12.5).TextColor(p.Plus).Opacity(nonzero(f.Added))
		ui.Text(c, fmt.Sprintf("−%d", f.Removed)).Font("monospace").FontSize(12.5).TextColor(p.Minus).Opacity(nonzero(f.Removed))
	})
	if head.Clicked() {
		a.rv.folded[key] = !folded
	}
}

func (a *App) fileContextMenu(c *ui.Context, m *ui.Menu, path string, gone bool) {
	openItem := m.Item(locales.OpenInEditor())
	if gone {
		openItem.Disabled(true)
	}
	if openItem.Chosen() && !gone {
		edLabel, edApp := a.preferredEditor()
		if err := a.openIn(edApp, path); err != nil {
			if edLabel == "" {
				edLabel = filepath.Base(path)
			}
			a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
		}
	}
	if m.Item(locales.RevealInFinder()).Chosen() {
		_ = exec.Command("open", "-R", path).Start()
	}
	if m.Item(locales.CopyPath()).Chosen() {
		dir, base := a.splitPath(path)
		c.WriteClipboard(dir + base)
	}
}

// diffRow is one line: old and new line numbers, the sign and the text.
func (a *App) diffRow(c *ui.Context, path string, d *fileDiff, e edit) {
	p := a.p
	var bg, signCol ui.Color
	text, sign := "", ""
	var toks []editor.Token
	switch e.op {
	case '+':
		bg, signCol, sign, text = p.Plus.Alpha(.10), p.Plus, "+", d.new[e.b]
		if d.newTok != nil {
			toks = d.newTok[e.b]
		}
	case '-':
		bg, signCol, sign, text = p.Minus.Alpha(.10), p.Minus, "−", d.old[e.a]
		if d.oldTok != nil {
			toks = d.oldTok[e.a]
		}
	default:
		text = d.new[e.b]
		if d.newTok != nil {
			toks = d.newTok[e.b]
		}
	}
	num := func(i int) string {
		if i < 0 {
			return ""
		}
		return fmt.Sprint(i + 1)
	}
	at := lineRemark(path, d, e)
	row := ui.Row(c.Key(fmt.Sprintf("line:%d:%d", e.a, e.b))).Height(diffRowHeight).AlignItems(ui.Center)
	if e.op != ' ' {
		row.Background(bg)
	}
	hover := row.Hovered()
	row.Children(func() {
		for i, n := range []string{num(e.a), num(e.b)} {
			if i == 1 && (hover || a.rv.writing == at.at()) {
				// The new line's number gives way to a + that comments on it.
				b := ui.ButtonBase(c.Key("remark")).Size(38, diffRowHeight).Shrink(0).Center().Label(locales.AddRemark()).Tooltip(locales.AddRemark())
				b.Children(func() {
					ui.Box(c).Size(18, 18).Radius(5).Background(p.Acc).Center().Children(func() { sym(c, "plus", 12, p.OnAcc) })
				})
				if b.Clicked() {
					a.writeRemark(c, at)
				}
				continue
			}
			ui.Text(c, n).Font("monospace").FontSize(13).TextColor(p.Faint).Width(38).TextAlign(ui.End).Padding(0, 4, 0, 0).Shrink(0).SingleLine()
		}
		ui.Text(c, sign).Font("monospace").FontSize(13).TextColor(signCol).Width(16).TextAlign(ui.Center).Shrink(0)
		ui.RichText(c, a.codeSpans(c, strings.TrimRight(text, "\r\n"), toks)...).SingleLine().PreserveSpaces().
			Padding(0, 6).Grow(1).MinWidth(0).Selectable()
	})
	a.remarksAt(c, at)
}

// reviewFoot commits the folder, and takes back or redoes the turn of
// the file shown.
func (a *App) reviewFoot(c *ui.Context, turns []proto.ChangeTurn) {
	p := a.p
	var turn int64
	for _, t := range turns {
		for _, f := range t.Files {
			if client.DiffKey(t.TurnSeq, f.Path) == a.rv.sel {
				turn = t.TurnSeq
			}
		}
	}
	w := a.C.Workspace
	dirty := w.Dirty
	if turn <= 0 && !dirty && w.Ahead == 0 && w.PR == nil {
		return
	}
	reverted := a.C.Changes.Reverted[turn]
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		if dirty {
			a.commitField(c)
		} else {
			note := ""
			if turn > 0 {
				note = a.turnLabel(turn)
				if reverted {
					note = locales.Undone(note)
				}
			} else if w.Ahead > 0 {
				note = locales.AheadCommits(w.Ahead)
			} else if w.PR != nil {
				note = w.PR.Title
			}
			if note != "" {
				ui.Row(c).Height(28).Grow(1).MinWidth(0).Radius(7).Background(p.Chip).Padding(0, 10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, note).FontSize(13).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
				})
			} else {
				ui.Spacer(c)
			}
		}
		if turn > 0 {
			label := locales.UndoTurn()
			if reverted {
				label = locales.RedoTurn()
			}
			b := a.button(c, "revert", label, "", !reverted && !dirty && w.Ahead == 0)
			b.Disabled(a.C.Busy)
			if a.C.Busy {
				b.Tooltip(locales.CantRestoreWhileRunning())
			}
			if b.Clicked() {
				a.do(a.C.Revert(turn, "", reverted))
			}
		}
		switch {
		case dirty:
			a.commitGo(c)
		case w.PR != nil:
			a.prButton(c)
		case w.Ahead > 0:
			pushBtn := a.button(c, "push", locales.Push(), "", w.Branch == w.Base)
			pushBtn.Disabled(w.Pushing || a.C.Busy)
			if pushBtn.Clicked() {
				a.do(a.C.Push())
			}
			if w.Branch != "" && w.Branch != w.Base {
				prBtn := a.button(c, "create-pr", locales.CreatePr(), "", true)
				prBtn.Disabled(w.Pushing || a.C.Busy)
				if prBtn.Clicked() {
					a.do(a.C.CreatePR(a.prTitle(), ""))
				}
			}
		}
	})
}

// codeSpans colors a line of code by its tokens, as code blocks are.
func (a *App) codeSpans(c *ui.Context, text string, toks []editor.Token) []ui.Span {
	p := a.p
	span := func(s string, st editor.TokenStyle) ui.Span {
		sp := ui.Span{Text: s, Font: "monospace", Size: 12, Color: p.Fg, Italic: st.Italic}
		if st.Color != (ui.Color{}) {
			sp.Color = st.Color
		}
		if st.Bold {
			sp.Weight = 600
		}
		return sp
	}
	if len(toks) == 0 {
		return []ui.Span{span(text, editor.TokenStyle{})}
	}
	styles := editor.DefaultTheme(c.Theme()).Tokens
	var out []ui.Span
	at := 0
	for _, t := range toks {
		end := min(t.End, len(text))
		if end <= at {
			continue
		}
		var st editor.TokenStyle
		if int(t.Kind) < len(styles) {
			st = styles[t.Kind]
		}
		out = append(out, span(text[at:end], st))
		at = end
	}
	if at < len(text) {
		out = append(out, span(text[at:], editor.TokenStyle{}))
	}
	return out
}
