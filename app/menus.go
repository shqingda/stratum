package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	if a.menu.modelsAccount != a.modelAccount() {
		a.menu.modelsRequested = false
	}
	p := a.p
	effort := effortLabel(a.C.Current.Effort)
	b := ui.ButtonBase(c.Key("pill:model")).Row().Height(28).Padding(0, 9).Gap(5).Radius(14).AlignItems(ui.Center).
		Label(label).Disabled(!a.C.CanAct())
	focusRing(b, p)
	switch {
	case a.menu.modelMenu:
		b.Background(p.Sel)
	case b.Hovered():
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
		ui.Text(c, effort).FontSize(13).TextColor(p.Mute).SingleLine()
		sym(c, "chevron-down", 11, p.Mute)
	})
	if b.Clicked() {
		a.menu.modelMenu = !a.menu.modelMenu
		if a.menu.modelMenu {
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
		var nextHover string
		var subAnchor ui.Element
		var subModel proto.Model
		var usageRow ui.Element

		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Text(c, locales.SettingsModels()).FontSize(11.5).FontWeight(500).LetterSpacing(.02).
				TextColor(p.Mute).Padding(4, 10, 4, 10).SingleLine()
			filtered := a.filterModels(a.C.Models, a.menu.modelQuery)
			if len(a.C.Models) > 4 || a.menu.modelQuery != "" {
				ui.Row(c).Height(30).Padding(0, 8).Margin(0, 4, 4, 4).Gap(6).Radius(8).Background(p.Chip).AlignItems(ui.Center).Children(func() {
					sym(c, "search", 13, p.Mute)
					ui.TextInputBase(c.Key("model-search"), &a.menu.modelQuery).Bind(&a.menu.modelSearch).
						Label(locales.Search()).Placeholder(locales.SearchModels()).PlaceholderColor(p.Mute).
						FontSize(13).TextColor(p.Fg).Grow(1).SingleLine().AutoFocus().
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
				ui.Text(c, locales.Loading()).FontSize(12.5).TextColor(p.Mute).Padding(8, 10)
			case a.menu.modelsError != "":
				ui.Column(c).Padding(8, 10).Gap(2).Children(func() {
					ui.Text(c, locales.ModelsLoadFailed()).FontSize(12.5).FontWeight(500).TextColor(p.Minus)
					ui.Text(c, a.menu.modelsError).FontSize(11.5).TextColor(p.Mute)
				})
			case len(a.C.Models) == 0:
				ui.Text(c, locales.NoModels()).FontSize(12.5).TextColor(p.Mute).Padding(8, 10)
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
					row := ui.Row(c.Key("m-row:"+sec+":"+m.ID)).Height(32).Padding(0, 8, 0, 10).Gap(6).Radius(8).AlignItems(ui.Center)
					hov := row.Hovered()
					subKey := "model:" + m.ID
					if hov {
						if m.Reasoning {
							nextHover = subKey
						} else {
							nextHover = "none"
						}
					}
					if (hov && m.Reasoning) || (nextHover == "" && a.menu.hoverSub == subKey) {
						subAnchor = row
						subModel = m
					}
					if selected {
						row.Background(p.Sel)
					} else if hov || (nextHover == "" && a.menu.hoverSub == subKey) {
						row.Background(p.Hover)
					}
					row.Children(func() {
						btn := ui.ButtonBase(c.Key("m-pick:" + sec + ":" + m.ID)).Row().Grow(1).MinWidth(0).Height(32).Gap(6).AlignItems(ui.Center).Label(name).Checked(selected)
						focusRing(btn, p)
						btn.Children(func() {
							weight := 400
							if selected {
								weight = 500
							}
							ui.Text(c, name).FontSize(13.5).FontWeight(weight).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
							if ctx := contextSize(m.Context); ctx != "" {
								ui.Text(c, ctx).FontSize(11.5).FontFeatures("tnum").TextColor(p.Mute).SingleLine().Shrink(0)
							}
						})
						if btn.Clicked() {
							pickModel(m.ID)
						}
						if canPin && (fav || hov) {
							pinTip := locales.FavoriteModel()
							pinCol := p.Mute
							if fav {
								pinTip = locales.UnfavoriteModel()
								pinCol = p.Acc
							}
							pinBtn := ui.ButtonBase(c.Key("m-pin:"+sec+":"+m.ID)).Size(22, 22).Radius(5).Center().Shrink(0).Label(pinTip).Tooltip(pinTip)
							if pinBtn.Hovered() {
								pinBtn.Background(p.Hover)
							}
							pinBtn.Children(func() { sym(c, "pin", 12, pinCol) })
							if pinBtn.Clicked() {
								a.toggleFavoriteModel(account, m.ID)
							}
						}
						ui.Box(c).Size(16, 16).Center().Shrink(0).Children(func() {
							if selected {
								sym(c, "check", 14, p.Fg)
							} else if m.Reasoning {
								sym(c, "chevron-right", 12, p.Mute)
							}
						})
					})
				}
				renderSection := func(sec, title string, list []proto.Model) {
					if len(list) == 0 {
						return
					}
					if title != "" {
						ui.Text(c, title).FontSize(11).FontWeight(600).LetterSpacing(.03).TextColor(p.Mute).Padding(8, 10, 3, 10).SingleLine()
					}
					for _, m := range list {
						renderModelRow(sec, m)
					}
				}
				ui.Scroll(c).MaxHeight(280).Children(func() {
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

			ui.Box(c).FillWidth().Height(.5).Background(p.Edge).Margin(4, 4)
			if a.reasoningUnsupported() {
				effRow := ui.Row(c).Padding(4, 10, 2, 10).Gap(6).AlignItems(ui.Center).Label(locales.ReasoningEffort()).Disabled(true).Tooltip(locales.NoEffortSupport())
				effRow.Children(func() {
					ui.Text(c, locales.ReasoningEffort()).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0)
					ui.Spacer(c)
					ui.Text(c, locales.NoEffortSupport()).FontSize(11.5).TextColor(p.Faint).SingleLine().Ellipsis("…").Shrink(1)
				})
			}
			usageRow = ui.ButtonBase(c.Key("m-usage")).Row().FillWidth().Height(32).Padding(0, 8, 0, 10).Gap(8).Radius(8).
				AlignItems(ui.Center).Label(locales.UsageView())
			focusRing(usageRow, p)
			uHov := usageRow.Hovered()
			if uHov {
				nextHover = "usage"
			}
			if uHov || (nextHover == "" && a.menu.hoverSub == "usage") {
				usageRow.Background(p.Hover)
			}
			usageRow.Children(func() {
				sym(c, "speedometer", 15, p.Fg2)
				ui.Text(c, locales.UsageView()).FontSize(13.5).TextColor(p.Fg).SingleLine().Grow(1)
				sym(c, "chevron-right", 12, p.Mute)
			})
			if usageRow.Clicked() {
				a.menu.modelMenu = false
				a.menu.hoverSub = ""
				a.openSettings()
			}
		})

		activeSub := nextHover
		if activeSub == "" {
			activeSub = a.menu.hoverSub
		}
		if activeSub == "none" {
			activeSub = ""
		}
		subWrapHovered := false
		switch {
		case activeSub == "usage":
			ui.Overlay(c, func() {
				// 14px left padding bridges panel padding (6px) + visual gap (8px) cleanly across hover transitions.
				subWrap := ui.Box(c.Key("model-sub-wrap")).AttachTo(usageRow, ui.AnchorBottomEnd, ui.AnchorBottomStart).Padding(0, 0, 0, 14)
				subWrapHovered = subWrap.Hovered()
				subWrap.Children(func() {
					usageCard := ui.Column(c.Key("usage-card")).Width(236).Gap(10).Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(10, 12)
					lift(usageCard, p)
					rim(usageCard, p)
					if !a.reduceMotion() {
						usageCard.Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 120 * time.Millisecond})
					}
					usageCard.Children(func() {
						ui.Text(c, locales.UsageTitle()).FontSize(11.5).FontWeight(500).LetterSpacing(.02).
							TextColor(p.Mute).SingleLine()
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
						if hasQuota {
							q := acc.Quota
							pct := max(0, min(100, q.RemainingPct))
							qTitle := locales.UsageQuota()
							if acc.Name != "" {
								qTitle = acc.Name
							}
							ui.Column(c).FillWidth().Gap(5).Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
									ui.Text(c, qTitle).FontSize(12.5).FontWeight(500).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
									pctCol := p.Fg
									if pct < 15 {
										pctCol = p.Minus
									}
									ui.Text(c, locales.QuotaRemaining(pct)).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(pctCol).SingleLine().Shrink(0)
								})
								barCol := p.Fg
								if pct < 15 {
									barCol = p.Minus
								} else if pct < 35 {
									barCol = p.Live
								}
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.Edge2).Clip().Children(func() {
									ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(barCol)
								})
								if reset := formatQuotaReset(a.now(), q); reset != "" {
									ui.Text(c, reset).FontSize(11.5).TextColor(p.Mute).SingleLine()
								}
							})
						}
						if hasCtx {
							used := a.C.LastInput
							pct := min(100, (used*100+window/2)/window)
							ui.Column(c).FillWidth().Gap(5).Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
									ui.Text(c, locales.UsageSession()).FontSize(12.5).FontWeight(500).TextColor(p.Fg).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
									ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(p.Fg2).SingleLine().Shrink(0)
								})
								barCol := p.Fg2
								if pct >= 85 {
									barCol = p.Minus
								}
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.Edge2).Clip().Children(func() {
									ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(barCol)
								})
								subLine := locales.ContextUsed(tokens(used), tokens(window))
								if cost := a.turnCost(a.C.Usage); cost != "" {
									subLine += " · " + cost
								}
								ui.Text(c, subLine).FontSize(11.5).TextColor(p.Mute).SingleLine().Ellipsis("…")
							})
						}
						if !hasQuota && !hasCtx {
							ui.Text(c, locales.UsageNoData()).FontSize(12.5).TextColor(p.Mute).SingleLine()
						}
					})
				})
			})
		case strings.HasPrefix(activeSub, "model:") && subModel.ID != "" && subModel.Reasoning:
			ui.Overlay(c, func() {
				subWrap := ui.Box(c.Key("model-sub-wrap")).AttachTo(subAnchor, ui.AnchorTopEnd, ui.AnchorTopStart).Padding(0, 0, 0, 14)
				subWrapHovered = subWrap.Hovered()
				subWrap.Children(func() {
					effCard := ui.Column(c.Key("effort-card")).Width(180).Gap(1).Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(6)
					lift(effCard, p)
					rim(effCard, p)
					if !a.reduceMotion() {
						effCard.Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: 120 * time.Millisecond})
					}
					effCard.Children(func() {
						ui.Text(c, locales.ReasoningEffort()).FontSize(11.5).FontWeight(500).LetterSpacing(.02).
							TextColor(p.Mute).Padding(4, 10, 4, 10).SingleLine()
						hoveringOther := subModel.ID != activeModel.ID && subModel.ID != current
						for _, ef := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
							on := !hoveringOther && a.C.Current.Effort == ef
							eb := ui.ButtonBase(c.Key("eff:"+ef)).Row().FillWidth().Height(30).Padding(0, 10).Gap(8).Radius(8).
								AlignItems(ui.Center).Label(effortLabel(ef)).Checked(on)
							focusRing(eb, p)
							if on {
								eb.Background(p.Sel)
							} else if eb.Hovered() {
								eb.Background(p.Hover)
							}
							weight := 400
							if on {
								weight = 500
							}
							eb.Children(func() {
								ui.Text(c, effortLabel(ef)).FontSize(13.5).FontWeight(weight).TextColor(p.Fg).SingleLine().Grow(1)
								if on {
									sym(c, "check", 14, p.Fg)
								}
							})
							if eb.Clicked() {
								e := ef
								if hoveringOther && subModel.ID != "" {
									id := subModel.ID
									a.do(a.C.Update(proto.SessionUpdate{Model: &id, Effort: &e}))
									a.recordRecentModel(account, id)
								} else {
									a.do(a.C.Update(proto.SessionUpdate{Effort: &e}))
								}
								a.menu.modelMenu = false
								a.menu.hoverSub = ""
							}
						}
					})
				})
			})
		}

		switch {
		case nextHover == "none":
			a.menu.hoverSub = ""
		case nextHover != "":
			a.menu.hoverSub = nextHover
		case !subWrapHovered:
			a.menu.hoverSub = ""
		}
	})
	pop.AttachTo(b, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 8, 0).
		Width(296).Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(6).NoShadow()
	lift(pop, p)
	rim(pop, p)
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
	var b ui.Element
	if a.C.Current != nil && a.C.Current.Mode == "full" {
		b = ui.ButtonBase(c.Key("pill:mode")).Row().Height(28).Padding(0, 9).Gap(5).Radius(14).AlignItems(ui.Center).Label(label)
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
	}, a.menuOptions(&a.menu.modeMenu))
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
