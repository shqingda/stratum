package app

import (
	"path"
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
	if a.C.FilesFor != q {
		a.quiet(a.C.FindFiles(q), nil)
	}
	files := a.C.Files
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
			row := ui.ButtonBase(c.Key("file:"+f)).Row().Height(30).Padding(0, 8).Gap(8).Radius(8).AlignItems(ui.Center).Label(f)
			if i == m.sel || row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				sym(c, "file", 14, p.Mute)
				ui.Text(c, path.Base(f)).FontSize(13.5).TextColor(p.Fg).SingleLine().Shrink(0)
				if dir := path.Dir(f); dir != "." {
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
	n := len(a.C.Files)
	if !m.open || n == 0 || k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		m.sel = (m.sel + n - 1) % n
	case ui.KeyDown:
		m.sel = (m.sel + 1) % n
	case ui.KeyEnter, ui.KeyTab:
		a.complete(a.C.Files[min(m.sel, n-1)])
		k.SetText(a.draft)
	default:
		return false
	}
	return true
}
