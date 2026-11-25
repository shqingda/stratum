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

func (a *App) closeComposerPopovers(keep string) {
	if keep != "model" {
		a.menu.modelMenu = false
	}
	if keep != "agent" {
		a.menu.agentMenu = false
	}
	if keep != "mode" {
		a.menu.modeMenu = false
	}
	if keep != "folder" {
		a.menu.folderMenu = false
	}
	if keep != "worktree" {
		a.menu.worktreeMenu = false
	}
	if keep != "branch" {
		a.menu.branchMenu = false
	}
}

func (a *App) syncMenus() {
	id := ""
	if a.C.Current != nil {
		id = a.C.Current.ID
	}
	if a.menu.modelsLoadedAccount == "" && len(a.C.Models) > 0 {
		a.menu.modelsLoadedAccount = a.modelAccount()
	}
	if a.C.Phase == client.Disconnected {
		a.menu.modelsPending = ""
	}
	if id != a.menu.session || !a.C.CanAct() {
		a.menu.modelMenu, a.menu.modeMenu, a.menu.detailMenu, a.menu.worktreeMenu, a.menu.branchMenu = false, false, false, false, false
		a.menu.modelsRequested = false
		a.menu.hoverSub = ""
		a.menu.session = id
	}
}

func (a *App) receive(f proto.Frame) {
	a.send(a.C.Receive(f))
	a.watch()
	if f.Type == proto.FrameResponse && a.menu.modelsPending != "" && f.ID == a.menu.modelsPending {
		a.menu.modelsPending = ""
		if f.Error != nil {
			a.menu.modelsError = f.Error.Message
		} else {
			a.menu.modelsLoadedAccount = a.menu.modelsAccount
		}
	}
	if a.C.Phase == client.Disconnected {
		a.menu.modelsPending = ""
	}
}

func (a *App) menuOptions(open *bool) ui.PopoverMenuButtonOptions {
	return ui.PopoverMenuButtonOptions{Open: open, Panel: func(panel ui.Element) {
		panel.Background(a.p.SurfaceContainerHigh).Radius(16).FontSize(13).NoShadow()
		elevation2(panel, a.p)
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
	if a.menu.modelsAccount != a.modelAccount() {
		a.menu.modelsRequested = false
	}
	p := a.p
	effort := effortLabel(a.C.Current.Effort)
	b := ui.ButtonBase(c.Key("pill:model")).Row().Height(30).Padding(0, 10).Gap(5).Radius(10).AlignItems(ui.Center).
		Label(label).Disabled(!a.C.CanAct())
	m3Focus(b, p)
	pillBg := ui.Transparent
	if a.menu.modelMenu {
		pillBg = p.SurfaceContainerHigh
	}
	m3State(b, p, pillBg, false)
	b.Children(func() {
		fg, sub := p.OnSurfaceVariant, p.OnSurfaceVariant
		if a.menu.modelMenu {
			fg = p.OnSurface
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(fg).SingleLine()
		ui.Text(c, effort).FontSize(13).TextColor(sub).SingleLine()
		sym(c, "chevron-down", 12, sub)
	})
	if b.Clicked() {
		a.menu.modelMenu = !a.menu.modelMenu
		if a.menu.modelMenu {
			a.closeComposerPopovers("model")
			a.menu.modelQuery = ""
			a.menu.hoverSub = ""
		}
	}
	account := a.modelAccount()
	if a.menu.modelMenu && !a.menu.modelsRequested {
		a.menu.modelsRequested = true
		a.menu.modelsAccount, a.menu.modelsError = account, ""
		cmds, err := a.C.ListModels(account)
		if err == nil {
			a.menu.modelsPending = cmds[0].ID
		} else {
			a.menu.modelsError = err.Error()
		}
		a.do(cmds, err)
	}
	if !a.menu.modelMenu {
		a.menu.modelsRequested = false
		a.menu.modelQuery = ""
		a.menu.hoverSub = ""
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
		a.menu.modelMenu = false
		a.menu.hoverSub = ""
	}
	var activeModel proto.Model
	for _, m := range a.C.Models {
		if m.ID == current || (current == "" && m.Default) {
			activeModel = m
			break
		}
	}

	pop := ui.PopoverBase(c, b, &a.menu.modelMenu, func(panel ui.Element) {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, locales.SettingsModels()).FontSize(11.5).FontWeight(600).LetterSpacing(.02).
				TextColor(p.OnSurfaceVariant).Padding(4, 10, 2, 10).SingleLine()
			filtered := a.filterModels(a.C.Models, a.menu.modelQuery)
			if len(a.C.Models) > 4 || a.menu.modelQuery != "" {
				ui.Row(c).Height(32).Padding(0, 10).Margin(0, 4, 2, 4).Gap(6).Radius(16).Background(p.SurfaceContainerHighest).AlignItems(ui.Center).Children(func() {
					sym(c, "search", 13, p.OnSurfaceVariant)
					ui.TextInputBase(c.Key("model-search"), &a.menu.modelQuery).Bind(&a.menu.modelSearch).
						Label(locales.Search()).Placeholder(locales.SearchModels()).PlaceholderColor(p.OnSurfaceVariant).
						FontSize(13).TextColor(p.OnSurface).Grow(1).SingleLine().AutoFocus().
						OnTextKey(func(k ui.TextKey) bool {
							switch {
							case k.Key == ui.KeyEscape && k.Mods == 0:
								a.menu.modelMenu = false
								return true
							case k.Key == ui.KeyEnter && k.Mods == 0 && len(filtered) > 0:
								pickModel(filtered[0].ID)
								return true
							}
							return false
						})
				})
			}
			switch {
			case a.menu.modelsPending != "":
				ui.Text(c, locales.Loading()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Padding(8, 10)
			case a.menu.modelsError != "":
				ui.Column(c).Padding(8, 10).Gap(2).Children(func() {
					ui.Text(c, locales.ModelsLoadFailed()).FontSize(12.5).FontWeight(500).TextColor(p.Minus)
					ui.Text(c, a.menu.modelsError).FontSize(11.5).TextColor(p.OnSurfaceVariant)
				})
			case len(a.C.Models) == 0:
				ui.Text(c, locales.NoModels()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Padding(8, 10)
			default:
				canPin := len(a.C.Models) > 1
				renderModelRow := func(sec string, m proto.Model) {
					name := m.Name
					if name == "" {
						name = m.ID
					}
					key := modelKey(account, m.ID)
					fav := favSet[key]
					selected := m.ID == current || current == "" && m.Default
					row := ui.Row(c.Key("m-row:"+sec+":"+m.ID)).Height(36).Padding(0, 8, 0, 12).Gap(6).Radius(10).AlignItems(ui.Center)
					hov := row.Hovered()
					fg, sub := p.OnSurface, p.OnSurfaceVariant
					if selected {
						selBg := p.Surface
						if p.Dark {
							selBg = p.SurfaceContainerHighest
						}
						row.Background(selBg)
					} else if hov {
						row.Background(p.Hover)
					}
					row.Children(func() {
						if selected {
							ui.Box(c).Absolute().Left(0).Top(10).Size(3, 16).Radius(1.5).Background(p.Primary)
						}
						btn := ui.ButtonBase(c.Key("m-pick:" + sec + ":" + m.ID)).Row().Grow(1).MinWidth(0).Height(36).Gap(6).AlignItems(ui.Center).Label(name).Checked(selected)
						m3Focus(btn, p)
						btn.Children(func() {
							weight := 450
							if selected {
								weight = 600
							}
							ui.Text(c, name).FontSize(13.5).FontWeight(weight).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
							if ctx := contextSize(m.Context); ctx != "" {
								ui.Text(c, ctx).FontSize(11.5).FontFeatures("tnum").TextColor(sub).SingleLine().Shrink(0)
							}
						})
						if btn.Clicked() {
							pickModel(m.ID)
						}
						if canPin && (fav || hov) {
							pinTip := locales.FavoriteModel()
							pinCol := sub
							if fav {
								pinTip = locales.UnfavoriteModel()
								pinCol = p.Primary
							}
							pinBtn := ui.ButtonBase(c.Key("m-pin:"+sec+":"+m.ID)).Size(22, 22).Radius(6).Center().Shrink(0).Label(pinTip).Tooltip(pinTip)
							m3State(pinBtn, p, ui.Color{}, false)
							pinBtn.Children(func() { sym(c, "pin", 12, pinCol) })
							if pinBtn.Clicked() {
								a.toggleFavoriteModel(account, m.ID)
							}
						}
					})
				}
				renderSection := func(sec, title string, list []proto.Model) {
					if len(list) == 0 {
						return
					}
					if title != "" {
						ui.Text(c, title).FontSize(11).FontWeight(600).LetterSpacing(.03).TextColor(p.OnSurfaceVariant).Padding(6, 10, 2, 10).SingleLine()
					}
					for _, m := range list {
						renderModelRow(sec, m)
					}
				}
				ui.Scroll(c).MaxHeight(240).Children(func() {
					if strings.TrimSpace(a.menu.modelQuery) != "" {
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
					sections := 0
					for _, n := range []int{len(favs), len(recents), len(rest)} {
						if n > 0 {
							sections++
						}
					}
					if sections <= 1 {
						renderSection("fav", "", favs)
						renderSection("rec", "", recents)
						renderSection("all", "", rest)
						return
					}
					renderSection("fav", locales.FavoriteModels(), favs)
					renderSection("rec", locales.RecentModels(), recents)
					renderSection("all", locales.AllModels(), rest)
				})
			}

			// Inline Reasoning Effort segmented bar (no hover-only flyout)
			ui.Box(c).FillWidth().Height(1).Background(p.OutlineVariant.Alpha(.25)).Margin(2, 4)
			if a.reasoningUnsupported() {
				ui.Row(c).Padding(4, 10, 4, 10).Gap(6).AlignItems(ui.Center).Label(locales.ReasoningEffort()).Disabled(true).Children(func() {
					ui.Text(c, locales.ReasoningEffort()).FontSize(12).TextColor(p.OnSurfaceVariant.Alpha(.55)).SingleLine().Shrink(0)
					ui.Spacer(c)
					ui.Text(c, locales.NoEffortSupport()).FontSize(11.5).TextColor(p.OnSurfaceVariant.Alpha(.55)).SingleLine().Ellipsis("…").Shrink(1)
				})
			} else {
				ui.Column(c).FillWidth().Padding(2, 4, 4, 4).Gap(6).Children(func() {
					ui.Text(c, locales.ReasoningEffort()).FontSize(11.5).FontWeight(600).LetterSpacing(.02).
						TextColor(p.OnSurfaceVariant).Padding(0, 6).SingleLine()
					trackBg := p.SurfaceContainerHighest
					if !p.Dark {
						trackBg = p.SurfaceContainer
					}
					effOpts := []string{"", "minimal", "low", "medium", "high", "xhigh"}
					ui.Row(c.Key("eff-track")).FillWidth().Height(32).Padding(3).Radius(14).Background(trackBg).Gap(2).Children(func() {
						for _, ef := range effOpts {
							on := a.C.Current.Effort == ef
							eb := ui.ButtonBase(c.Key("eff:"+ef)).Row().Grow(1).Shrink(0).Height(26).Padding(0, 7).Radius(11).
								Center().Label(effortLabel(ef)).Checked(on)
							m3Focus(eb, p)
							fg, weight := p.OnSurfaceVariant, 500
							if on {
								selBg := p.Surface
								if p.Dark {
									selBg = p.SurfaceContainerHighest
								}
								eb.Background(selBg)
								fg, weight = p.OnSurface, 600
							} else {
								m3State(eb, p, ui.Transparent, false)
							}
							eb.Children(func() {
								ui.Text(c, effortLabel(ef)).FontSize(11.5).FontWeight(weight).TextColor(fg).SingleLine()
							})
							if eb.Clicked() {
								e := ef
								a.do(a.C.Update(proto.SessionUpdate{Effort: &e}))
							}
						}
					})
				})
			}

			// Inline Quota & Context Usage summary
			acc := a.account()
			hasQuota := acc != nil && acc.Quota != nil && acc.Quota.RemainingPct >= 0
			mInfo, okInfo := a.modelInfo()
			window := 0
			if okInfo && mInfo.Context > 0 {
				window = mInfo.Context
			} else if activeModel.Context > 0 {
				window = activeModel.Context
			}
			hasCtx := a.C.Current != nil && window > 0
			if hasQuota || hasCtx {
				ui.Box(c).FillWidth().Height(1).Background(p.OutlineVariant.Alpha(.25)).Margin(2, 4)
				ui.Column(c).FillWidth().Padding(4, 10, 6, 10).Gap(8).Children(func() {
					if hasQuota {
						q := acc.Quota
						pct := max(0, min(100, q.RemainingPct))
						qTitle := locales.UsageQuota()
						if acc.Name != "" {
							qTitle = acc.Name
						}
						ui.Column(c).FillWidth().Gap(4).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Text(c, qTitle).FontSize(12).FontWeight(500).TextColor(p.OnSurface).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
								pctCol := p.OnSurfaceVariant
								if pct < 15 {
									pctCol = p.Minus
								}
								ui.Text(c, locales.QuotaRemaining(pct)).FontSize(11.5).FontWeight(600).FontFeatures("tnum").TextColor(pctCol).SingleLine().Shrink(0)
							})
							barCol := p.Primary
							if pct < 15 {
								barCol = p.Minus
							}
							ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.SurfaceContainerHighest).Clip().Children(func() {
								ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(barCol)
							})
							if reset := formatQuotaReset(a.now(), q); reset != "" {
								ui.Text(c, reset).FontSize(11).TextColor(p.OnSurfaceVariant).SingleLine()
							}
						})
					}
					if hasCtx {
						used := a.C.LastInput
						pct := min(100, (used*100+window/2)/window)
						ui.Column(c).FillWidth().Gap(4).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Text(c, locales.UsageSession()).FontSize(12).FontWeight(500).TextColor(p.OnSurface).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
								ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(11.5).FontWeight(600).FontFeatures("tnum").TextColor(p.OnSurfaceVariant).SingleLine().Shrink(0)
							})
							barCol := p.Secondary
							if pct >= 85 {
								barCol = p.Minus
							}
							ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.SurfaceContainerHighest).Clip().Children(func() {
								ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(barCol)
							})
							subLine := locales.ContextUsed(tokens(used), tokens(window))
							if cost := a.turnCost(a.C.Usage); cost != "" {
								subLine += " · " + cost
							}
							ui.Text(c, subLine).FontSize(11).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
						})
					}
				})
			}
		})
	})
	pop.AttachTo(b, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 8, 0).
		Width(376).Background(p.SurfaceContainerHigh).Radius(18).Padding(8).NoShadow()
	if !a.reduceMotion() {
		pop.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8, Scale: .97, Opacity: 0}, Animation: &animStandard})
	}
	elevation2(pop, p)
}

// modelItems lists an account's models in a menu, asking for them once
// per opening, with current checked.
func (a *App) modelItems(m *ui.Menu, account, current string, choose func(id string)) {
	if !a.menu.modelsRequested {
		a.menu.modelsRequested = true
		a.menu.modelsAccount, a.menu.modelsError = account, ""
		cmds, err := a.C.ListModels(account)
		if err == nil {
			a.menu.modelsPending = cmds[0].ID
		} else {
			a.menu.modelsError = err.Error()
		}
		a.do(cmds, err)
	}
	switch {
	case a.menu.modelsPending != "":
		m.Item(locales.Loading()).Disabled(true)
	case a.menu.modelsError != "":
		m.Item(locales.ModelsLoadFailed()).Subtitle(a.menu.modelsError).Disabled(true)
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
	if a.menu.modelsLoadedAccount != a.modelAccount() {
		return false
	}
	for _, model := range a.C.Models {
		if model.ID == a.C.Current.Model || a.C.Current.Model == "" && model.Default {
			return !model.Reasoning
		}
	}
	return false
}

func (a *App) modePill(c *ui.Context, label string) {
	p := a.p
	full := a.C.Current != nil && a.C.Current.Mode == "full"
	b := ui.ButtonBase(c.Key("pill:mode")).Row().Height(30).Padding(0, 10).Gap(5).Radius(10).
		AlignItems(ui.Center).Label(label)
	m3Focus(b, p)
	base := ui.Transparent
	if a.menu.modeMenu {
		base = p.SurfaceContainerHigh
	}
	m3State(b, p, base, false)
	b.Children(func() {
		col := p.OnSurfaceVariant
		weight := 500
		if full {
			col = p.Error
			weight = 600
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(weight).TextColor(col).SingleLine()
		sym(c, "chevron-down", 12, col)
	})
	wasOpen := a.menu.modeMenu
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
	}, a.menuOptions(&a.menu.modeMenu))
	if a.menu.modeMenu && !wasOpen {
		a.closeComposerPopovers("mode")
	}
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
	b := ui.ButtonBase(c.Key("pill:folder")).Row().Height(28).Padding(0, 10).Gap(6).Radius(8).AlignItems(ui.Center).
		Label(name).Disabled(!a.C.CanAct())
	m3Focus(b, p)
	m3State(b, p, ui.Color{}, false)
	b.Children(func() {
		projectSwatch(c, p, name)
		ui.Text(c, name).FontSize(12.5).TextColor(p.OnSurfaceVariant).SingleLine()
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
	}, a.menuOptions(&a.menu.folderMenu))
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
	}, a.menuOptions(&a.menu.worktreeMenu))
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
	}, a.menuOptions(&a.menu.branchMenu))
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
