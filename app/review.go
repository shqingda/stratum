package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/shqingda/talos/client"
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
	open  bool
	tab   string // "", "term" or "plan"
	all   bool   // every turn, not just the last
	sel   string // client.DiffKey of the file shown
	asked map[string]bool
	diffs map[string]*fileDiff
	// The diffs show one under another: jump is the file to scroll to,
	// tops where each file's diff starts.
	scroll ui.ScrollState
	jump   string
	tops   map[string]float32
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
	for _, t := range a.shownTurns() {
		for _, f := range t.Files {
			key := client.DiffKey(t.TurnSeq, f.Path)
			if _, ok := ch.Diffs[key]; !ok && !r.asked[key] {
				r.asked[key] = true
				a.quiet(a.C.DiffChange(t.TurnSeq, f.Path))
			}
		}
	}
}

// shownTurns are the turns the panel lists, newest first.
func (a *App) shownTurns() []proto.ChangeTurn {
	turns := a.C.Changes.Turns
	if len(turns) == 0 {
		return nil
	}
	if !a.rv.all {
		return turns[len(turns)-1:]
	}
	out := slices.Clone(turns)
	slices.Reverse(out)
	return out
}

func (a *App) fileDiff(key string) *fileDiff {
	if f, ok := a.rv.diffs[key]; ok {
		return f
	}
	d, ok := a.C.Changes.Diffs[key]
	if !ok {
		return nil
	}
	f := newFileDiff(d)
	a.rv.diffs[key] = f
	return f
}

// reviewToggle is the button in the bar that shows and hides the panel.
func (a *App) reviewToggle(c *ui.Context) {
	label := "显示变更"
	if a.rv.open {
		label = "隐藏变更"
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
	ui.Column(c).Width(reviewWidth).Shrink(0).Background(p.Pane).BorderWidth(0, 0, 0, .5).BorderColor(p.Edge).Children(func() {
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
			add, del := a.totals(turns)
			ui.Row(c).Gap(6).Margin(0, 14, 0, 0).Children(func() {
				ui.Text(c, fmt.Sprintf("+%d", add)).Font("monospace").FontSize(12).TextColor(p.Plus)
				ui.Text(c, fmt.Sprintf("−%d", del)).Font("monospace").FontSize(12).TextColor(p.Minus)
			})
		})
		ch := a.C.Changes
		switch {
		case a.C.Current == nil:
			a.reviewNote(c, "没有打开的会话")
		case ch.Err != "" && len(ch.Turns) == 0:
			a.reviewNote(c, "读取变更失败："+ch.Err)
		case len(ch.Turns) == 0 && (ch.Loading || ch.Stale):
			a.reviewNote(c, "正在读取…")
		case len(ch.Turns) == 0:
			a.reviewNote(c, "这个会话还没有改动文件")
		default:
			a.reviewFiles(c, turns)
			a.reviewDiff(c, turns)
		}
		if a.C.Current != nil {
			a.reviewFoot(c, turns)
		}
	})
}

func (a *App) reviewNote(c *ui.Context, s string) {
	ui.Column(c).Grow(1).Center().Children(func() {
		ui.Text(c, s).FontSize(12.5).TextColor(a.p.Mute)
	})
}

// scope is the segmented control between the last turn and all of them.
func (a *App) scope(c *ui.Context) {
	p := a.p
	ui.Row(c).Padding(2).Radius(8).Background(p.Chip).Margin(0, 12).Children(func() {
		for _, s := range []struct {
			label string
			all   bool
		}{{"本轮", false}, {"全部", true}} {
			on := a.rv.all == s.all
			b := ui.ButtonBase(c.Key("scope:"+s.label)).Height(24).Padding(0, 10).Radius(6).Center().Label(s.label).Selected(on)
			col, weight := p.Fg2, 400
			if on {
				b.Background(p.Card).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .08)).Border(.5, p.Edge2)
				col, weight = p.Fg, 500
			}
			b.Children(func() { ui.Text(c, s.label).FontSize(12).FontWeight(weight).TextColor(col) })
			if b.Clicked() && !on {
				a.rv.all, a.rv.sel = s.all, ""
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
	return "这一轮"
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
				if a.rv.all {
					ui.Row(c).Height(26).Padding(0, 8).Gap(6).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
						ui.Text(c, a.turnLabel(t.TurnSeq)).FontSize(11.5).FontWeight(600).TextColor(p.Mute).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
						if a.C.Changes.Reverted[t.TurnSeq] {
							ui.Text(c, "已撤销").FontSize(11).TextColor(p.Mute)
						}
					})
				}
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
		ui.Text(c, status).FontSize(10.5).FontWeight(600).TextColor(col).Width(12).TextAlign(ui.Center).Shrink(0)
		dir, base := a.splitPath(f.Path)
		ui.Row(c).Grow(1).MinWidth(0).Children(func() {
			if dir != "" {
				ui.Text(c, dir).FontSize(12.5).TextColor(p.Mute).SingleLine().TruncateMiddle().Shrink(1).MinWidth(0)
			}
			name := ui.Text(c, base).FontSize(12.5).TextColor(p.Fg).SingleLine().Shrink(0)
			if reverted {
				name.TextColor(p.Mute).Strikethrough()
			}
		})
		ui.Row(c).Gap(6).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf("+%d", f.Added)).Font("monospace").FontSize(11.5).TextColor(p.Plus).Opacity(nonzero(f.Added))
			ui.Text(c, fmt.Sprintf("−%d", f.Removed)).Font("monospace").FontSize(11.5).TextColor(p.Minus).Opacity(nonzero(f.Removed))
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
// scrolls to it.
func (a *App) reviewDiff(c *ui.Context, turns []proto.ChangeTurn) {
	if a.rv.tops == nil {
		a.rv.tops = map[string]float32{}
	}
	ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.rv.scroll).Children(func() {
		var secs []ui.Element
		var keys []string
		inner := ui.Column(c).Padding(10, 0).Children(func() {
			shown, first := 0, true
			for _, t := range turns {
				for _, f := range t.Files {
					key := client.DiffKey(t.TurnSeq, f.Path)
					name := ""
					if !first {
						_, name = a.splitPath(f.Path)
					}
					sec := ui.Column(c.Key("diff:"+key)).Margin(map[bool]float32{true: 0, false: 14}[first], 0, 0, 0).Children(func() {
						shown = a.fileDiffView(c, key, name, shown)
					})
					secs, keys = append(secs, sec), append(keys, key)
					first = false
				}
			}
		})
		if top := inner.Bounds().Y; inner.Bounds().H > 0 {
			for i, sec := range secs {
				a.rv.tops[keys[i]] = sec.Bounds().Y - top
			}
			if y, ok := a.rv.tops[a.rv.jump]; ok {
				a.rv.scroll.ScrollTo(0, y, ui.Smooth)
				a.rv.jump = ""
			}
		}
	})
}

// fileDiffView is one file's hunks, named in the first hunk's header
// when name is set, and returns how many lines show so far.
func (a *App) fileDiffView(c *ui.Context, key, name string, shown int) int {
	p := a.p
	note := func(s string) {
		head := s
		if name != "" {
			head = name + " · " + s
		}
		ui.Text(c, head).FontSize(12).TextColor(p.Mute).Padding(6, 14)
	}
	d := a.fileDiff(key)
	switch {
	case d == nil:
		note("正在读取…")
		return shown
	case len(d.hunks) == 0:
		note("内容没有变化")
		return shown
	case shown >= maxDiffLines:
		note("改动太多，没有显示")
		return shown
	}
	if d.gaveUp {
		note("改动太多，按整体替换显示")
	}
	for i, h := range d.hunks {
		if shown >= maxDiffLines {
			ui.Text(c, fmt.Sprintf("还有 %d 处改动未显示", len(d.hunks)-i)).FontSize(12).TextColor(p.Mute).Padding(8, 14)
			break
		}
		ui.Row(c.Key(i)).Height(30).Padding(0, 8, 0, 14).AlignItems(ui.Center).Margin(map[bool]float32{true: 0, false: 14}[i == 0], 0, 0, 0).Children(func() {
			head := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.oldStart, h.oldLen, h.newStart, h.newLen)
			if i == 0 && name != "" {
				head = name + " · " + head
			}
			ui.Text(c, head).Font("monospace").FontSize(11.5).TextColor(p.Mute).SingleLine()
		})
		for _, e := range h.edits {
			if shown >= maxDiffLines {
				break
			}
			a.diffRow(c, d, e)
			shown++
		}
	}
	return shown
}

// diffRow is one line: old and new line numbers, the sign and the text.
func (a *App) diffRow(c *ui.Context, d *fileDiff, e edit) {
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
	row := ui.Row(c).Height(diffRowHeight).AlignItems(ui.Center)
	if e.op != ' ' {
		row.Background(bg)
	}
	row.Children(func() {
		for _, n := range []string{num(e.a), num(e.b)} {
			ui.Text(c, n).Font("monospace").FontSize(12).TextColor(p.Faint).Width(38).TextAlign(ui.End).Padding(0, 4, 0, 0).Shrink(0).SingleLine()
		}
		ui.Text(c, sign).Font("monospace").FontSize(12).TextColor(signCol).Width(16).TextAlign(ui.Center).Shrink(0)
		ui.RichText(c, a.codeSpans(c, strings.TrimRight(text, "\r\n"), toks)...).SingleLine().PreserveSpaces().
			Padding(0, 6).Grow(1).MinWidth(0).Selectable()
	})
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
	if turn == 0 && !dirty {
		return
	}
	reverted := a.C.Changes.Reverted[turn]
	ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		if dirty {
			a.commitField(c)
		} else {
			note := a.turnLabel(turn)
			if reverted {
				note = "已撤销：" + note
			}
			ui.Row(c).Height(28).Grow(1).MinWidth(0).Radius(7).Background(p.Chip).Padding(0, 10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, note).FontSize(12.5).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		if turn != 0 {
			label := "撤销本轮"
			if reverted {
				label = "重做本轮"
			}
			b := a.button(c, "revert", label, "", !reverted && !dirty)
			b.Disabled(a.C.Busy)
			if a.C.Busy {
				b.Tooltip("运行中不能恢复文件")
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
