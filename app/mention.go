package app

import (
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

// mention is the @ word being written at the caret. The list above the
// composer offers other sessions, folders, files and MCP resources.
// Claude Desktop groups the first three; Claude Code adds @server:uri.
type mention struct {
	caret int    // the caret as the composer was last built
	at    int    // the rune the @ is at, -1 with no such word
	query string // what follows the @ up to the caret
	sel   int
	shut  int  // the @ Escape put away, +1
	open      bool // the list shows
	place     int  // where the caret goes next frame, +1
	scrollSel bool // scroll selected row into view once after ↑/↓
}

// mentionItem is one row. text is what replaces the @ word, without the @.
type mentionItem struct {
	kind string // session, folder, file, resource
	text string
	name string
	note string
}

const (
	mentionSessions  = 5
	mentionResources = 8
)

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

// mentionMenu lists what the @ word can name.
func (a *App) mentionMenu(c *ui.Context) {
	m := &a.mention
	if m.place > 0 {
		return // the caret moves after a row just put in
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
	a.ensureMCP()
	groups := a.mentionGroups()
	flat := flattenMentions(groups)
	if len(flat) == 0 {
		return
	}
	m.sel = min(m.sel, len(flat)-1)
	m.open = true
	p := a.p
	list := ui.Column(c.Key("mention-menu")).Margin(0, 8, 6, 8).Padding(4).Radius(12).Background(p.Card).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 6, Scale: .985, Opacity: 0}, Duration: 150 * time.Millisecond})
	rim(list, p)
	lift(list, p)
	list.Children(func() {
		ui.Scroll(c).MaxHeight(320).Children(func() {
			n := 0
			for _, g := range groups {
				ui.Text(c, g.label).FontSize(12).FontWeight(600).LetterSpacing(.18).TextColor(p.Mute).Margin(6, 10, 2, 10)
				for _, it := range g.items {
					i, item := n, it
					n++
					row := ui.ButtonBase(c.Key("file:"+item.kind+":"+item.text)).Row().Height(32).Padding(0, 10).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(item.name).KeepFocus()
					press(row)
					focusRing(row, p)
					switch {
					case i == m.sel:
						row.Background(p.Sel)
						if m.scrollSel {
							row.ScrollIntoView()
							m.scrollSel = false
						}
					case row.Hovered():
						row.Background(p.Hover)
					}
					row.Children(func() {
						sym(c, mentionIcon(item.kind), 14, p.Mute)
						ui.Text(c, item.name).FontSize(13.5).TextColor(p.Fg).SingleLine().Shrink(0)
						if item.note != "" {
							ui.Text(c, item.note).FontSize(12.5).TextColor(p.Mute).SingleLine().TruncateMiddle().MinWidth(0)
						}
					})
					if row.Clicked() {
						a.complete(item.text)
					}
				}
			}
		})
	})
}

func mentionIcon(kind string) string {
	switch kind {
	case "session":
		return "message"
	case "folder":
		return "folder"
	case "resource":
		return "plug"
	default:
		return "file"
	}
}

// ensureMCP reads the folder's servers once, so @ can list their resources.
func (a *App) ensureMCP() {
	cur := a.C.Current
	if cur == nil || a.C.Phase != client.Ready {
		return
	}
	if a.C.MCP != nil && a.C.MCPFor == cur.Cwd {
		return
	}
	if a.slashAsked == cur.Cwd {
		return
	}
	a.slashAsked = cur.Cwd
	a.quiet(a.C.ReadMCP())
}

type mentionGroup struct {
	label string
	items []mentionItem
}

func (a *App) mentionGroups() []mentionGroup {
	var sessions, folders, files, resources []mentionItem
	for _, it := range a.mentionItems() {
		switch it.kind {
		case "session":
			sessions = append(sessions, it)
		case "folder":
			folders = append(folders, it)
		case "resource":
			resources = append(resources, it)
		default:
			files = append(files, it)
		}
	}
	var groups []mentionGroup
	add := func(label string, items []mentionItem) {
		if len(items) > 0 {
			groups = append(groups, mentionGroup{label, items})
		}
	}
	add(locales.Sessions(), sessions)
	add(locales.Folders(), folders)
	add(locales.Files(), files)
	add(locales.McpResources(), resources)
	return groups
}

func flattenMentions(groups []mentionGroup) []mentionItem {
	var flat []mentionItem
	for _, g := range groups {
		flat = append(flat, g.items...)
	}
	return flat
}

func (a *App) mentionItems() []mentionItem {
	q := strings.ToLower(a.mention.query)
	var items []mentionItem
	if a.C.Current != nil {
		n := 0
		for _, s := range a.C.Sessions {
			if s.ID == a.C.Current.ID || s.Archived {
				continue
			}
			title := s.Title
			if title == "" {
				title = s.ID
			}
			if q != "" && !strings.Contains(strings.ToLower(title), q) && !strings.Contains(strings.ToLower(s.ID), q) {
				continue
			}
			if n == mentionSessions {
				break
			}
			n++
			note := ""
			if s.Cwd != a.C.Current.Cwd {
				note = path.Base(s.Cwd)
			}
			items = append(items, mentionItem{kind: "session", text: "session:" + s.ID, name: title, note: note})
		}
	}
	for _, f := range a.mentionList() {
		g := strings.TrimSuffix(f, "/")
		kind, name := "file", path.Base(g)
		if g != f {
			kind, name = "folder", name+"/"
		}
		note := ""
		if dir := path.Dir(g); dir != "." {
			note = dir
		}
		items = append(items, mentionItem{kind: kind, text: f, name: name, note: note})
	}
	if l := a.C.MCP; l != nil && a.C.Current != nil && a.C.MCPFor == a.C.Current.Cwd {
		n := 0
		for _, r := range l.Resources {
			if r.Server == "" || r.URI == "" || strings.ContainsAny(r.Server, " \t:") || strings.ContainsAny(r.URI, " \t") {
				continue
			}
			blob := strings.ToLower(r.Server + " " + r.URI + " " + r.Name + " " + r.Description)
			if q != "" && !strings.Contains(blob, q) {
				continue
			}
			if n == mentionResources {
				break
			}
			n++
			name := r.Name
			if name == "" {
				name = r.URI
			}
			note := r.Server + ":" + r.URI
			if r.Name == "" {
				note = r.Description
			}
			items = append(items, mentionItem{kind: "resource", text: r.Server + ":" + r.URI, name: name, note: note})
		}
	}
	return items
}

// complete puts the choice in place of the @ word, the caret after it.
func (a *App) complete(text string) {
	m := &a.mention
	r := []rune(a.draft)
	if m.at < 0 || m.at >= len(r) {
		return
	}
	end := m.at
	for end < len(r) && !unicode.IsSpace(r[end]) {
		end++
	}
	if id, ok := strings.CutPrefix(text, "session:"); ok {
		a.draft = string(r[:m.at]) + strings.TrimLeft(string(r[end:]), " ")
		m.place = m.at + 1
		m.open = false
		if !slices.Contains(a.mentionSessions, id) {
			a.mentionSessions = append(a.mentionSessions, id)
		}
		return
	}
	word := "@" + text
	rest := string(r[end:])
	if rest == "" {
		rest = " " // the caret goes after a space, the one there or this
	}
	a.draft = string(r[:m.at]) + word + rest
	m.place = m.at + len([]rune(word)) + 2
	m.open = false
}

// mentionKey moves through the list and takes a row from it.
func (a *App) mentionKey(k ui.TextKey) bool {
	m := &a.mention
	items := flattenMentions(a.mentionGroups())
	n := len(items)
	if !m.open || n == 0 || k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		m.sel = (m.sel + n - 1) % n
		m.scrollSel = true
	case ui.KeyDown:
		m.sel = (m.sel + 1) % n
		m.scrollSel = true
	case ui.KeyEnter, ui.KeyTab:
		a.complete(items[min(m.sel, n-1)].text)
		k.SetText(a.draft)
	default:
		return false
	}
	return true
}

// mentionList is the files the @ list shows: with nothing typed yet, the
// files this session changed come first, the latest first.
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

var (
	mentionTokenRe = regexp.MustCompile(`(?:^|\s)(@[^\s@]+)`)
	slashTokenRe   = regexp.MustCompile(`(?:^|\s)(/[A-Za-z0-9][A-Za-z0-9._:-]*)`)
)

// skillAccent returns the Claude-style crisp blue highlight for /skill tokens.
func skillAccent(p *Palette) ui.Color {
	if p.Dark {
		return ui.Hex("#6BAEFF")
	}
	return ui.Hex("#1D5BB8")
}

func (a *App) knownSlashToken(name string) bool {
	if l := a.C.MCP; l != nil {
		for _, sk := range l.Skills {
			if !sk.Disabled && sk.Name == name {
				return true
			}
		}
		for _, cmd := range l.Commands {
			if cmd.Name == name {
				return true
			}
		}
		for _, pr := range l.Prompts {
			if pr.Server+":"+pr.Name == name {
				return true
			}
		}
	}
	return false
}

type mentionChip struct {
	raw       string
	icon      string
	label     string
	slash     bool
	sessionID string
}

func (a *App) extractMentionChips() []mentionChip {
	seen := map[string]bool{}
	var out []mentionChip
	for _, id := range a.mentionSessions {
		tok := "@session:" + id
		if seen[tok] {
			continue
		}
		seen[tok] = true
		label := id
		for _, s := range a.C.Sessions {
			if s.ID == id && title(s) != "" {
				label = title(s)
				break
			}
		}
		out = append(out, mentionChip{raw: tok, icon: "message", label: label, sessionID: id})
	}
	if strings.Contains(a.draft, "/") && !a.slash.open {
		for _, m := range slashTokenRe.FindAllStringSubmatchIndex(a.draft, -1) {
			tok := a.draft[m[2]:m[3]]
			name := strings.TrimPrefix(tok, "/")
			if len(name) == 0 || seen[tok] || !a.knownSlashToken(name) {
				continue
			}
			seen[tok] = true
			out = append(out, mentionChip{raw: tok, label: name, slash: true})
		}
	}
	if !strings.Contains(a.draft, "@") {
		return out
	}
	matches := mentionTokenRe.FindAllStringSubmatchIndex(a.draft, -1)
	if len(matches) == 0 {
		return out
	}
	activeAt := -1
	if a.mention.open && a.mention.at >= 0 {
		activeAt = len(string([]rune(a.draft)[:a.mention.at]))
	}
	for _, m := range matches {
		start, end := m[2], m[3]
		if start == activeAt {
			continue
		}
		tok := a.draft[start:end]
		if len(tok) < 2 || seen[tok] {
			continue
		}
		seen[tok] = true
		body := tok[1:]
		icon, label := "file-text", path.Base(body)
		switch {
		case strings.HasPrefix(body, "session:"):
			icon = "message"
			id := strings.TrimPrefix(body, "session:")
			label = id
			for _, s := range a.C.Sessions {
				if s.ID == id && title(s) != "" {
					label = title(s)
					break
				}
			}
		case strings.Contains(body, "://") || strings.Contains(body, ":"):
			icon = "plug"
			label = body
		case strings.HasSuffix(body, "/"):
			icon = "folder"
			label = strings.TrimSuffix(body, "/")
			if base := path.Base(label); base != "" && base != "." {
				label = base + "/"
			}
		}
		out = append(out, mentionChip{raw: tok, icon: icon, label: label})
	}
	return out
}

// mentionChips renders /skills (Claude-style blue highlight) and @mentions in
// the draft as removable Apple-style pills.
func (a *App) mentionChips(c *ui.Context) {
	chips := a.extractMentionChips()
	if len(chips) == 0 {
		return
	}
	p := a.p
	blue := skillAccent(p)
	ui.Row(c).Wrap().Gap(6).Margin(0, 0, 8, 0).Children(func() {
		for _, ch := range chips {
			bg, border := p.Chip, p.Edge
			if ch.slash {
				bg, border = blue.Alpha(.10), blue.Alpha(.24)
			}
			pill := ui.Row(c.Key("mchip:"+ch.raw)).Height(24).Padding(0, 2, 0, 8).Gap(4).Radius(7).
				Background(bg).Border(.5, border).AlignItems(ui.Center)
			pill.Children(func() {
				if ch.slash {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "/").FontSize(12.5).FontWeight(500).TextColor(blue.Alpha(.55)).SingleLine()
						ui.Text(c, ch.label).FontSize(12.5).FontWeight(500).TextColor(blue).SingleLine()
					})
				} else {
					sym(c, ch.icon, 12, p.Mute)
					ui.Text(c, ch.label).FontSize(12).FontWeight(500).TextColor(p.Fg2).SingleLine()
				}
				rm := ui.ButtonBase(c.Key("mchip-rm:"+ch.raw)).Size(24, 24).Radius(8).Center().KeepFocus().Label(locales.Remove())
				press(rm)
				if rm.Hovered() {
					rm.Background(p.Hover)
				}
				rmCol := p.Mute
				if ch.slash {
					rmCol = blue.Alpha(.7)
				}
				rm.Children(func() { sym(c, "x", 10, rmCol) })
				if rm.Clicked() {
					if ch.sessionID != "" {
						a.mentionSessions = slices.DeleteFunc(a.mentionSessions, func(id string) bool { return id == ch.sessionID })
					} else {
						next := strings.Replace(a.draft, ch.raw+" ", "", 1)
						if next == a.draft {
							next = strings.Replace(a.draft, ch.raw, "", 1)
						}
						a.draft = strings.TrimLeft(next, " ")
						a.toEnd = true
					}
				}
			})
		}
	})
}
