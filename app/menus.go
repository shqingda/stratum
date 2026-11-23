package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func (a *App) syncMenus() {
	id := ""
	if a.C.Current != nil {
		id = a.C.Current.ID
	}
	if a.modelsLoadedAccount == "" && len(a.C.Models) > 0 {
		a.modelsLoadedAccount = a.modelAccount()
	}
	if a.C.Phase == client.Disconnected {
		a.modelsPending = ""
	}
	if id != a.menuSession || !a.C.CanAct() {
		a.modelMenu, a.effortMenu, a.modeMenu, a.detailMenu, a.worktreeMenu, a.branchMenu = false, false, false, false, false, false
		a.modelsRequested = false
		a.menuSession = id
	}
}

func (a *App) receive(f proto.Frame) {
	a.send(a.C.Receive(f))
	a.watch()
	if f.Type == proto.FrameResponse && a.modelsPending != "" && f.ID == a.modelsPending {
		a.modelsPending = ""
		if f.Error != nil {
			a.modelsError = f.Error.Message
		} else {
			a.modelsLoadedAccount = a.modelsAccount
		}
	}
	if a.C.Phase == client.Disconnected {
		a.modelsPending = ""
	}
}

func (a *App) menuOptions(open *bool) ui.PopoverMenuButtonOptions {
	return ui.PopoverMenuButtonOptions{Open: open, Panel: func(panel ui.Element) {
		panel.Background(a.p.Card).Border(.5, a.p.Edge2).Radius(12).FontSize(13).NoShadow()
		lift(panel, a.p)
		rim(panel, a.p)
	}}
}

func (a *App) modelAccount() string {
	if a.C.Current != nil && a.C.Current.Account != "" {
		return a.C.Current.Account
	}
	if acc := a.account(); acc != nil {
		return acc.ID
	}
	return ""
}

func modelKey(account, id string) string {
	if account == "" {
		return id
	}
	return account + "/" + id
}

func (a *App) toggleFavoriteModel(account, id string) {
	if a.C.Settings == nil {
		return
	}
	k := modelKey(account, id)
	s := *a.C.Settings
	favs := slices.Clone(s.FavoriteModels)
	if idx := slices.Index(favs, k); idx >= 0 {
		favs = slices.Delete(favs, idx, idx+1)
	} else {
		favs = append(favs, k)
	}
	s.FavoriteModels = favs
	a.do(a.C.SaveSettings(s))
}

func (a *App) recordRecentModel(account, id string) {
	if a.C.Settings == nil {
		return
	}
	k := modelKey(account, id)
	s := *a.C.Settings
	rec := slices.DeleteFunc(slices.Clone(s.RecentModels), func(x string) bool { return x == k })
	rec = append([]string{k}, rec...)
	if len(rec) > 5 {
		rec = rec[:5]
	}
	if slices.Equal(s.RecentModels, rec) {
		return
	}
	s.RecentModels = rec
	a.do(a.C.SaveSettings(s))
}

func (a *App) filterModels(models []proto.Model, query string) []proto.Model {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return models
	}
	var out []proto.Model
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Name), q) || strings.Contains(strings.ToLower(m.ID), q) {
			out = append(out, m)
		}
	}
	return out
}

func (a *App) modelPill(c *ui.Context, label string) {
	if a.modelsAccount != a.modelAccount() {
		a.modelsRequested = false
	}
	p := a.p
	effort := effortLabel(a.C.Current.Effort)
	b := ui.ButtonBase(c.Key("pill:model")).Row().Height(28).Padding(0, 9).Gap(5).Radius(14).AlignItems(ui.Center).
		Label(label).Disabled(!a.C.CanAct())
	press(b)
	focusRing(b, p)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
		ui.Text(c, effort).FontSize(13).TextColor(p.Mute).SingleLine()
		sym(c, "selector", 11, p.Mute)
	})
	if b.Clicked() {
		a.modelMenu = !a.modelMenu
		if a.modelMenu {
			a.modelQuery = ""
		}
	}
	account := a.modelAccount()
	if a.modelMenu && !a.modelsRequested {
		a.modelsRequested = true
		a.modelsAccount, a.modelsError = account, ""
		cmds, err := a.C.ListModels(account)
		if err == nil {
			a.modelsPending = cmds[0].ID
		} else {
			a.modelsError = err.Error()
		}
		a.do(cmds, err)
	}
	if !a.modelMenu {
		a.modelsRequested = false
		a.modelQuery = ""
	}
	current := a.C.Current.Model
	set := a.settings()
	favSet := map[string]bool{}
	for _, k := range set.FavoriteModels {
		favSet[k] = true
	}
	recSet := map[string]bool{}
	for _, k := range set.RecentModels {
		recSet[k] = true
	}
	pickModel := func(id string) {
		a.do(a.C.Update(proto.SessionUpdate{Model: &id}))
		a.recordRecentModel(account, id)
		a.modelMenu = false
	}
	pop := ui.Popover(c, b, &a.modelMenu, func() {
		ui.Column(c).Width(300).Gap(4).Children(func() {
			filtered := a.filterModels(a.C.Models, a.modelQuery)
			ui.Row(c).Height(32).Padding(0, 8).Gap(6).Radius(8).Background(p.Chip).AlignItems(ui.Center).Children(func() {
				sym(c, "search", 13, p.Mute)
				ui.TextInputBase(c.Key("model-search"), &a.modelQuery).Bind(&a.modelSearch).
					Label(locales.Search()).Placeholder(locales.SearchModels()).PlaceholderColor(p.Mute).
					FontSize(13).TextColor(p.Fg).Grow(1).SingleLine().AutoFocus().
					OnTextKey(func(k ui.TextKey) bool {
						switch {
						case k.Key == ui.KeyEscape && k.Mods == 0:
							a.modelMenu = false
							return true
						case k.Key == ui.KeyEnter && k.Mods == 0 && len(filtered) > 0:
							pickModel(filtered[0].ID)
							return true
						}
						return false
					})
			})
			switch {
			case a.modelsPending != "":
				ui.Text(c, locales.Loading()).FontSize(12.5).TextColor(p.Mute).Padding(6, 8)
			case a.modelsError != "":
				ui.Column(c).Padding(6, 8).Gap(2).Children(func() {
					ui.Text(c, locales.ModelsLoadFailed()).FontSize(12.5).FontWeight(500).TextColor(p.Minus)
					ui.Text(c, a.modelsError).FontSize(11.5).TextColor(p.Mute)
				})
			case len(a.C.Models) == 0:
				ui.Text(c, locales.NoModels()).FontSize(12.5).TextColor(p.Mute).Padding(6, 8)
			default:
				renderModelRow := func(sec string, m proto.Model) {
					name := m.Name
					if name == "" {
						name = m.ID
					}
					key := modelKey(account, m.ID)
					fav := favSet[key]
					selected := m.ID == current || current == "" && m.Default
					ui.Row(c.Key("m-row:"+sec+":"+m.ID)).Height(30).Padding(0, 4).Gap(2).AlignItems(ui.Center).Children(func() {
						btn := ui.ButtonBase(c.Key("m-pick:"+sec+":"+m.ID)).Row().Grow(1).MinWidth(0).Height(28).Padding(0, 6).Gap(6).Radius(6).AlignItems(ui.Center).Label(name).Selected(selected)
						press(btn)
						focusRing(btn, p)
						if selected {
							btn.Background(p.Sel)
						} else if btn.Hovered() {
							btn.Background(p.Hover)
						}
						btn.Children(func() {
							if selected {
								sym(c, "check", 12, p.Acc)
							} else {
								ui.Box(c).Width(12).Shrink(0)
							}
							ui.Text(c, name).FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
							if ctx := contextSize(m.Context); ctx != "" {
								ui.Text(c, ctx).FontSize(11).TextColor(p.Mute).SingleLine().Shrink(0)
							}
						})
						if btn.Clicked() {
							pickModel(m.ID)
						}
						pinTip := locales.FavoriteModel()
						pinCol := p.Faint
						if fav {
							pinTip = locales.UnfavoriteModel()
							pinCol = p.Acc
						}
						pinBtn := ui.ButtonBase(c.Key("m-pin:"+sec+":"+m.ID)).Size(24, 24).Radius(6).Center().Shrink(0).Label(pinTip).Tooltip(pinTip)
						press(pinBtn)
						if pinBtn.Hovered() {
							pinBtn.Background(p.Hover)
						}
						pinBtn.Children(func() { sym(c, "pin", 12, pinCol) })
						if pinBtn.Clicked() {
							a.toggleFavoriteModel(account, m.ID)
						}
					})
				}
				renderSection := func(sec, title string, list []proto.Model) {
					if len(list) == 0 {
						return
					}
					if title != "" {
						ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(p.Mute).Padding(6, 8, 2, 8).SingleLine()
					}
					for _, m := range list {
						renderModelRow(sec, m)
					}
				}
				ui.Scroll(c).MaxHeight(260).Children(func() {
					if strings.TrimSpace(a.modelQuery) != "" {
						renderSection("search", "", filtered)
						return
					}
					var favs, recents, rest []proto.Model
					for _, m := range a.C.Models {
						k := modelKey(account, m.ID)
						switch {
						case favSet[k]:
							favs = append(favs, m)
						case recSet[k]:
							recents = append(recents, m)
						default:
							rest = append(rest, m)
						}
					}
					renderSection("fav", locales.FavoriteModels(), favs)
					renderSection("rec", locales.RecentModels(), recents)
					allTitle := ""
					if len(favs) > 0 || len(recents) > 0 {
						allTitle = locales.AllModels()
					}
					renderSection("all", allTitle, rest)
				})
			}

			ui.Box(c).FillWidth().Height(.5).Background(p.Edge).Margin(4, 0)
			unsupported := a.reasoningUnsupported()
			effRow := ui.Row(c).Padding(4, 6).Gap(4).AlignItems(ui.Center).Label(locales.ReasoningEffort()).Disabled(unsupported)
			if unsupported {
				effRow.Tooltip(locales.NoEffortSupport())
			}
			effRow.Children(func() {
				ui.Text(c, locales.ReasoningEffort()).FontSize(12).TextColor(p.Mute).SingleLine()
				ui.Spacer(c)
				for _, ef := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
					on := a.C.Current.Effort == ef
					eb := ui.ButtonBase(c.Key("eff:"+ef)).Height(22).Padding(0, 6).Radius(5).Center().Label(effortLabel(ef)).Disabled(unsupported)
					if on {
						eb.Background(p.Sel)
					} else if eb.Hovered() && !unsupported {
						eb.Background(p.Hover)
					}
					eb.Children(func() {
						col := p.Fg2
						if on {
							col = p.Fg
						} else if unsupported {
							col = p.Faint
						}
						ui.Text(c, effortLabel(ef)).FontSize(11.5).TextColor(col).SingleLine()
					})
					if eb.Clicked() && !unsupported {
						e := ef
						a.do(a.C.Update(proto.SessionUpdate{Effort: &e}))
					}
				}
			})
		})
	})
	pop.AttachTo(b, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).
		Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(8).NoShadow()
	lift(pop, p)
	rim(pop, p)
}

// modelItems lists an account's models in a menu, asking for them once
// per opening, with current checked.
func (a *App) modelItems(m *ui.Menu, account, current string, choose func(id string)) {
	if !a.modelsRequested {
		a.modelsRequested = true
		a.modelsAccount, a.modelsError = account, ""
		cmds, err := a.C.ListModels(account)
		if err == nil {
			a.modelsPending = cmds[0].ID
		} else {
			a.modelsError = err.Error()
		}
		a.do(cmds, err)
	}
	switch {
	case a.modelsPending != "":
		m.Item(locales.Loading()).Disabled(true)
	case a.modelsError != "":
		m.Item(locales.ModelsLoadFailed()).Subtitle(a.modelsError).Disabled(true)
	case len(a.C.Models) == 0:
		m.Item(locales.NoModels()).Disabled(true)
	default:
		for _, model := range a.C.Models {
			name := model.Name
			if name == "" {
				name = model.ID
			}
			selected := model.ID == current || current == "" && model.Default
			item := m.Item(name).ID(model.ID).Checked(selected).Trailing(contextSize(model.Context))
			if item.Chosen() {
				choose(model.ID)
			}
		}
	}
}

func contextSize(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 1_000_000:
		return fmt.Sprintf("%gM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%gK", float64(n)/1_000)
	default:
		return fmt.Sprint(n)
	}
}

func (a *App) reasoningUnsupported() bool {
	if a.modelsLoadedAccount != a.modelAccount() {
		return false
	}
	for _, model := range a.C.Models {
		if model.ID == a.C.Current.Model || a.C.Current.Model == "" && model.Default {
			return !model.Reasoning
		}
	}
	return false
}

func (a *App) effortPill(c *ui.Context, label string) {
	unsupported := a.reasoningUnsupported()
	container := ui.Box(c)
	if unsupported {
		// Disabled controls do not show tooltips in mygo; their container can.
		container.Tooltip(locales.NoEffortSupport())
	}
	container.Children(func() {
		b := a.pill(c, "effort", "", label, false).Disabled(!a.C.CanAct() || unsupported)
		if unsupported {
			b.Description(locales.NoEffortSupport())
		}
		b.PopoverMenuButton(func(m *ui.Menu) {
			for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
				if m.Item(effortLabel(effort)).ID("effort:" + effort).Checked(a.C.Current.Effort == effort).Chosen() {
					a.do(a.C.Update(proto.SessionUpdate{Effort: &effort}))
				}
			}
		}, a.menuOptions(&a.effortMenu))
	})
}

func (a *App) modePill(c *ui.Context, label string) {
	p := a.p
	var b ui.Element
	if a.C.Current != nil && a.C.Current.Mode == "full" {
		b = ui.ButtonBase(c.Key("pill:mode")).Row().Height(28).Padding(0, 9).Gap(5).Radius(14).AlignItems(ui.Center).Label(label)
		press(b)
		focusRing(b, p)
		if b.Hovered() {
			b.Background(p.Minus.Alpha(.08))
		}
		b.Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.Minus).SingleLine()
			sym(c, "selector", 11, p.Minus.Alpha(.6))
		})
	} else {
		b = a.pill(c, "mode", "", label, true)
	}
	b.Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		for _, mode := range []struct{ id, description string }{
			{"ask", locales.ModeAskShort()},
			{"auto", locales.ModeAutoShort()},
			{"full", locales.ModeFullShort()},
		} {
			if m.Item(modeName(mode.id)).ID(mode.id).Subtitle(mode.description).Checked(a.C.Current.Mode == mode.id).Chosen() {
				a.do(a.C.Update(proto.SessionUpdate{Mode: &mode.id}))
			}
		}
	}, a.menuOptions(&a.modeMenu))
}

func phaseText(phase client.Phase) string {
	switch phase {
	case client.Starting:
		return locales.PhaseStarting()
	case client.Handshaking:
		return locales.PhaseConnecting()
	case client.Syncing:
		return locales.PhaseSyncing()
	case client.Ready:
		return locales.PhaseReady()
	case client.Disconnected:
		return locales.PhaseDisconnected()
	default:
		return locales.PhaseUnknown()
	}
}

// folderPill moves a session not yet begun to another folder: one the
// sidebar knows, or one chosen with the open panel.
func (a *App) folderPill(c *ui.Context, cur *proto.Session) {
	p := a.p
	curDir := cur.Cwd
	if cur.Repo != "" {
		curDir = cur.Repo
	}
	name := filepath.Base(curDir)
	b := ui.ButtonBase(c.Key("pill:folder")).Row().Height(28).Padding(0, 9).Gap(6).Radius(14).AlignItems(ui.Center).
		Label(name).Disabled(!a.C.CanAct())
	press(b)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		folderDot(c, p, name)
		ui.Text(c, name).FontSize(13).TextColor(p.Fg2).SingleLine()
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		seen := map[string]bool{}
		for _, s := range a.C.Sessions {
			dirPath := s.Cwd
			if s.Repo != "" {
				dirPath = s.Repo
			}
			if seen[dirPath] {
				continue
			}
			seen[dirPath] = true
			if m.Item(filepath.Base(dirPath)).ID("folder:" + dirPath).Checked(dirPath == curDir).Chosen() {
				a.moveTo(dirPath)
			}
		}
		if a.pickDir != nil && a.update != nil {
			m.Separator()
			if m.Item(locales.ChooseFolder()).ID("folder:choose").Chosen() {
				go func() {
					if dir := a.pickDir(); dir != "" {
						a.update(func() { a.moveTo(dir) })
					}
				}()
			}
		}
	}, a.menuOptions(&a.folderMenu))
}

// worktreePill toggles whether a session not yet begun runs in an isolated git worktree.
func (a *App) worktreePill(c *ui.Context, cur *proto.Session) {
	icon, label := "device-laptop", locales.Local()
	if cur.Worktree {
		icon, label = "git-branch", locales.Worktree()
	}
	a.pill(c, "worktree", icon, label, true).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		for _, opt := range []struct {
			on          bool
			label, desc string
		}{
			{false, locales.Local(), locales.LocalSub()},
			{true, locales.Worktree(), locales.WorktreeSub()},
		} {
			wt := opt.on
			if m.Item(opt.label).ID(fmt.Sprintf("worktree:%v", wt)).Subtitle(opt.desc).Checked(cur.Worktree == wt).Chosen() {
				if cur.Worktree != wt {
					a.do(a.C.Update(proto.SessionUpdate{Worktree: &wt}))
				}
			}
		}
	}, a.menuOptions(&a.worktreeMenu))
}

// branchPill switches the git branch of a new session before it starts.
func (a *App) branchPill(c *ui.Context, cur *proto.Session) {
	if a.C.Workspace.Branch == "" || cur.Worktree {
		return
	}
	a.pill(c, "branch", "git-branch", a.C.Workspace.Branch, true).Disabled(!a.C.CanAct() || len(a.C.Workspace.Branches) == 0).PopoverMenuButton(func(m *ui.Menu) {
		for _, br := range a.C.Workspace.Branches {
			branch := br
			if m.Item(branch).ID("branch:" + branch).Checked(branch == a.C.Workspace.Branch).Chosen() {
				if branch != a.C.Workspace.Branch {
					a.do(a.C.Checkout(branch))
				}
			}
		}
	}, a.menuOptions(&a.branchMenu))
}

// moveTo starts the session over in dir: a new one there, and the empty
// one here goes.
func (a *App) moveTo(dir string) {
	cur := a.C.Current
	if cur == nil || dir == cur.Cwd {
		return
	}
	// Both are asked while the client is ready; the new one goes first.
	var drop []proto.Command
	var dropErr error
	if len(a.C.Entries) == 0 {
		drop, dropErr = a.C.Delete(cur.ID)
	}
	cmds, err := a.C.NewSessionIn(dir)
	if a.do(cmds, err); err == nil && drop != nil {
		a.do(drop, dropErr)
	}
}

// pickFolder is the system's open panel, for one folder.
func pickFolder() string {
	paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: locales.ChooseFolder(), Directory: true, CreateDirectories: true})
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
