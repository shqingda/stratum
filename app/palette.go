package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// opt is a row of the palette: a session to open or an action to take.
type opt struct {
	icon, label, sub, kbd string
	mono                  bool
	run                   func()
}

// palOpts are the palette's rows for the query, by section.
func (a *App) palOpts(c *ui.Context) (sessions, actions []opt) {
	q := strings.ToLower(strings.TrimSpace(a.search.query))
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
	if len(a.visibleSessions) > 0 {
		add(opt{icon: "arrow-right", label: locales.NextWaiting(), kbd: "⌘J", run: a.nextWaiting})
	}
	cur := a.C.Current
	if cur != nil {
		add(opt{icon: "search", label: locales.FindInSession(), kbd: "⌘F", run: func() { a.openFind(c) }})
		if len(a.C.Entries) > 0 {
			add(opt{icon: "copy", label: locales.ExportMarkdown(), run: func() { c.WriteClipboard(a.sessionMarkdown()) }})
			if !a.C.Busy {
				add(opt{icon: "git-branch", label: locales.ForkSession(), run: func() { a.do(a.C.Fork(cur.ID)) }})
			}
		}
	}
	if cur != nil && a.C.CanAct() {
		next := map[string]string{"ask": "auto", "auto": "full", "full": "ask"}[cur.Mode]
		if next == "" {
			next = "ask"
		}
		add(opt{icon: "shield-half", label: locales.SwitchPermissionMode(), sub: modeName(cur.Mode) + " → " + modeName(next), run: func() {
			a.do(a.C.Update(proto.SessionUpdate{Mode: &next}))
		}})
		add(opt{icon: "sparkles", label: locales.SwitchModel(), sub: cur.Model, run: func() { a.menu.modelMenu = true }})
		add(opt{icon: "brain", label: locales.SwitchEffort(), run: func() { a.menu.modelMenu = true }})
	}
	if a.C.Busy {
		add(opt{icon: "player-stop", label: locales.StopRunning(), kbd: "esc", run: func() { a.do(a.C.Cancel()) }})
	}
	if a.clearWebData != nil {
		add(opt{icon: "trash", label: locales.ClearBrowserData() + "…", sub: locales.Browser(), run: a.askClearWeb})
	}
	set := a.settings()
	for _, d := range []struct {
		id   string
		name func() string
	}{{"", locales.DetailPlain}, {"thinking", locales.DetailThinking}, {"all", locales.DetailAll}} {
		if d.id != set.Detail {
			add(opt{icon: "list-details", label: locales.DetailTo(d.name()), run: func() {
				s := set
				s.Detail = d.id
				a.do(a.C.SaveSettings(s))
			}})
		}
	}
	add(opt{icon: "layout-sidebar-right", label: map[bool]string{true: locales.HideChanges(), false: locales.ShowChanges()}[a.rv.open], kbd: "⌘⌥I", run: func() { a.rv.open = !a.rv.open }})
	if cur != nil {
		add(opt{icon: "message", label: locales.SideAsk(), kbd: "⌘;", run: a.openSideAsk})
		if len(a.C.Entries) > 0 {
			add(opt{icon: "sparkles", label: locales.RegenerateTitle(), run: func() { a.do(a.C.Retitle(cur.ID)) }})
		}
		add(opt{icon: "terminal-2", label: locales.ShowTerminal(), run: a.showTerminal})
		add(opt{icon: "list-check", label: locales.ShowPlan(), run: func() { a.rv.open, a.rv.tab = true, "plan" }})
		add(opt{icon: "world", label: locales.Browser(), run: a.showBrowser})
		if !a.C.Busy && (a.C.Workspace.Branch != "" || cur.Worktree) {
			wt := !cur.Worktree
			lbl := locales.HandoffToWorktree()
			if !wt {
				lbl = locales.HandoffToLocal()
			}
			add(opt{icon: "git-branch", label: lbl, run: func() {
				a.do(a.C.Update(proto.SessionUpdate{Worktree: &wt}))
			}})
		}
		if a.C.Workspace.Dirty {
			add(opt{icon: "git-commit", label: locales.CommitChanges(), run: func() {
				a.rv.open, a.rv.tab = true, ""
				a.commitInput.Focus(c)
			}})
		}
		for _, o := range a.installedOpeners() {
			add(opt{icon: o.icon, label: locales.OpenWith(o.label), run: func() {
				if err := a.openIn(o.app, cur.Cwd); err != nil {
					a.flash("alert-circle", locales.CouldntOpen(o.label, shortOpenErr(err)))
				}
			}})
		}
		s := *cur
		add(opt{icon: "pencil", label: locales.Rename(), sub: title(s), run: func() { a.rename(s, "bar") }})
		pin := locales.Pin()
		if s.Pinned {
			pin = locales.Unpin()
		}
		add(opt{icon: "pin", label: pin, sub: title(s), run: func() {
			on := !s.Pinned
			a.do(a.C.Flag(proto.SessionFlag{SessionID: s.ID, Pinned: &on}))
		}})
		add(opt{icon: "eye", label: locales.MarkUnread(), sub: title(s), kbd: "⇧⌘U", run: func() { a.markUnread(s.ID) }})
		add(opt{icon: "archive", label: map[bool]string{true: locales.Unarchive(), false: locales.Archive()}[s.Archived], sub: title(s), kbd: "⇧⌘A", run: func() { a.archive(s, !s.Archived) }})
		add(opt{icon: "trash", label: locales.DeleteSession(), sub: title(s), run: func() { a.deleting = s.ID }})
	}
	add(opt{icon: "layout-sidebar-left-collapse", label: map[bool]string{true: locales.ShowSidebar(), false: locales.HideSidebar()}[a.side.noSide], kbd: "⌘\\", run: a.toggleSide})
	themes := []struct{ id, name string }{{"", locales.ThemeAuto()}, {"light", locales.ThemeLight()}, {"dark", locales.ThemeDark()}}
	for _, t := range themes {
		if t.id != set.Theme {
			add(opt{icon: "palette", label: locales.ThemeTo(t.name), run: func() {
				s := set
				s.Theme = t.id
				a.do(a.C.SaveSettings(s))
			}})
		}
	}
	a.settingOpts(set, add)
	add(opt{icon: "settings", label: locales.Settings(), kbd: "⌘,", run: a.openSettings})
	add(opt{icon: "user-circle", label: locales.AddAccount(), run: func() {
		a.openSettings()
		a.settingsPage, a.addingAccount = "accounts", true
	}})
	add(opt{icon: "puzzle", label: locales.SettingsMcp(), run: a.openMCP})
	add(opt{icon: "clock-play", label: locales.Automations(), run: a.openAutomations})
	return sessions, actions
}

// settingOpts are the settings the palette sets, a row for each value but
// the current one, as with its appearance and detail rows, so that none
// needs a trip to Settings.
func (a *App) settingOpts(set proto.Settings, add func(opt)) {
	save := func(edit func(*proto.Settings)) func() {
		return func() {
			s := set
			edit(&s)
			a.do(a.C.SaveSettings(s))
		}
	}
	chosen := a.locale.Chosen()
	for _, l := range languages() {
		if l.tag != chosen {
			add(opt{icon: "world", label: locales.SettingTo(locales.Language(), l.label), run: func() { a.locale.Set(l.tag) }})
		}
	}
	for _, t := range []struct{ id, name string }{{"small", locales.TextSmall()}, {"", locales.TextDefault()}, {"large", locales.TextLarge()}} {
		if t.id != set.TextSize {
			add(opt{icon: "a-large-small", label: locales.SettingTo(locales.TextSize(), t.name), run: save(func(s *proto.Settings) { s.TextSize = t.id })})
		}
	}
	add(opt{icon: "accessibility", label: locales.SettingTo(locales.ReduceMotion(), onOff(!set.ReduceMotion)), run: save(func(s *proto.Settings) {
		s.ReduceMotion = !set.ReduceMotion
	})})
	enter := locales.SendRightAway()
	if set.EnterSteers {
		enter = locales.QueueIt()
	}
	add(opt{icon: "keyboard", label: locales.SettingTo(locales.ReturnKey(), enter), run: save(func(s *proto.Settings) {
		s.EnterSteers = !set.EnterSteers
	})})
	add(opt{icon: "clock", label: locales.SettingTo(locales.PreventSleep(), onOff(!set.PreventSleep)), run: save(func(s *proto.Settings) {
		s.PreventSleep = !set.PreventSleep
	})})
	add(opt{icon: "sparkles", label: locales.SettingTo(locales.PromptSuggestions(), onOff(set.NoSuggestions)), run: save(func(s *proto.Settings) {
		s.NoSuggestions = !set.NoSuggestions
	})})
	for _, n := range []struct{ kind, label string }{
		{"wait", locales.NotifyKind(locales.NotifyWaitSetting())}, {"done", locales.NotifyKind(locales.NotifyDoneSetting())},
		{"fail", locales.NotifyKind(locales.NotifyFailSetting())}, {"badge", locales.DockBadge()},
	} {
		quiet := slices.Contains(set.Quiet, n.kind)
		add(opt{icon: "bell", label: locales.SettingTo(n.label, onOff(quiet)), run: save(func(s *proto.Settings) {
			s.Quiet = slices.DeleteFunc(slices.Clone(set.Quiet), func(k string) bool { return k == n.kind })
			if !quiet {
				s.Quiet = append(s.Quiet, n.kind)
			}
		})})
	}
	for _, w := range []struct{ id, name string }{{"", locales.NotifyInBackground()}, {"always", locales.NotifyAlways()}, {"never", locales.NotifyNever()}} {
		if w.id != set.NotifyWhen {
			add(opt{icon: "bell", label: locales.SettingTo(locales.NotifyWhen(), w.name), run: save(func(s *proto.Settings) { s.NotifyWhen = w.id })})
		}
	}
}

// onOff names a switch's state, as a row of Settings shows it.
func onOff(on bool) string {
	if on {
		return locales.SettingOn()
	}
	return locales.SettingOff()
}

// palFiles are the workspace's files the query names; chosen, one opens
// in the editor.
func (a *App) palFiles() []opt {
	q := strings.TrimSpace(a.search.query)
	cur := a.C.Current
	if q == "" || cur == nil {
		return nil
	}
	if a.C.FilesFor != q {
		a.quiet(a.C.FindFiles(q), nil)
		return nil
	}
	var out []opt
	for _, f := range a.C.Files[:min(5, len(a.C.Files))] {
		path := filepath.Join(cur.Cwd, f)
		out = append(out, opt{icon: "file-code", label: f, sub: filepath.Base(cur.Cwd), mono: true, run: func() {
			edLabel, edApp := a.preferredEditor()
			if err := a.openIn(edApp, path); err != nil {
				if edLabel == "" {
					edLabel = filepath.Base(path)
				}
				a.flash("alert-circle", locales.CouldntOpen(edLabel, shortOpenErr(err)))
			}
		}})
	}
	return out
}

// palHits are the messages the kernel found for the query.
func (a *App) palHits() []opt {
	q := strings.TrimSpace(a.search.query)
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
			a.search.jump, a.search.jumpIn = h.Seq, h.SessionID
			if cur := a.C.Current; cur == nil || cur.ID != h.SessionID {
				a.do(a.C.Open(h.SessionID))
			}
		}})
	}
	return out
}

// openPalette shows the palette, its query empty.
func (a *App) openPalette(c *ui.Context) {
	a.pal.open, a.search.query, a.pal.sel, a.pal.filter = true, "", 0, ""
	a.search.searchInput.Focus(c)
}

// palette is ⌘K: M3 SearchView with category FilterChips and keyboard navigation.
func (a *App) palette(c *ui.Context) {
	if !a.pal.open {
		return
	}
	p := a.p
	a.send(a.C.Search(strings.TrimSpace(a.search.query)))
	sessions, actions := a.palOpts(c)
	files, hits := a.palFiles(), a.palHits()
	switch a.pal.filter {
	case "sessions":
		actions, files, hits = nil, nil, nil
	case "actions":
		sessions, files, hits = nil, nil, nil
	case "files":
		sessions, actions, hits = nil, nil, nil
	case "messages":
		sessions, actions, files = nil, nil, nil
	}
	all := append(append(append(append([]opt(nil), sessions...), actions...), files...), hits...)
	a.pal.sel = max(0, min(a.pal.sel, len(all)-1))
	choose := func(i int) {
		a.pal.open, a.search.query, a.pal.filter = false, "", ""
		all[i].run()
	}
	ui.DialogBase(c, &a.pal.open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.Scrim).Justify(ui.Start).Padding(72, 16, 16, 16)
		panel.Width(580).MaxWidthPercent(100).Radius(24).Background(p.SurfaceContainerHigh).
			Clip().Label(locales.CommandPalette()).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -8, Scale: .98, Opacity: 0}, Animation: &animFast})
		elevation3(panel, p)
		panel.Children(func() {
			ui.Row(c).Height(56).Padding(0, 20).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.OutlineVariant.Alpha(.20)).Children(func() {
				sym(c, "search", 18, p.OnSurfaceVariant)
				ui.TextInputBase(c, &a.search.query).Bind(&a.search.searchInput).Label(locales.Search()).Placeholder(locales.PalettePlaceholder()).PlaceholderColor(p.OnSurfaceVariant).
					FontSize(16).TextColor(p.OnSurface).Grow(1).SingleLine().AutoFocus().OnTextKey(func(k ui.TextKey) bool {
					a.search.query = k.Text()
					switch {
					case k.Key == ui.KeyDown && k.Mods == 0:
						if len(all) > 0 {
							a.pal.sel = (a.pal.sel + 1) % len(all)
							a.pal.scroll = true
						}
					case k.Key == ui.KeyUp && k.Mods == 0:
						if len(all) > 0 {
							a.pal.sel = (a.pal.sel + len(all) - 1) % len(all)
							a.pal.scroll = true
						}
					case k.Key == ui.KeyEnter && (k.Mods == 0 || k.Mods == ui.Cmd):
						if len(all) > 0 {
							choose(a.pal.sel)
						}
					case k.Key == ui.KeyW && k.Mods == ui.Cmd:
						a.pal.open, a.search.query, a.pal.filter = false, "", ""
					default:
						if k.Key != ui.KeyEnter {
							a.pal.sel = 0
						}
						return false
					}
					return true
				})
			})
			ui.Row(c).Height(42).Padding(0, 16).Gap(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.OutlineVariant.Alpha(.20)).Children(func() {
				chips := []struct{ id, label string }{
					{"", locales.FilterAll()},
					{"sessions", locales.Sessions()},
					{"actions", locales.Actions()},
					{"files", locales.Files()},
					{"messages", locales.PaletteMessages()},
				}
				for _, ch := range chips {
					on := a.pal.filter == ch.id
					cb := ui.ButtonBase(c.Key("pal-filter:"+ch.id)).Row().Height(28).Padding(0, 10).Gap(4).Radius(10).AlignItems(ui.Center).
						Label(ch.label).Checked(on).KeepFocus()
					m3Focus(cb, p)
					fg, weight := p.OnSurfaceVariant, 500
					if on {
						m3State(cb, p, p.SecondaryContainer, true)
						fg, weight = p.OnSecondaryContainer, 600
					} else {
						m3State(cb, p, p.SurfaceContainerHighest, false)
					}
					cb.Children(func() {
						if on {
							sym(c, "check", 12, fg)
						}
						ui.Text(c, ch.label).FontSize(12).FontWeight(weight).TextColor(fg).SingleLine()
					})
					if cb.Clicked() {
						a.pal.filter = ch.id
						a.pal.sel = 0
					}
				}
			})
			ui.Scroll(c).MaxHeight(380).Children(func() {
				i := 0
				section := func(name string, opts []opt) {
					if len(opts) == 0 {
						return
					}
					ui.Column(c).Padding(8, 10, 4, 10).Children(func() {
						ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(p.OnSurfaceVariant).Margin(4, 10, 4, 10)
						for _, o := range opts {
							a.palRow(c, o, i, choose)
							i++
						}
					})
				}
				section(locales.Sessions(), sessions)
				section(locales.Actions(), actions)
				section(locales.Files(), files)
				section(locales.PaletteMessages(), hits)
				if len(all) == 0 {
					emptyState(c, p, "search", locales.PaletteNoMatches(), "", nil)
				}
				ui.Box(c).Height(8)
			})
		})
	})
	if !a.pal.open {
		a.search.query, a.pal.filter = "", ""
	}
}

// palRow is one row, the query's match in it bold.
func (a *App) palRow(c *ui.Context, o opt, i int, choose func(int)) {
	p := a.p
	b := ui.ButtonBase(c.Key(fmt.Sprintf("pal:%d", i))).Row().Height(38).Padding(0, 12).Gap(10).Radius(12).AlignItems(ui.Center).Label(o.label)
	m3Focus(b, p)
	fg, sub := p.OnSurface, p.OnSurfaceVariant
	if i == a.pal.sel {
		b.Background(p.SecondaryContainer)
		fg = p.OnSecondaryContainer
		if a.pal.scroll {
			b.ScrollIntoView()
			a.pal.scroll = false
		}
	} else {
		m3State(b, p, ui.Color{}, false)
	}
	if b.Clicked() {
		choose(i)
	}
	b.Children(func() {
		ui.Box(c).Width(18).Center().Children(func() { sym(c, o.icon, 16, sub) })
		ui.Row(c).Shrink(1).MinWidth(0).Children(func() {
			pre, hit, post := split(o.label, a.search.query)
			text := func(s string, weight int) {
				if s != "" {
					t := ui.Text(c, s).FontSize(14).FontWeight(weight).TextColor(fg).SingleLine().PreserveSpaces().Shrink(1).MinWidth(0)
					if o.mono {
						t.Font("monospace").FontSize(13)
					}
				}
			}
			text(pre, 400)
			text(hit, 600)
			text(post, 400)
		})
		if o.sub != "" {
			ui.Text(c, o.sub).FontSize(13).TextColor(sub).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(160)
		}
		ui.Spacer(c)
		kbd := o.kbd
		if i == a.pal.sel {
			kbd = "⏎"
		}
		if kbd != "" {
			ui.Text(c, kbd).FontSize(12).TextColor(sub).Shrink(0)
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
