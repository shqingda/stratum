package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// settingsPages are the pages of the settings, in the order of the list,
// with the words their search finds them by.
var settingsPages = []struct {
	id, symbol   string
	label, words func(locales.Messages) string
}{
	{"general", "adjustments-horizontal", locales.Messages.SettingsGeneral, locales.Messages.SettingsGeneralWords},
	{"accounts", "user-circle", locales.Messages.SettingsAccounts, locales.Messages.SettingsAccountsWords},
	{"models", "sparkles", locales.Messages.SettingsModels, locales.Messages.SettingsModelsWords},
	{"permissions", "shield-half", locales.Messages.SettingsPermissions, locales.Messages.SettingsPermissionsWords},
	{"shortcuts", "keyboard", locales.Messages.SettingsShortcuts, locales.Messages.SettingsShortcutsWords},
	{"mcp", "puzzle", locales.Messages.SettingsMcp, locales.Messages.SettingsMcpWords},
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
	switch id {
	case "mcp":
		a.openMCP()
	case a.settingsPage:
	default:
		a.settingsPage, a.addingAccount = id, false
	}
}

// settingsView is the settings: a list of pages, and the page.
func (a *App) settingsView(c *ui.Context) {
	p := a.p
	title := ""
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(200).Shrink(0).Padding(0, 10).Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Spacer(c)
				if a.iconButton(c, "settings-close", "x", locales.CloseSettings()).KeyShortcuts(0, ui.KeyEscape).Clicked() {
					a.settingsOpen = false
				}
			})
			ui.SearchField(c, &a.settingsQuery).Label(locales.SearchSettings()).Placeholder(locales.Search()).Margin(0, 0, 10, 0)
			q := strings.ToLower(strings.TrimSpace(a.settingsQuery))
			for _, pg := range settingsPages {
				label := pg.label(locales.In(i18n.Locale()))
				if pg.id == a.settingsPage {
					title = label
				}
				if q != "" && !finds(pg.label, pg.words, q) {
					continue
				}
				it := ui.ButtonBase(c.Key("settings-"+pg.id)).Row().AlignSelf(ui.Stretch).Justify(ui.Start).Height(28).Padding(0, 8).Gap(8).Radius(6).AlignItems(ui.Center).Label(label)
				switch {
				case pg.id == a.settingsPage:
					it.Background(p.Sel)
				case it.Hovered():
					it.Background(p.Hover)
				}
				if it.Clicked() {
					a.openSettingsPage(pg.id)
				}
				it.Children(func() {
					ui.Box(c).Width(16).Center().Children(func() { sym(c, pg.symbol, 16, p.Fg2) })
					ui.Text(c, label).FontSize(13).TextColor(p.Fg).SingleLine()
				})
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Background(p.Bg).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 22).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.Fg)
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Width(580).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(4, 20, 40, 20).Children(func() {
					switch a.settingsPage {
					case "accounts":
						a.accountsPage(c)
					case "models":
						a.modelsPage(c)
					case "permissions":
						a.permissionsPage(c)
					case "shortcuts":
						a.shortcutsPage(c)
					case "mcp":
						a.mcpPage(c)
					default:
						a.generalPage(c)
					}
				})
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
	top := float32(20)
	if first {
		top = 6
	}
	ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(p.Fg).Margin(top, 0, 7, 2)
}

// note is the small text under a group.
func note(c *ui.Context, p *Palette, text string) {
	ui.Text(c, text).FontSize(12).LineHeight(1.45).TextColor(p.Mute).Margin(6, 2, 0, 2)
}

func group(c *ui.Context, p *Palette, children func()) {
	ui.Column(c).Radius(10).Background(p.Card).Border(.5, p.Edge2).Clip().Children(children)
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
	ui.Row(c.Key(key)).MinHeight(40).Padding(7, 10, 7, 12).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, label).FontSize(13).TextColor(p.Fg)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(p.Mute)
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
		r.row("set-theme", locales.Appearance(), "", func() {
			themes := []string{"", "light", "dark"}
			i := max(slices.Index(themes, set.Theme), 0)
			if ui.Segmented(c, &i, locales.ThemeAuto(), locales.ThemeLight(), locales.ThemeDark()).Label(locales.Appearance()).Changed() {
				s := set
				s.Theme = themes[i]
				a.do(a.C.SaveSettings(s))
			}
		})
	})

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
	})
	note(c, p, locales.NotifyNote())
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
		r.row("set-model-row", locales.DefaultModel(), "", func() { a.defaultModel(c, set) })
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
		r.row("role:sub", locales.Subtasks(), "", func() {
			a.roleModel(c, "sub", set.Sub, &a.subMenu, func(ref *proto.ModelRef) { s := set; s.Sub = ref; a.do(a.C.SaveSettings(s)) })
		})
		r.row("role:light", locales.TitlesAndSummaries(), "", func() {
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

func modeRows() []struct{ id, label, sub string } {
	return []struct{ id, label, sub string }{
		{"ask", locales.ModeAsk(), locales.ModeAskSub()},
		{"auto", locales.ModeAuto(), locales.ModeAutoSub()},
		{"full", locales.ModeFullAccess(), locales.ModeFullSub()},
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
	ui.RadioGroup(c, func() {
		group(c, p, func() {
			r := rows{c: c, p: p}
			for _, m := range modeRows() {
				r.sep()
				chosen := mode
				it := ui.RadioBase(c.Key("set-mode:"+m.id), &chosen, m.id).Row().MinHeight(44).Padding(7, 12).Gap(10).AlignItems(ui.Center).Label(m.label)
				if chosen != mode {
					s := set
					s.Mode = chosen
					a.do(a.C.SaveSettings(s))
				}
				it.Children(func() {
					a.radioDot(c, m.id == mode)
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, m.label).FontSize(13).TextColor(p.Fg)
						ui.Text(c, m.sub).FontSize(12).TextColor(p.Mute)
					})
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
func shortcuts() []struct {
	group string
	keys  []struct{ label, key string }
} {
	type key = struct{ label, key string }
	return []struct {
		group string
		keys  []struct{ label, key string }
	}{
		{locales.Sessions(), []key{
			{locales.NewSession(), "⌘N"}, {locales.SessionsOneToNine(), "⌘1 – ⌘9"}, {locales.NextWaiting(), "⌘J"}, {locales.CommandPalette(), "⌘K"}, {locales.ToggleSidebar(), "⌘\\"}, {locales.Settings(), "⌘,"},
		}},
		{locales.Approvals(), []key{
			{locales.Allow(), "↩"}, {locales.AllowForSession(), "⌘↩"}, {locales.Deny(), "esc"},
		}},
		{locales.WhileWorking(), []key{
			{locales.Stop(), "esc"},
		}},
	}
}

func (a *App) shortcutsPage(c *ui.Context) {
	p := a.p
	ui.SearchField(c, &a.keysQuery).Label(locales.SearchShortcuts()).Placeholder(locales.Search()).Margin(6, 0, 4, 0)
	q := strings.ToLower(strings.TrimSpace(a.keysQuery))
	found := false
	for _, g := range shortcuts() {
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
	b := ui.ButtonBase(c.Key(key)).Row().Height(24).Padding(0, 6, 0, 9).Gap(6).Radius(6).AlignItems(ui.Center).
		Background(p.Card).Border(.5, p.Edge2).Shrink(0).Label(strings.TrimSpace(prefix + " " + value))
	if b.Hovered() {
		b.Background(p.Card.Mix(p.Fg, .04))
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
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 10).Radius(6).Center().Background(p.Card).Border(.5, p.Edge2).Label(label)
	if b.Hovered() {
		b.Background(p.Card.Mix(p.Fg, .04))
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
