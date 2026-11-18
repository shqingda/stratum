package app

import (
	"fmt"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// settingsPages are the pages of the settings, in the order of the list,
// with their macOS squircle tint and search keywords.
var settingsPages = []struct {
	id, symbol         string
	badge              ui.Color
	label, words, lede func(locales.Messages) string
}{
	{"general", "adjustments-horizontal", ui.Hex("#636366"), locales.Messages.SettingsGeneral, locales.Messages.SettingsGeneralWords, locales.Messages.SettingsGeneralLede},
	{"accounts", "sparkles", ui.Hex("#007AFF"), locales.Messages.SettingsAccountsAndModels, locales.Messages.SettingsAccountsAndModelsWords, locales.Messages.SettingsAccountsAndModelsLede},
	{"permissions", "shield-half", ui.Hex("#34C759"), locales.Messages.SettingsPermissions, locales.Messages.SettingsPermissionsWords, locales.Messages.SettingsPermissionsLede},
	{"appearance", "palette", ui.Hex("#5856D6"), locales.Messages.Appearance, locales.Messages.SettingsAppearanceWords, locales.Messages.SettingsAppearanceLede},
	{"shortcuts", "keyboard", ui.Hex("#FF9500"), locales.Messages.SettingsShortcuts, locales.Messages.SettingsShortcutsWords, locales.Messages.SettingsShortcutsLede},
	{"mcp", "puzzle", ui.Hex("#32ADE6"), locales.Messages.SettingsMcp, locales.Messages.SettingsMcpWords, locales.Messages.SettingsMcpLede},
}

// finds says whether a settings page answers a search, in any language
// the app speaks: people search with the words they think in.
func finds(label, words func(locales.Messages) string, q string) bool {
	for _, tag := range []string{"zh-Hans", "en"} {
		m := locales.In(tag)
		if strings.Contains(strings.ToLower(label(m)+" "+words(m)), q) {
			return true
		}
	}
	return false
}

func (a *App) openSettings() {
	a.settingsOpen, a.addingAccount, a.settingsPage = true, false, "general"
	a.quiet(a.C.ReadSettings())
}

// openMCP opens the settings at the MCP servers.
func (a *App) openMCP() {
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.quiet(a.C.ReadMCP())
}

func (a *App) openSettingsPage(id string) {
	if id == "models" {
		id = "accounts"
	}
	switch id {
	case "mcp":
		a.openMCP()
	case a.settingsPage:
	default:
		a.settingsPage, a.addingAccount = id, false
	}
}

// settingsView shares the main window's Liquid Glass sidebar and calm
// paper stage so Settings feels like part of the same macOS app.
func (a *App) settingsView(c *ui.Context, vibrant bool) {
	p := a.p
	curPage := a.settingsPage
	if curPage == "models" || curPage == "" {
		curPage = "accounts"
		if a.settingsPage == "" {
			curPage = "general"
		}
	}
	title, lede := "", ""
	stageBg := p.Sheet.Mix(p.Bg, .45)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0)
		topGap := float32(barHeight)
		if vibrant {
			tint := ui.RGBA(255, 255, 255, .18)
			if p == dark {
				tint = ui.RGBA(22, 24, 32, .24)
			}
			side.Width(sidebarWidth-12).Margin(8, 4, 8, 8).Radius(16).
				Background(tint).Border(.5, p.Edge).Clip()
			rim(side, p)
			topGap = barHeight - 8
		} else {
			side.Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge)
		}
		side.Children(func() {
			ui.Box(c).Height(topGap).Shrink(0).DragWindow()
			ui.Row(c).Padding(4, 20, 14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Image(c, mark).Size(24, 24)
				ui.Text(c, locales.Settings()).FontSize(16).FontWeight(600).TextColor(p.Fg)
			})
			ui.SearchField(c, &a.settingsQuery).Label(locales.SearchSettings()).Placeholder(locales.Search()).Margin(0, 12, 12, 12)
			ui.Column(c).Grow(1).Padding(0, 12, 8, 12).Gap(2).Children(func() {
				q := strings.ToLower(strings.TrimSpace(a.settingsQuery))
				for _, pg := range settingsPages {
					label := pg.label(locales.In(i18n.Locale()))
					if pg.id == curPage {
						title = label
						if pg.lede != nil {
							lede = pg.lede(locales.In(i18n.Locale()))
						}
					}
					if q != "" && !finds(pg.label, pg.words, q) {
						continue
					}
					active := pg.id == curPage
					it := ui.ButtonBase(c.Key("settings-"+pg.id)).Row().AlignSelf(ui.Stretch).Justify(ui.Start).
						Height(32).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(label)
					press(it)
					switch {
					case active:
						it.Background(p.Sel)
						rim(it, p)
					case it.Hovered():
						it.Background(p.Hover)
					}
					if it.Clicked() {
						a.openSettingsPage(pg.id)
					}
					iconColor, textColor, weight := p.Fg2, p.Fg2, 450
					if active {
						iconColor, textColor, weight = p.Fg, p.Fg, 600
					} else if it.Hovered() {
						textColor = p.Fg
					}
					it.Children(func() {
						ui.Box(c).Width(16).Center().Children(func() { sym(c, pg.symbol, 16, iconColor) })
						ui.Text(c, label).FontSize(13.5).FontWeight(weight).TextColor(textColor).SingleLine().Ellipsis("…").MinWidth(0)
					})
				}
			})
			a.settingsBuildFoot(c)
		})
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Background(stageBg).Clip().Children(func() {
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Width(596).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(barHeight+8, 28, 48, 28).Children(func() {
					if lede != "" {
						ui.Text(c, lede).FontSize(13.5).LineHeight(1.45).TextColor(p.Fg2).Margin(0, 0, 18, 4)
					}
					switch curPage {
					case "accounts":
						a.accountsAndModelsPage(c)
					case "permissions":
						a.permissionsPage(c)
					case "appearance":
						a.appearancePage(c)
					case "shortcuts":
						a.shortcutsPage(c)
					case "mcp":
						a.mcpPage(c)
					default:
						a.generalPage(c)
					}
				})
			})
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(barHeight+8).PassThrough().
				Material(glass.ScrollEdge{Background: stageBg})
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(barHeight).Padding(0, 24).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(16).FontWeight(650).LetterSpacing(-.2).TextColor(p.Fg).SingleLine()
				ui.Spacer(c)
				done := ui.ButtonBase(c.Key("settings-close")).Row().Shrink(0).Height(28).Padding(0, 12).Radius(8).AlignItems(ui.Center).
					Background(p.Chip).Label(locales.CloseSettings()).KeyShortcuts(0, ui.KeyEscape)
				rim(done, p)
				press(done)
				if done.Hovered() {
					done.Background(p.Sel)
				}
				done.Children(func() { ui.Text(c, locales.Done()).FontSize(13).FontWeight(550).TextColor(p.Fg).SingleLine() })
				if done.Clicked() {
					a.settingsOpen = false
				}
			})
		})
	})
}

// settings is what new sessions start with, as last read.
func (a *App) settings() proto.Settings {
	if a.C.Settings != nil {
		return *a.C.Settings
	}
	return proto.Settings{}
}

// section is the bold name over a group of rows; the first sits higher.
func section(c *ui.Context, p *Palette, label string, first bool) {
	top := float32(22)
	if first {
		top = 4
	}
	ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.Mute).Margin(top, 0, 6, 4)
}

// note is the small text under a group.
func note(c *ui.Context, p *Palette, text string) {
	ui.Text(c, text).FontSize(12).LineHeight(1.45).TextColor(p.Mute).Margin(8, 4, 0, 4)
}

func group(c *ui.Context, p *Palette, children func()) {
	g := ui.Column(c).Radius(12).Background(p.Card).Border(.5, p.Edge).Clip()
	raise(g, p)
	rim(g, p)
	g.Children(children)
}

// rows draws the rows of a group, with a hairline between two.
type rows struct {
	c *ui.Context
	p *Palette
	n int
}

func (r *rows) sep() {
	if r.n > 0 {
		ui.Box(r.c).Height(.5).Shrink(0).Margin(0, 0, 0, 12).Background(r.p.Edge)
	}
	r.n++
}

// row is a label, with a line under it if sub is set, and its control on
// the right.
func (r *rows) row(key, label, sub string, control func()) {
	r.sep()
	c, p := r.c, r.p
	ui.Row(c.Key(key)).MinHeight(42).Padding(8, 10, 8, 12).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13.5).TextColor(p.Fg)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).LineHeight(1.4).TextColor(p.Mute)
			}
		})
		control()
	})
}

// toggle is a row with a switch; set saves the new value.
func (r *rows) toggle(key, label string, on, disabled bool, set func(bool)) {
	r.row(key, label, "", func() {
		v := on
		ui.Switch(r.c, &v).Label(label).Disabled(disabled)
		if v != on {
			set(v)
		}
	})
}

// languages are the app's languages, each named in itself.
func languages() []struct{ tag, label string } {
	return []struct{ tag, label string }{{"", locales.LanguageSystem()}, {"zh-Hans", "简体中文"}, {"en", "English"}}
}

func (a *App) generalPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	group(c, p, func() {
		r := rows{c: c, p: p}
		r.row("set-lang", locales.Language(), "", func() {
			chosen := a.locale.Chosen()
			label := languages()[0].label
			for _, l := range languages() {
				if l.tag == chosen {
					label = l.label
				}
			}
			a.picker(c, "set-lang", "", label).PopoverMenuButton(func(m *ui.Menu) {
				for _, l := range languages() {
					if m.Item(l.label).ID("set-lang:" + l.tag).Checked(l.tag == chosen).Chosen() {
						a.locale.Set(l.tag)
					}
				}
			}, a.menuOptions(nil))
		})
	})

	section(c, p, locales.WhileWorking(), false)
	group(c, p, func() {
		r := rows{c: c, p: p}
		r.row("set-enter", locales.ReturnKey(), "", func() {
			i := map[bool]int{false: 0, true: 1}[set.EnterSteers]
			if ui.Segmented(c, &i, locales.QueueIt(), locales.SendRightAway()).Label(locales.ReturnKey()).Changed() {
				s := set
				s.EnterSteers = i == 1
				a.do(a.C.SaveSettings(s))
			}
		})
	})
	note(c, p, map[bool]string{false: locales.CmdReturnSteers(), true: locales.CmdReturnQueues()}[set.EnterSteers])

	section(c, p, locales.Notifications(), false)
	group(c, p, func() {
		r := rows{c: c, p: p}
		for _, n := range []struct{ kind, label string }{
			{"wait", locales.NotifyWaitSetting()}, {"done", locales.NotifyDoneSetting()}, {"fail", locales.NotifyFailSetting()}, {"badge", locales.DockBadge()},
		} {
			r.toggle("set-quiet:"+n.kind, n.label, !slices.Contains(set.Quiet, n.kind), false, func(on bool) {
				s := set
				s.Quiet = slices.DeleteFunc(slices.Clone(set.Quiet), func(k string) bool { return k == n.kind })
				if !on {
					s.Quiet = append(s.Quiet, n.kind)
				}
				a.do(a.C.SaveSettings(s))
			})
		}
		r.row("set-notify-when", locales.NotifyWhen(), "", func() {
			whens := []string{"", "always", "never"}
			i := max(slices.Index(whens, set.NotifyWhen), 0)
			if ui.Segmented(c, &i, locales.NotifyInBackground(), locales.NotifyAlways(), locales.NotifyNever()).Label(locales.NotifyWhen()).Changed() {
				s := set
				s.NotifyWhen = whens[i]
				a.do(a.C.SaveSettings(s))
			}
		})
	})
}

func (a *App) appearancePage(c *ui.Context) {
	p := a.p
	set := a.settings()
	pick := func(r *rows, key, label string, values []string, current string, names []string, save func(*proto.Settings, string)) {
		r.row(key, label, "", func() {
			i := max(slices.Index(values, current), 0)
			if ui.Segmented(c, &i, names...).Label(label).Changed() {
				s := set
				save(&s, values[i])
				a.do(a.C.SaveSettings(s))
			}
		})
	}
	group(c, p, func() {
		r := &rows{c: c, p: p}
		pick(r, "set-theme", locales.Appearance(), []string{"", "light", "dark"}, set.Theme,
			[]string{locales.ThemeAuto(), locales.ThemeLight(), locales.ThemeDark()}, func(s *proto.Settings, v string) { s.Theme = v })
		pick(r, "set-size", locales.TextSize(), []string{"small", "", "large"}, set.TextSize,
			[]string{locales.TextSmall(), locales.TextDefault(), locales.TextLarge()}, func(s *proto.Settings, v string) { s.TextSize = v })
	})
	section(c, p, locales.Conversation(), false)
	group(c, p, func() {
		pick(&rows{c: c, p: p}, "set-detail", locales.FinishedTurns(), []string{"", "thinking", "all"}, set.Detail,
			[]string{locales.DetailPlain(), locales.DetailThinking(), locales.DetailAll()}, func(s *proto.Settings, v string) { s.Detail = v })
	})
	note(c, p, locales.DetailNote())
}

// settingsBuildFoot shows the build version and update check in the sheet footer.
func (a *App) settingsBuildFoot(c *ui.Context) {
	p := a.p
	version, commit, built := a.version, "", ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; version == "" && v != "" && v != "(devel)" {
			version = v
		}
		for _, kv := range bi.Settings {
			switch kv.Key {
			case "vcs.revision":
				commit = kv.Value[:min(7, len(kv.Value))]
			case "vcs.time":
				built, _, _ = strings.Cut(kv.Value, "T")
			case "vcs.modified":
				if kv.Value == "true" && commit != "" {
					commit += "+"
				}
			}
		}
	}
	if version == "" {
		version = "devel"
	}
	tip := strings.TrimSpace(commit + " " + built)
	ui.Row(c).Height(42).Shrink(0).Padding(0, 14).Gap(6).AlignItems(ui.Center).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
		v := ui.Text(c, "Talos "+version).FontSize(11.5).TextColor(p.Mute).SingleLine()
		if tip != "" {
			v.Tooltip(tip)
		}
		ui.Spacer(c)
		if a.checkUpdates != nil {
			b := ui.ButtonBase(c.Key("check-updates")).Padding(2, 6).Radius(5).Center().Label(locales.CheckForUpdates())
			if b.Hovered() {
				b.Background(p.Hover)
			}
			b.Children(func() { ui.Text(c, locales.CheckForUpdates()).FontSize(11.5).TextColor(p.Live).SingleLine() })
			if b.Clicked() {
				a.checkUpdates()
			}
		}
	})
}

func (a *App) accountsAndModelsPage(c *ui.Context) {
	section(c, a.p, locales.SettingsAccounts(), true)
	a.accountsPage(c)
	ui.Box(c).Height(10).Shrink(0)
	a.modelsPage(c)
}

func (a *App) accountsPage(c *ui.Context) {
	p := a.p
	if len(a.C.Accounts) != a.accountsSeen {
		a.addingAccount = false // the sign-in went through
	}
	group(c, p, func() {
		r := rows{c: c, p: p}
		for _, acc := range a.C.Accounts {
			r.sep()
			a.accountRow(c, acc)
		}
	})
	ui.Row(c).Margin(8, 0, 0, 0).Children(func() {
		ui.Spacer(c)
		if a.pushButton(c, "add-account", locales.AddAccount()).Clicked() {
			a.addingAccount, a.accountsSeen = !a.addingAccount, len(a.C.Accounts)
		}
	})
	if a.addingAccount {
		ui.Column(c).Margin(12, 0, 0, 0).Children(func() { a.loginList(c) })
	}
}

func (a *App) accountRow(c *ui.Context, acc proto.Account) {
	p := a.p
	name := acc.Name
	if name == "" {
		name = acc.ID
	}
	sub := acc.Email
	if sub == "" {
		sub = locales.ApiKey()
		if acc.Subscription {
			sub = locales.Subscription()
		}
	}
	ui.Row(c.Key("account-row:"+acc.ID)).MinHeight(44).Padding(7, 8, 7, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(26, 26).Radius(6).Background(p.Chip).Center().Shrink(0).Children(func() {
			sym(c, providerSymbol(acc.Provider), 15, p.Fg)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, name).FontSize(13).TextColor(p.Fg).SingleLine()
			ui.Text(c, sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
		})
		a.iconButton(c, "account:"+acc.ID, "dots", locales.AccountOptions(name)).PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locales.SignOut()).ID("logout:" + acc.ID).Danger().Chosen() {
				a.do(a.C.Logout(acc.ID))
			}
		}, a.menuOptions(nil))
	})
}

func (a *App) modelsPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	section(c, p, locales.NewSessions(), true)
	group(c, p, func() {
		r := rows{c: c, p: p}
		r.row("set-model-row", locales.DefaultModel(), locales.DefaultModelNote(), func() { a.defaultModel(c, set) })
		r.row("set-effort-row", locales.ReasoningEffort(), "", func() {
			a.picker(c, "set-effort", "", effortLabel(set.Effort)).PopoverMenuButton(func(m *ui.Menu) {
				for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
					if m.Item(effortLabel(effort)).ID("set-effort:" + effort).Checked(set.Effort == effort).Chosen() {
						s := set
						s.Effort = effort
						a.do(a.C.SaveSettings(s))
					}
				}
			}, a.menuOptions(nil))
		})
	})

	section(c, p, locales.BackgroundTasks(), false)
	group(c, p, func() {
		r := rows{c: c, p: p}
		r.row("role:sub", locales.Subtasks(), locales.SubtasksNote(), func() {
			a.roleModel(c, "sub", set.Sub, &a.subMenu, func(ref *proto.ModelRef) { s := set; s.Sub = ref; a.do(a.C.SaveSettings(s)) })
		})
		r.row("role:light", locales.TitlesAndSummaries(), locales.LightNote(), func() {
			a.roleModel(c, "light", set.Light, &a.lightMenu, func(ref *proto.ModelRef) { s := set; s.Light = ref; a.do(a.C.SaveSettings(s)) })
		})
	})

	section(c, p, locales.WhenQuotaRunsOut(), false)
	var names []string
	for _, acc := range a.C.Accounts {
		if acc.ID != set.Account {
			names = append(names, a.accountName(acc.ID))
		}
	}
	group(c, p, func() {
		r := rows{c: c, p: p}
		r.toggle("set-fallback", locales.SwitchAccount(), set.Fallback && len(names) > 0, len(names) == 0, func(on bool) {
			s := set
			s.Fallback = on
			a.do(a.C.SaveSettings(s))
		})
	})
	switch len(names) {
	case 0:
		note(c, p, locales.FallbackNeedsAccount())
	case 1:
		note(c, p, locales.FallbackTo(names[0]))
	default:
		note(c, p, locales.FallbackOrder(strings.Join(names, locales.ListSeparator())))
	}
}

// defaultModel picks the account and model new sessions start with.
func (a *App) defaultModel(c *ui.Context, set proto.Settings) {
	account, name, model := set.Account, "", set.Model
	for _, acc := range a.C.Accounts {
		if account == "" && name == "" || acc.ID == account {
			account, name = acc.ID, acc.Name
			if model == "" {
				model = acc.Model
			}
		}
	}
	if a.modelsAccount != account && a.setModelMenu {
		a.modelsRequested = false
	}
	a.picker(c, "set-model", name, model).PopoverMenuButton(func(m *ui.Menu) {
		for _, acc := range a.C.Accounts {
			if m.Item(acc.Name).ID("set-account:" + acc.ID).Checked(acc.ID == account).Chosen() {
				s := set
				s.Account, s.Model = acc.ID, ""
				a.do(a.C.SaveSettings(s))
			}
		}
		m.Separator()
		a.modelItems(m, account, set.Model, func(id string) {
			s := set
			s.Account, s.Model = account, id
			a.do(a.C.SaveSettings(s))
		})
	}, a.menuOptions(&a.setModelMenu))
	if !a.setModelMenu && !a.subMenu && !a.lightMenu {
		a.modelsRequested = false
	}
}

// roleModel picks the account and model of background work, or none for
// the default model.
func (a *App) roleModel(c *ui.Context, key string, ref *proto.ModelRef, open *bool, save func(*proto.ModelRef)) {
	name, model := "", locales.SameAsDefault()
	if ref != nil {
		name, model = a.accountName(ref.Account), ref.Model
		if ref.Effort != "" {
			model += " · " + effortLabel(ref.Effort)
		}
	}
	a.picker(c, "role-model:"+key, name, model).PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(locales.SameAsDefault()).ID("role-same:" + key).Checked(ref == nil).Chosen() {
			save(nil)
		}
		m.Separator()
		for _, acc := range a.C.Accounts {
			if m.Item(acc.Name).ID("role-account:" + key + ":" + acc.ID).Checked(ref != nil && ref.Account == acc.ID).Chosen() {
				save(&proto.ModelRef{Account: acc.ID, Model: acc.Model})
			}
		}
		if ref != nil {
			m.Separator()
			if a.modelsAccount != ref.Account {
				a.modelsRequested = false
			}
			a.modelItems(m, ref.Account, ref.Model, func(id string) {
				r := *ref
				r.Model = id
				save(&r)
			})
			m.Separator()
			for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
				if m.Item(effortLabel(effort)).ID("role-effort:" + key + ":" + effort).Checked(ref.Effort == effort).Chosen() {
					r := *ref
					r.Effort = effort
					save(&r)
				}
			}
		}
	}, a.menuOptions(open))
}

func (a *App) accountName(id string) string {
	for _, acc := range a.C.Accounts {
		if acc.ID == id && acc.Name != "" {
			return acc.Name
		}
	}
	return id
}

func modeRows() []struct{ id, icon, label, sub string } {
	return []struct{ id, icon, label, sub string }{
		{"ask", "eye", locales.ModeAsk(), locales.ModeAskSub()},
		{"auto", "shield-half", locales.ModeAuto(), locales.ModeAutoSub()},
		{"full", "shield-off", locales.ModeFullAccess(), locales.ModeFullSub()},
	}
}

func (a *App) permissionsPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	mode := set.Mode
	if mode == "" {
		mode = "ask"
	}
	section(c, p, locales.NewSessions(), true)
	// Three cards side by side, as the mockup has them; the chosen one
	// has a darker edge.
	ui.RadioGroup(c, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Stretch).Children(func() {
			for _, m := range modeRows() {
				chosen := mode
				it := ui.RadioBase(c.Key("set-mode:"+m.id), &chosen, m.id).Column().Grow(1).Basis(0).MinWidth(0).Padding(12).Gap(4).Radius(10).AlignItems(ui.Start).Justify(ui.Start).
					Label(m.label)
				if m.id == mode {
					it.Background(p.LiveBg).Border(1, p.Live)
				} else {
					it.Background(p.Card).Border(.5, p.Edge2)
				}
				if chosen != mode {
					s := set
					s.Mode = chosen
					a.do(a.C.SaveSettings(s))
				}
				it.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						sym(c, m.icon, 15, p.Fg)
						ui.Text(c, m.label).FontSize(13).FontWeight(600).TextColor(p.Fg)
					})
					ui.Text(c, m.sub).FontSize(12).LineHeight(1.45).TextColor(p.Fg2)
				})
			}
		})
	}).Label(locales.NewSessionPermissions())
	section(c, p, locales.AlwaysAllowed(), false)
	group(c, p, func() {
		r := rows{c: c, p: p}
		if len(set.Rules) == 0 {
			ui.Text(c, locales.NoRules()).FontSize(12.5).TextColor(p.Mute).Padding(11, 12)
		}
		for i, rule := range set.Rules {
			what := ruleText(rule)
			r.row(fmt.Sprintf("rule:%d:%s:%s", i, rule.Tool, rule.Prefix), what, "", func() {
				if a.iconButton(c, fmt.Sprintf("rule-x:%d", i), "x", locales.RemoveRule(what)).Clicked() {
					s := set
					s.Rules = slices.Delete(slices.Clone(set.Rules), i, i+1)
					a.do(a.C.SaveSettings(s))
				}
			})
		}
	})
}

func (a *App) radioDot(c *ui.Context, on bool) {
	p := a.p
	dot := ui.Box(c).Size(16, 16).Radius(8).Shrink(0).Center()
	if !on {
		dot.Background(p.Card).Border(.5, p.Edge2)
		return
	}
	dot.Background(p.Acc).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(p.OnAcc)
	})
}

// shortcuts are the app's keys, as keys.go binds them.
func shortcuts(steers bool) []struct {
	group string
	keys  []struct{ label, key string }
} {
	type key = struct{ label, key string }
	now, queue := "⌘↩", "↩"
	if steers {
		now, queue = queue, now
	}
	return []struct {
		group string
		keys  []struct{ label, key string }
	}{
		{locales.Sessions(), []key{
			{locales.NewSession(), "⌘N"}, {locales.FindInSession(), "⌘F"}, {locales.SessionsOneToNine(), "⌘1 – ⌘9"}, {locales.NextWaiting(), "⌘J"}, {locales.CommandPalette(), "⌘K"}, {locales.ToggleSidebar(), "⌘B / ⌘\\"}, {locales.Settings(), "⌘,"},
		}},
		{locales.Approvals(), []key{
			{locales.Allow(), "↩"}, {locales.AllowForSession(), "⌘↩"}, {locales.Deny(), "esc"},
		}},
		{locales.WhileWorking(), []key{
			{locales.Stop(), "esc"}, {locales.SendRightAway(), now}, {locales.QueueIt(), queue},
		}},
	}
}

func (a *App) shortcutsPage(c *ui.Context) {
	p := a.p
	ui.SearchField(c, &a.keysQuery).Label(locales.SearchShortcuts()).Placeholder(locales.Search()).Margin(6, 0, 4, 0)
	q := strings.ToLower(strings.TrimSpace(a.keysQuery))
	found := false
	for _, g := range shortcuts(a.settings().EnterSteers) {
		var keys []struct{ label, key string }
		for _, k := range g.keys {
			if q == "" || strings.Contains(strings.ToLower(k.label+" "+k.key), q) {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		found = true
		section(c, p, g.group, false)
		group(c, p, func() {
			r := rows{c: c, p: p}
			for _, k := range keys {
				r.row("key:"+k.label, k.label, "", func() {
					ui.Text(c, k.key).FontSize(12.5).TextColor(p.Fg2)
				})
			}
		})
	}
	if !found {
		note(c, p, locales.NoShortcutMatches())
	}
}

// picker is a value that opens a menu: a muted prefix, the value and a
// selector mark.
func (a *App) picker(c *ui.Context, key, prefix, value string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 8, 0, 10).Gap(6).Radius(7).AlignItems(ui.Center).
		Background(p.Sheet).Border(.5, p.Edge2).Shrink(0).Label(strings.TrimSpace(prefix + " " + value))
	press(b)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		if prefix != "" {
			ui.Text(c, prefix).FontSize(13).TextColor(p.Mute).SingleLine()
		}
		ui.Text(c, value).FontSize(13).TextColor(p.Fg).SingleLine()
		sym(c, "selector", 12, p.Fg2)
	})
	return b
}

// pushButton is a plain push button, as the settings draw them.
func (a *App) pushButton(c *ui.Context, key, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Height(28).Padding(0, 11).Radius(7).Center().Background(p.Sheet).Border(.5, p.Edge2).Label(label)
	press(b)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).TextColor(p.Fg) })
	return b
}

func providerSymbol(provider string) string {
	switch provider {
	case "codex", "openai":
		return "brand-openai"
	case "xai":
		return "brand-x"
	case "openrouter":
		return "route"
	case "copilot":
		return "brand-github"
	}
	return "key"
}
