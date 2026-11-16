package app

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
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
	open bool
	tab  string // "", "term" or "plan"
	// uncommitted shows what git has not got, in place of a turn: turn,
	// or the last one when 0.
	uncommitted bool
	// branch shows what the branch changed since its base, as turn -1.
	branch bool
	turn   int64
	wsRead int
	sel    string // client.DiffKey of the file shown
	asked  map[string]bool
	diffs  map[string]*fileDiff
	// The diffs show one under another: jump is the file to scroll to,
	// tops where each file's diff starts.
	scroll ui.ScrollState
	jump   string
	tops   map[string]float32
	lastY  float32 // where the diffs were scrolled to last frame
	folded map[string]bool
	// remarks are the comments on lines waiting to go to the agent
	// together; writing is the line one is being written at.
	remarks     []remark
	writing     string
	remarkDraft string
	remarkInput ui.Handle
}

// fileDiff is a file's change cut into hunks, computed once.
type fileDiff struct {
	status         byte // 'A', 'D' or 'M'
	hunks          []hunk
	old, new       []string
	oldTok, newTok [][]editor.Token // the lines' tokens, nil when the language is unknown
	added, removed int
	gaveUp         bool
}

func newFileDiff(d proto.ChangeDiff) *fileDiff {
	f := &fileDiff{status: 'M'}
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
		clear(r.diffs)
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
		for _, prefix := range []string{client.DiffKey(0, ""), client.DiffKey(-1, "")} {
			maps.DeleteFunc(r.diffs, func(k string, _ *fileDiff) bool { return strings.HasPrefix(k, prefix) })
			maps.DeleteFunc(r.asked, func(k string, _ bool) bool { return strings.HasPrefix(k, prefix) })
		}
	}
	for _, t := range a.shownTurns() {
		for _, f := range t.Files {
			key := client.DiffKey(t.TurnSeq, f.Path)
			if !r.asked[key] && a.fileDiff(key) == nil {
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

func (a *App) fileDiff(key string) *fileDiff {
	if f, ok := a.rv.diffs[key]; ok {
		return f
	}
	d, ok := a.C.Changes.Diffs[key]
	if path, uncommitted := strings.CutPrefix(key, client.DiffKey(0, "")); uncommitted {
		d, ok = a.C.Workspace.Diffs[path]
	}
	if path, branch := strings.CutPrefix(key, client.DiffKey(-1, "")); branch {
		d, ok = a.C.Workspace.BaseDiffs[path]
	}
	if !ok {
		return nil
	}
	f := newFileDiff(d)
	a.rv.diffs[key] = f
	return f
}

// reviewToggle is the button in the bar that shows and hides the panel.
func (a *App) reviewToggle(c *ui.Context) {
	label := locales.ShowChanges()
	if a.rv.open {
		label = locales.HideChanges()
	}
	b := a.iconButton(c, "review", "layout-sidebar-right", label)
	if a.rv.open {
		b.Background(a.p.Sel)
	}
	if b.Clicked() {
		a.rv.open = !a.rv.open
	}
}

func (a *App) reviewPanel(c *ui.Context) {
	p := a.p
	turns := a.shownTurns()
	panel := ui.Column(c).Width(reviewWidth).Shrink(0).Background(p.Sheet).Clip().BorderWidth(0, 0, 0, .5).BorderColor(p.Edge)
	panel.Children(func() {
		a.reviewTabs(c, fileCount(turns))
		switch a.rv.tab {
		case "term":
			a.terminalPane(c)
			return
		case "plan":
			a.planPane(c)
			return
		}
		ui.Row(c).Height(40).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
			a.scope(c)
			ui.Spacer(c)
			if fileCount(turns) > 1 {
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
			a.reviewFiles(c, turns)
			a.reviewDiff(c, turns)
		case ch.Err != "" && len(ch.Turns) == 0:
			a.reviewNote(c, locales.ChangesLoadFailed(ch.Err))
		case len(ch.Turns) == 0 && (ch.Loading || ch.Stale):
			a.reviewNote(c, locales.Reading())
		case len(ch.Turns) == 0:
			a.reviewNote(c, locales.NoChangesYet())
		default:
			a.reviewFiles(c, turns)
			a.reviewDiff(c, turns)
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
	switch {
	case on:
		row.Background(p.Sel)
	case row.Hovered():
		row.Background(p.Hover)
	}
	if row.Clicked() {
		a.rv.sel, a.rv.jump = key, key
	}
	status, col := "M", p.Mute
	if d := a.fileDiff(key); d != nil {
		switch d.status {
		case 'A':
			status, col = "A", p.Plus
		case 'D':
			status, col = "D", p.Minus
		}
	}
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

// reviewDiff shows the files' diffs one under another; picking a file
// scrolls to it, and scrolling picks the file at the top.
func (a *App) reviewDiff(c *ui.Context, turns []proto.ChangeTurn) {
	if a.rv.tops == nil {
		a.rv.tops, a.rv.folded = map[string]float32{}, map[string]bool{}
	}
	ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.rv.scroll).Children(func() {
		var secs []ui.Element
		var keys []string
		inner := ui.Column(c).Padding(6, 0, 10, 0).Children(func() {
			shown := 0
			for _, t := range turns {
				for _, f := range t.Files {
					key := client.DiffKey(t.TurnSeq, f.Path)
					sec := ui.Column(c.Key("diff:" + key)).Children(func() {
						a.fileHead(c, key, f)
						if !a.rv.folded[key] {
							shown = a.fileDiffView(c, t.TurnSeq, f.Path, shown)
						}
					})
					secs, keys = append(secs, sec), append(keys, key)
				}
			}
		})
		if top := inner.Bounds().Y; inner.Bounds().H > 0 {
			for i, sec := range secs {
				a.rv.tops[keys[i]] = sec.Bounds().Y - top
			}
			y := a.rv.scroll.Y
			if to, ok := a.rv.tops[a.rv.jump]; ok {
				a.rv.scroll.ScrollTo(0, to, ui.Smooth)
				a.rv.jump = ""
			} else if y != a.rv.lastY && !a.rv.scroll.Scrolling() {
				for _, k := range keys {
					if a.rv.tops[k] <= y+8 {
						a.rv.sel = k
					}
				}
			}
			a.rv.lastY = y
		}
	})
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
	if head.Hovered() {
		head.Background(p.Hover)
	}
	head.Children(func() {
		sym(c, map[bool]string{true: "chevron-right", false: "chevron-down"}[folded], 14, p.Mute)
		gone := false
		if d := a.fileDiff(key); d != nil && d.status == 'D' {
			gone = true
		}
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
			if err := a.openIn("Visual Studio Code", f.Path); err != nil {
				a.err = locales.CouldntOpen("VS Code", err.Error())
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

// fileDiffView is one file's hunks, and returns how many lines show so
// far.
func (a *App) fileDiffView(c *ui.Context, turn int64, path string, shown int) int {
	p := a.p
	key := client.DiffKey(turn, path)
	note := func(s string) {
		ui.Text(c, s).FontSize(13).TextColor(p.Mute).Padding(6, 14)
	}
	d := a.fileDiff(key)
	switch {
	case d == nil:
		note(locales.Reading())
		return shown
	case len(d.hunks) == 0:
		note(locales.NoContentChange())
		return shown
	case shown >= maxDiffLines:
		note(locales.TooManyChanges())
		return shown
	}
	if d.gaveUp {
		note(locales.TooManyChangesWhole())
	}
	for i, h := range d.hunks {
		if shown >= maxDiffLines {
			ui.Text(c, locales.MoreHunks(len(d.hunks)-i)).FontSize(13).TextColor(p.Mute).Padding(8, 14)
			break
		}
		ui.Row(c.Key(i)).Height(30).Padding(0, 8, 0, 14).AlignItems(ui.Center).Children(func() {
			head := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.oldStart, h.oldLen, h.newStart, h.newLen)
			ui.Text(c, head).Font("monospace").FontSize(12.5).TextColor(p.Mute).SingleLine().Grow(1).MinWidth(0)
			if turn > 0 && !a.C.Changes.FileReverted(turn, path) {
				b := a.barButton(c, fmt.Sprintf("hunk:%s:%d", key, i), "arrow-back-up", locales.UndoHunk(), func() {}).Height(24)
				b.Disabled(a.C.Busy).Tooltip(map[bool]string{true: locales.CantRestoreWhileRunning()}[a.C.Busy])
				if b.Clicked() {
					a.do(a.C.RevertHunk(turn, path, h.oldStart, h.oldLen, h.newStart, h.newLen))
				}
			}
		})
		for _, e := range h.edits {
			if shown >= maxDiffLines {
				break
			}
			a.diffRow(c, path, d, e)
			shown++
		}
	}
	return shown
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
	dirty := a.C.Workspace.Dirty
	if turn <= 0 && !dirty {
		return
	}
	reverted := a.C.Changes.Reverted[turn]
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		if dirty {
			a.commitField(c)
		} else {
			note := a.turnLabel(turn)
			if reverted {
				note = locales.Undone(note)
			}
			ui.Row(c).Height(28).Grow(1).MinWidth(0).Radius(7).Background(p.Chip).Padding(0, 10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, note).FontSize(13.5).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		if turn > 0 {
			label := locales.UndoTurn()
			if reverted {
				label = locales.RedoTurn()
			}
			b := a.button(c, "revert", label, "", !reverted && !dirty)
			b.Disabled(a.C.Busy)
			if a.C.Busy {
				b.Tooltip(locales.CantRestoreWhileRunning())
			}
			if b.Clicked() {
				a.do(a.C.Revert(turn, "", reverted))
			}
		}
		if dirty {
			a.commitGo(c)
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
	styles := mdTheme(p, c.Theme()).CodeColors.Tokens
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
