package app

import (
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
)

// mention is the @ word being written at the caret, which names a file
// of the workspace from a list above the composer.
type mention struct {
	caret int    // the caret as the composer was last built
	at    int    // the rune the @ is at, -1 with no such word
	query string // what follows the @ up to the caret
	sel   int
	shut  int  // the @ Escape put away, +1
	open  bool // the list shows
	place int  // where the caret goes next frame, +1
}

// mentionAt finds the @ word that ends at the caret.
func (a *App) mentionAt() (int, string) {
	r := []rune(a.draft)
	n := min(a.mention.caret, len(r))
	i := n
	for i > 0 && !unicode.IsSpace(r[i-1]) {
		i--
	}
	if i < n && r[i] == '@' {
		return i, string(r[i+1 : n])
	}
	return -1, ""
}

// files lists the workspace's files the @ word names.
func (a *App) files(c *ui.Context) {
	m := &a.mention
	if m.place > 0 {
		return // the caret moves after a file just put in
	}
	at, q := a.mentionAt()
	if at != m.at || q != m.query {
		m.sel = 0
	}
	if at < 0 {
		m.shut = 0
	}
	m.at, m.query, m.open = at, q, false
	if at < 0 || m.shut == at+1 || a.C.Current == nil || a.pal {
		return
	}
	// Asked once for each folder and query; an empty query too.
	if key := a.C.Current.Cwd + "\x00" + q; a.filesKey != key {
		if cmds := a.C.FindFiles(q); cmds != nil {
			a.filesKey = key
			a.quiet(cmds, nil)
		}
	}
	files := a.mentionList()
	if len(files) == 0 {
		return
	}
	m.sel = min(m.sel, len(files)-1)
	m.open = true
	p := a.p
	list := ui.Column(c).Margin(0, 8, 6, 8).Padding(4).Radius(12).Background(p.Card)
	lift(list, p)
	list.Children(func() {
		for i, f := range files {
			row := ui.ButtonBase(c.Key("file:"+f)).Row().Height(30).Padding(0, 10).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(f)
			if i == m.sel || row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				g := strings.TrimSuffix(f, "/") // a folder ends in /
				icon, name := "file", path.Base(g)
				if g != f {
					icon, name = "folder", name+"/"
				}
				sym(c, icon, 14, p.Mute)
				ui.Text(c, name).FontSize(13.5).TextColor(p.Fg).SingleLine().Shrink(0)
				if dir := path.Dir(g); dir != "." {
					ui.Text(c, dir).FontSize(12.5).TextColor(p.Mute).SingleLine().TruncateMiddle().MinWidth(0)
				}
			})
			if row.Clicked() {
				a.complete(f)
			}
		}
	})
}

// complete puts the file in place of the @ word, the caret after it.
func (a *App) complete(file string) {
	m := &a.mention
	r := []rune(a.draft)
	if m.at < 0 || m.at >= len(r) {
		return
	}
	end := m.at
	for end < len(r) && !unicode.IsSpace(r[end]) {
		end++
	}
	word := "@" + file
	rest := string(r[end:])
	if rest == "" {
		rest = " " // the caret goes after a space, the one there or this
	}
	a.draft = string(r[:m.at]) + word + rest
	m.place = m.at + len([]rune(word)) + 2
	m.open = false
}

// mentionKey moves through the list and takes a file from it.
func (a *App) mentionKey(k ui.TextKey) bool {
	m := &a.mention
	files := a.mentionList()
	n := len(files)
	if !m.open || n == 0 || k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		m.sel = (m.sel + n - 1) % n
	case ui.KeyDown:
		m.sel = (m.sel + 1) % n
	case ui.KeyEnter, ui.KeyTab:
		a.complete(files[min(m.sel, n-1)])
		k.SetText(a.draft)
	default:
		return false
	}
	return true
}

// mentionList is what the @ list shows: with nothing typed yet, the files
// this session changed come first, the latest first.
func (a *App) mentionList() []string {
	if a.mention.query != "" || a.C.Current == nil {
		return a.C.Files
	}
	var list []string
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] && len(list) < 8 {
			seen[f] = true
			list = append(list, f)
		}
	}
	turns := a.C.Changes.Turns
	for i := len(turns) - 1; i >= 0; i-- {
		for _, f := range turns[i].Files {
			p := f.Path
			if rel, err := filepath.Rel(a.C.Current.Cwd, p); err == nil && filepath.IsAbs(p) {
				p = rel
			}
			add(filepath.ToSlash(p))
		}
	}
	for _, f := range a.C.Files {
		add(f)
	}
	return list
}
