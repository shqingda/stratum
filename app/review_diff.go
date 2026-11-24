package app

import (
	"crypto/sha256"
	"io"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

// fileDiff is a file's change cut into hunks, computed once.
type fileDiff struct {
	hash                  [32]byte
	hasBefore, hasAfter   bool
	beforeData, afterData *byte
	beforeLen, afterLen   int
	status                byte // 'A', 'D' or 'M'
	hunks                 []hunk
	old, new              []string
	oldTok, newTok        [][]editor.Token // the lines' tokens, nil when the language is unknown
	spanTheme             *ui.Theme
	oldSpans, newSpans    [][]ui.Span
	added, removed        int
	gaveUp                bool
}

func setDiffSource(f *fileDiff, d proto.ChangeDiff) {
	f.hasBefore = d.Before != nil
	if d.Before != nil {
		f.beforeData = unsafe.StringData(*d.Before)
		f.beforeLen = len(*d.Before)
	} else {
		f.beforeData, f.beforeLen = nil, 0
	}
	f.hasAfter = d.After != nil
	if d.After != nil {
		f.afterData = unsafe.StringData(*d.After)
		f.afterLen = len(*d.After)
	} else {
		f.afterData, f.afterLen = nil, 0
	}
}

func sameDiffSource(f *fileDiff, d proto.ChangeDiff) bool {
	if f.hasBefore != (d.Before != nil) || f.hasAfter != (d.After != nil) {
		return false
	}
	if d.Before != nil && (f.beforeLen != len(*d.Before) || f.beforeData != unsafe.StringData(*d.Before)) {
		return false
	}
	if d.After != nil && (f.afterLen != len(*d.After) || f.afterData != unsafe.StringData(*d.After)) {
		return false
	}
	return true
}

func diffHash(d proto.ChangeDiff) [32]byte {
	h := sha256.New()
	if d.Before != nil {
		h.Write([]byte{1})
		_, _ = io.WriteString(h, *d.Before)
	} else {
		h.Write([]byte{0})
	}
	if d.After != nil {
		h.Write([]byte{1})
		_, _ = io.WriteString(h, *d.After)
	} else {
		h.Write([]byte{0})
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func newFileDiff(d proto.ChangeDiff) *fileDiff {
	f := &fileDiff{hash: diffHash(d), status: 'M'}
	setDiffSource(f, d)
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
	needOld := make([]bool, len(f.old))
	needNew := make([]bool, len(f.new))
	for _, h := range f.hunks {
		for _, e := range h.edits {
			if e.a >= 0 && e.a < len(needOld) {
				needOld[e.a] = true
			}
			if e.b >= 0 && e.b < len(needNew) {
				needNew[e.b] = true
			}
		}
	}
	f.oldTok, f.newTok = tokenizeNeeded(d.Path, f.old, needOld), tokenizeNeeded(d.Path, f.new, needNew)
	return f
}

// tokenize highlights a file's lines as its language does.
func tokenize(path string, lines []string) [][]editor.Token {
	return tokenizeNeeded(path, lines, nil)
}

// tokenizeNeeded highlights only lines where needed[i] is true (or all lines
// when needed is nil), reusing a scratch buffer and packing retained tokens
// into a single slab.
func tokenizeNeeded(path string, lines []string, needed []bool) [][]editor.Token {
	lang := editor.LanguageFor(path)
	if lang == nil || lang.Tokenizer == nil || len(lines) > 20000 {
		return nil
	}
	last := len(lines) - 1
	if needed != nil {
		last = -1
		for i := min(len(lines), len(needed)) - 1; i >= 0; i-- {
			if needed[i] {
				last = i
				break
			}
		}
		if last < 0 {
			return nil
		}
	}
	out := make([][]editor.Token, len(lines))
	scratch := make([]editor.Token, 0, 64)
	var chunk []editor.Token
	var state uint64
	for i := 0; i <= last; i++ {
		scratch, state = lang.Tokenizer.Tokenize(strings.TrimRight(lines[i], "\r\n"), state, scratch[:0])
		if (needed == nil || needed[i]) && len(scratch) > 0 {
			if len(chunk)+len(scratch) > cap(chunk) {
				chunk = make([]editor.Token, 0, max(4096, len(scratch)))
			}
			start := len(chunk)
			chunk = append(chunk, scratch...)
			out[i] = chunk[start:len(chunk):len(chunk)]
		}
	}
	return out
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
		if !hasSrc || sameDiffSource(f, d) {
			return f
		}
		if f.hash == diffHash(d) {
			setDiffSource(f, d)
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

var tokThemeCache = map[*ui.Theme][]editor.TokenStyle{}

func tokenStyles(th *ui.Theme) []editor.TokenStyle {
	if s, ok := tokThemeCache[th]; ok {
		return s
	}
	t := editor.DefaultTheme(th).Tokens
	s := t[:]
	tokThemeCache[th] = s
	return s
}

func (a *App) cachedDiffSpans(c *ui.Context, d *fileDiff, isNew bool, idx int, text string, toks []editor.Token) []ui.Span {
	th := c.Theme()
	if d.spanTheme != th {
		d.oldSpans, d.newSpans = nil, nil
		d.spanTheme = th
	}
	var cache *[][]ui.Span
	var lineCount int
	if isNew {
		cache, lineCount = &d.newSpans, len(d.new)
	} else {
		cache, lineCount = &d.oldSpans, len(d.old)
	}
	if idx < 0 || idx >= lineCount {
		return a.codeSpans(c, strings.TrimRight(text, "\r\n"), toks)
	}
	if *cache == nil {
		*cache = make([][]ui.Span, lineCount)
	}
	if sp := (*cache)[idx]; sp != nil {
		return sp
	}
	sp := a.codeSpans(c, strings.TrimRight(text, "\r\n"), toks)
	(*cache)[idx] = sp
	return sp
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
	styles := tokenStyles(c.Theme())
	out := make([]ui.Span, 0, len(toks)+1)
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
