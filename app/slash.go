package app

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// slash is the / menu over the composer: the app's own commands and the
// folder's skills, for a draft that is a / word and nothing more.
type slash struct {
	sel  int
	open bool
	shut string // the draft Escape put the menu away for
}

type slashItem struct {
	name, icon, note string
	run              func(c *ui.Context)
}

// slashQuery is the word after the /, if the draft is only that.
func (a *App) slashQuery() (string, bool) {
	q, ok := strings.CutPrefix(a.draft, "/")
	if !ok || strings.IndexFunc(q, unicode.IsSpace) >= 0 {
		return "", false
	}
	return strings.ToLower(q), true
}

func (a *App) slashItems(c *ui.Context) []slashItem {
	q, ok := a.slashQuery()
	if !ok || a.C.Current == nil {
		return nil
	}
	clear := func() { a.draft = "" }
	all := []slashItem{
		{"new", "edit", locales.NewSession(), func(*ui.Context) { clear(); a.do(a.C.NewSession()) }},
		{"changes", "layout-sidebar-right", locales.ShowChanges(), func(*ui.Context) { clear(); a.rv.open = !a.rv.open }},
		{"find", "search", locales.FindInSession(), func(c *ui.Context) { clear(); a.openFind(c) }},
		{"search", "search", locales.Search(), func(c *ui.Context) { clear(); a.openPalette(c) }},
		{"settings", "adjustments-horizontal", locales.Settings(), func(*ui.Context) { clear(); a.openSettings() }},
		{"skills", "puzzle", locales.SettingsMcp(), func(*ui.Context) { clear(); a.openMCP() }},
	}
	// The composer's own menus, opened from here.
	all = append(all,
		slashItem{"model", "sparkles", locales.DefaultModel(), func(*ui.Context) { clear(); a.modelMenu = true }},
		slashItem{"effort", "brain", locales.ReasoningEffort(), func(*ui.Context) { clear(); a.effortMenu = true }},
	)
	// /undo puts back the last turn's changes not yet undone.
	if turns := a.C.Changes.Turns; !a.C.Busy {
		for i := len(turns) - 1; i >= 0; i-- {
			if t := turns[i]; len(t.Files) > 0 && !a.C.Changes.Reverted[t.TurnSeq] {
				seq := t.TurnSeq
				all = append(all, slashItem{"undo", "arrow-back-up", locales.UndoAll() + " · " + locales.ChangedFiles(len(t.Files)), func(*ui.Context) {
					clear()
					a.do(a.C.Revert(seq, "", false))
				}})
				break
			}
		}
	}
	if !a.C.Busy && len(a.rewindPrompts()) > 0 {
		all = append(all, slashItem{"rewind", "history", locales.Rewind(), func(*ui.Context) {
			clear()
			a.openRewind()
		}})
	}
	if !a.C.Busy && len(a.C.Entries) == 0 && (a.C.Workspace.Branch != "" || a.C.Current.Worktree) {
		wt := !a.C.Current.Worktree
		note := locales.WorktreeSub()
		if !wt {
			note = locales.LocalSub()
		}
		all = append(all, slashItem{"worktree", "git-branch", note, func(*ui.Context) {
			clear()
			a.do(a.C.Update(proto.SessionUpdate{Worktree: &wt}))
		}})
	}
	if !a.C.Busy && len(a.C.Entries) > 0 {
		all = append(all,
			slashItem{"compact", "fold", locales.CompactNow(), func(*ui.Context) { clear(); a.do(a.C.Compact()) }},
			slashItem{"fork", "git-branch", locales.ForkNote(), func(*ui.Context) { clear(); a.do(a.C.Fork(a.C.Current.ID)) }},
		)
	}
	if len(a.C.Entries) > 0 {
		all = append(all, slashItem{"export", "copy", locales.ExportNote(), func(c *ui.Context) { clear(); c.WriteClipboard(a.sessionMarkdown()) }})
	}
	if a.C.Busy {
		all = append(all, slashItem{"stop", "player-stop", locales.Stop(), func(*ui.Context) { clear(); a.do(a.C.Cancel()) }})
	}
	for _, m := range modeRows() {
		mode := m.id
		all = append(all, slashItem{mode, m.icon, m.label + " · " + m.sub, func(*ui.Context) {
			clear()
			a.do(a.C.Update(proto.SessionUpdate{Mode: &mode}))
		}})
	}
	if l := a.C.MCP; l != nil && a.C.MCPFor == a.C.Current.Cwd {
		for _, sk := range l.Skills {
			if sk.Disabled {
				continue
			}
			name := sk.Name
			all = append(all, slashItem{name, "sparkles", sk.Description, func(*ui.Context) {
				// The agent takes it from here, with its skill tool.
				a.draft, a.toEnd = locales.UseSkill(name), true
			}})
		}
		for _, cmd := range l.Commands {
			name := cmd.Name
			all = append(all, slashItem{name, "file-text", cmd.Description, func(*ui.Context) {
				// What follows the name goes where $ARGUMENTS is, on send.
				a.draft, a.toEnd = "/"+name+" ", true
			}})
		}
		for _, pr := range l.Prompts {
			name := pr.Server + ":" + pr.Name
			desc := pr.Description
			if desc == "" && len(pr.Arguments) > 0 {
				var args []string
				for _, arg := range pr.Arguments {
					args = append(args, "<"+arg+">")
				}
				desc = strings.Join(args, " ")
			}
			all = append(all, slashItem{name, "plug", desc, func(*ui.Context) {
				a.draft, a.toEnd = "/"+name+" ", true
			}})
		}
	} else if a.slashAsked != a.C.Current.Cwd && a.C.Phase == client.Ready {
		a.slashAsked = a.C.Current.Cwd // once a folder, not every frame
		a.quiet(a.C.ReadMCP())
	}
	var out []slashItem
	for _, it := range all {
		if strings.HasPrefix(strings.ToLower(it.name), q) {
			out = append(out, it)
		}
	}
	return out
}

// slashMenu shows the / menu over the composer.
// slashName is the column the names take, so the notes line up.
const slashName = 140

func (a *App) slashMenu(c *ui.Context) {
	s := &a.slash
	s.open = false
	if s.shut != "" && s.shut == a.draft {
		return
	}
	s.shut = ""
	items := a.slashItems(c)
	if len(items) == 0 {
		return
	}
	s.sel = min(s.sel, len(items)-1)
	s.open = true
	p := a.p
	list := ui.Column(c).Margin(0, 8, 6, 8).Padding(4).Radius(12).Background(p.Card)
	lift(list, p)
	list.Children(func() {
		for i, it := range items {
			row := ui.ButtonBase(c.Key("slash:"+it.name)).Row().Height(32).Padding(0, 10).Gap(12).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label("/" + it.name)
			if i == s.sel || row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				ui.Text(c, "/"+it.name).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").Width(slashName).Shrink(0)
				ui.Text(c, it.note).FontSize(12.5).TextColor(p.Mute).SingleLine().MinWidth(0)
			})
			if row.Clicked() {
				it.run(c)
				s.sel = 0
			}
		}
	})
	a.slashRun = func(i int) { items[i].run(c) }
	a.slashLen = len(items)
}

// slashKey moves through the / menu and runs from it.
func (a *App) slashKey(k ui.TextKey) bool {
	s := &a.slash
	n := a.slashLen
	if !s.open || n == 0 || k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		s.sel = (s.sel + n - 1) % n
	case ui.KeyDown:
		s.sel = (s.sel + 1) % n
	case ui.KeyEnter, ui.KeyTab:
		a.slashRun(min(s.sel, n-1))
		s.sel = 0
		k.SetText(a.draft)
	case ui.KeyEscape:
		s.shut = a.draft
	default:
		return false
	}
	return true
}

// expandCommand turns "/name words" into the saved command's prompt, the
// words in place of $ARGUMENTS, or after it if it has no such place.
func (a *App) expandCommand(text string) string {
	l := a.C.MCP
	rest, ok := strings.CutPrefix(text, "/")
	if !ok || l == nil {
		return text
	}
	name, args, _ := strings.Cut(rest, " ")
	for _, cmd := range l.Commands {
		if cmd.Name != name {
			continue
		}
		args = strings.TrimSpace(args)
		if strings.Contains(cmd.Body, "$ARGUMENTS") {
			return strings.ReplaceAll(cmd.Body, "$ARGUMENTS", args)
		}
		if args != "" {
			return cmd.Body + "\n\n" + args
		}
		return cmd.Body
	}
	return text
}
