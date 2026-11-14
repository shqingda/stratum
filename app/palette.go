package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// opt is a row of the palette: a session to open or an action to take.
type opt struct {
	icon, label, sub, kbd string
	mono                  bool
	run                   func()
}

// palOpts are the palette's rows for the query, by section.
func (a *App) palOpts() (sessions, actions []opt) {
	q := strings.ToLower(strings.TrimSpace(a.query))
	match := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	now := a.now()
	for _, s := range a.C.Sessions {
		if len(sessions) == 6 {
			break
		}
		if t := title(s); match(t) {
			id := s.ID
			sessions = append(sessions, opt{icon: "message", label: t, sub: filepath.Base(s.Cwd) + " · " + ago(now, s.Updated), run: func() {
				if cur := a.C.Current; cur == nil || cur.ID != id {
					a.do(a.C.Open(id))
				}
			}})
		}
	}
	add := func(o opt) {
		if match(o.label) {
			actions = append(actions, o)
		}
	}
	add(opt{icon: "message-plus", label: locales.NewSession(), kbd: "⌘N", run: func() { a.do(a.C.NewSession()) }})
	if cur := a.C.Current; cur != nil && a.C.CanAct() {
		next := map[string]string{"ask": "auto", "auto": "full", "full": "ask"}[cur.Mode]
		if next == "" {
			next = "ask"
		}
		add(opt{icon: "shield-half", label: locales.SwitchPermissionMode(), sub: modeName(cur.Mode) + " → " + modeName(next), run: func() {
			a.do(a.C.Update(proto.SessionUpdate{Mode: &next}))
		}})
	}
	if a.C.Busy {
		add(opt{icon: "player-stop", label: locales.StopRunning(), kbd: "esc", run: func() { a.do(a.C.Cancel()) }})
	}
	add(opt{icon: "layout-sidebar-right", label: map[bool]string{true: locales.HideChanges(), false: locales.ShowChanges()}[a.rv.open], run: func() { a.rv.open = !a.rv.open }})
	add(opt{icon: "layout-sidebar-left-collapse", label: map[bool]string{true: locales.ShowSidebar(), false: locales.HideSidebar()}[a.noSide], kbd: "⌘\\", run: func() { a.noSide = !a.noSide }})
	return sessions, actions
}

// palHits are the messages the kernel found for the query.
func (a *App) palHits() []opt {
	q := strings.TrimSpace(a.query)
	if a.C.HitsFor != q {
		return nil
	}
	titles := map[string]string{}
	for _, s := range a.C.Sessions {
		titles[s.ID] = title(s)
	}
	var out []opt
	for _, h := range a.C.Hits {
		name, ok := titles[h.SessionID]
		if !ok {
			continue // deleted since
		}
		h := h
		text := strings.NewReplacer("[", "", "]", "").Replace(strings.Join(strings.Fields(h.Snippet), " "))
		out = append(out, opt{icon: "message", label: text, sub: name, run: func() {
			a.jump, a.jumpIn = h.Seq, h.SessionID
			if cur := a.C.Current; cur == nil || cur.ID != h.SessionID {
				a.do(a.C.Open(h.SessionID))
			}
		}})
	}
	return out
}

// openPalette shows the palette, its query empty.
func (a *App) openPalette(c *ui.Context) {
	a.pal, a.query, a.palSel = true, "", 0
	a.searchInput.Focus(c)
}

// palette is ⌘K: sessions and actions in one search, over the window.
func (a *App) palette(c *ui.Context) {
	if !a.pal {
		return
	}
	p := a.p
	a.send(a.C.Search(strings.TrimSpace(a.query)))
	sessions, actions := a.palOpts()
	hits := a.palHits()
	all := append(append(append([]opt(nil), sessions...), actions...), hits...)
	a.palSel = max(0, min(a.palSel, len(all)-1))
	choose := func(i int) {
		a.pal, a.query = false, ""
		all[i].run()
	}
	ui.DialogBase(c, &a.pal, func(backdrop, panel ui.Element) {
		scrim := ui.RGBA(18, 19, 26, .18)
		if p == dark {
			scrim = ui.RGBA(6, 7, 10, .45)
		}
		backdrop.Background(scrim).Justify(ui.Start).Padding(84, 16, 16, 16)
		panel.Width(520).MaxWidthPercent(100).Radius(14).Background(p.Card).Clip().Label(locales.CommandPalette())
		if p == dark {
			panel.Shadow(0, 0, 0, .5, ui.RGBA(0, 0, 0, .8)).Shadow(0, 18, 48, -8, ui.RGBA(0, 0, 0, .6)).InsetShadow(0, .5, 0, 0, ui.RGBA(255, 255, 255, .08))
		} else {
			panel.Shadow(0, 0, 0, .5, ink.Alpha(.14)).Shadow(0, 12, 32, -8, ink.Alpha(.22))
		}
		panel.Children(func() {
			ui.Row(c).Height(50).Padding(0, 16).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
				sym(c, "search", 18, p.Mute)
				ui.TextInputBase(c, &a.query).Bind(&a.searchInput).Label(locales.Search()).Placeholder(locales.PalettePlaceholder()).PlaceholderColor(p.Mute).
					FontSize(15).TextColor(p.Fg).Grow(1).SingleLine().AutoFocus().OnTextKey(func(k ui.TextKey) bool {
					a.query = k.Text()
					switch {
					case k.Key == ui.KeyDown && k.Mods == 0:
						a.palSel = min(a.palSel+1, len(all)-1)
					case k.Key == ui.KeyUp && k.Mods == 0:
						a.palSel = max(a.palSel-1, 0)
					case k.Key == ui.KeyEnter && (k.Mods == 0 || k.Mods == ui.Cmd):
						if len(all) > 0 {
							choose(a.palSel)
						}
					default:
						if k.Key != ui.KeyEnter {
							a.palSel = 0
						}
						return false
					}
					return true
				})
			})
			ui.Scroll(c).MaxHeight(380).Children(func() {
				i := 0
				section := func(name string, opts []opt) {
					if len(opts) == 0 {
						return
					}
					ui.Column(c).Padding(6, 6, 2, 6).Children(func() {
						ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(p.Mute).Margin(6, 10, 4, 10)
						for _, o := range opts {
							a.palRow(c, o, i, choose)
							i++
						}
					})
				}
				section(locales.Sessions(), sessions)
				section(locales.Actions(), actions)
				section(locales.PaletteMessages(), hits)
				if len(all) == 0 {
					ui.Text(c, locales.PaletteNoMatches()).FontSize(12.5).TextColor(p.Mute).Padding(18, 16)
				}
				ui.Box(c).Height(6)
			})
			ui.Row(c).Height(36).Padding(0, 14).Gap(14).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
				for _, h := range [][2]string{{"↑↓", locales.Select()}, {"⏎", locales.Open()}, {"esc", locales.Close()}} {
					ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Text(c, h[0]).FontSize(11).TextColor(p.Mute).Padding(0, 4).Radius(4).Background(p.Chip)
						ui.Text(c, h[1]).FontSize(11.5).TextColor(p.Mute)
					})
				}
			})
		})
	})
	if !a.pal {
		a.query = ""
	}
}

// palRow is one row, the query's match in it bold.
func (a *App) palRow(c *ui.Context, o opt, i int, choose func(int)) {
	p := a.p
	b := ui.ButtonBase(c.Key(fmt.Sprintf("pal:%d", i))).Row().Height(34).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(o.label)
	switch {
	case i == a.palSel:
		b.Background(p.Sel)
	case b.Hovered():
		b.Background(p.Hover)
	}
	if b.Clicked() {
		choose(i)
	}
	b.Children(func() {
		ui.Box(c).Width(18).Center().Children(func() { sym(c, o.icon, 16, p.Fg2) })
		ui.Row(c).Shrink(1).MinWidth(0).Children(func() {
			pre, hit, post := split(o.label, a.query)
			text := func(s string, weight int) {
				if s != "" {
					ui.Text(c, s).FontSize(13).FontWeight(weight).TextColor(p.Fg).SingleLine().PreserveSpaces().Shrink(1).MinWidth(0)
				}
			}
			text(pre, 400)
			text(hit, 600)
			text(post, 400)
		})
		if o.sub != "" {
			ui.Text(c, o.sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(160)
		}
		ui.Spacer(c)
		kbd := o.kbd
		if i == a.palSel {
			kbd = "⏎"
		}
		if kbd != "" {
			ui.Text(c, kbd).FontSize(11).TextColor(p.Mute).Shrink(0)
		}
	})
}

// split cuts s around the first match of q, ignoring case.
func split(s, q string) (pre, hit, post string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return s, "", ""
	}
	i := strings.Index(strings.ToLower(s), strings.ToLower(q))
	if i < 0 || len(strings.ToLower(s)) != len(s) {
		return s, "", ""
	}
	return s[:i], s[i : i+len(q)], s[i+len(q):]
}
