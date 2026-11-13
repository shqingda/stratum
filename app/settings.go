package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

var modeCards = []struct{ id, symbol, text string }{
	{"ask", "eye", "只读自动；改文件、跑命令都先问你。"},
	{"auto", "shield-half", "工作区内改文件、沙箱里跑命令不再问。"},
	{"full", "shield-off", "都不问。只在你信任的仓库用。"},
}

func (a *App) openSettings() {
	a.settingsOpen, a.addingAccount, a.settingsPage = true, false, ""
	a.quiet(a.C.ReadSettings())
}

// openMCP opens the settings at the MCP servers.
func (a *App) openMCP() {
	a.settingsOpen, a.settingsPage = true, "mcp"
	a.quiet(a.C.ReadMCP())
}

// settingsView is the settings window: accounts, and what new sessions
// start with.
func (a *App) settingsView(c *ui.Context) {
	p := a.p
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(220).Shrink(0).Padding(0, 8).Background(p.Side).BorderWidth(0, .5, 0, 0).BorderColor(p.Edge).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Spacer(c)
				if a.iconButton(c, "settings-close", "x", "关闭设置").Clicked() {
					a.settingsOpen = false
				}
			})
			for _, it := range []struct{ page, key, symbol, label string }{
				{"", "settings-accounts", "user-circle", "账号与模型"},
				{"mcp", "settings-mcp", "puzzle", "技能与 MCP"},
			} {
				b := a.navItem(c, it.key, it.symbol, it.label, "")
				if it.page == a.settingsPage {
					b.Background(p.Sel)
				}
				if b.Clicked() && it.page != a.settingsPage {
					if it.page == "mcp" {
						a.openMCP()
					} else {
						a.openSettings()
					}
				}
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Background(p.Bg).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).DragWindow()
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Width(600).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(8, 20, 40, 20).Children(func() {
					if a.settingsPage == "mcp" {
						a.mcpPage(c)
					} else {
						a.accountsPage(c)
					}
				})
			})
		})
	})
}

func (a *App) accountsPage(c *ui.Context) {
	p := a.p
	ui.Text(c, "账号与模型").FontSize(20).FontWeight(650).LetterSpacing(-.3).TextColor(p.Fg).Margin(0, 0, 4, 0)
	ui.Text(c, "凭据存在钥匙串里。订阅账号按额度计，不显示费用。").FontSize(13).TextColor(p.Fg2).Margin(0, 0, 22, 0)

	heading := func(label string, right func()) {
		ui.Row(c).Margin(24, 0, 8, 0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(p.Fg)
			ui.Spacer(c)
			right()
		})
	}
	heading("账号", func() {
		if a.barButton(c, "add-account", "plus", "添加账号", func() {}).Clicked() {
			a.addingAccount, a.accountsSeen = !a.addingAccount, len(a.C.Accounts)
		}
	})
	if len(a.C.Accounts) != a.accountsSeen {
		a.addingAccount = false // the sign-in went through
	}
	if a.addingAccount {
		ui.Column(c).Margin(0, 0, 10, 0).Children(func() { a.loginList(c) })
	}
	group(c, p, func() {
		for i, acc := range a.C.Accounts {
			if i > 0 {
				ui.Box(c).Height(.5).Background(p.Edge)
			}
			a.accountRow(c, acc)
		}
	})

	set := proto.Settings{}
	if a.C.Settings != nil {
		set = *a.C.Settings
	}
	heading("模型分工", func() {})
	group(c, p, func() {
		ui.Row(c).MinHeight(52).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, "主 agent").FontSize(13).FontWeight(500).TextColor(p.Fg)
				ui.Text(c, "你直接对话的那个，新会话默认用它").FontSize(12).TextColor(p.Mute)
			})
			a.defaultModel(c, set)
			a.picker(c, "set-effort", "", efforts[set.Effort]).PopoverMenuButton(func(m *ui.Menu) {
				for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
					if m.Item(efforts[effort]).ID("set-effort:" + effort).Checked(set.Effort == effort).Chosen() {
						s := set
						s.Effort = effort
						a.do(a.C.SaveSettings(s))
					}
				}
			}, a.menuOptions(nil))
		})
		ui.Box(c).Height(.5).Background(p.Edge)
		a.roleRow(c, "sub", "子 agent", "探索代码、并行的小任务", set.Sub, &a.subMenu, func(r *proto.ModelRef) { s := set; s.Sub = r; a.do(a.C.SaveSettings(s)) })
		ui.Box(c).Height(.5).Background(p.Edge)
		a.roleRow(c, "light", "标题与压缩", "起会话名、上下文满时写摘要", set.Light, &a.lightMenu, func(r *proto.ModelRef) { s := set; s.Light = r; a.do(a.C.SaveSettings(s)) })
		ui.Box(c).Height(.5).Background(p.Edge)
		a.fallbackRow(c, set)
	})

	heading("新会话默认权限", func() {})
	mode := set.Mode
	if mode == "" {
		mode = "ask"
	}
	ui.Row(c).Gap(8).AlignItems(ui.Stretch).Children(func() {
		for _, m := range modeCards {
			on := m.id == mode
			card := ui.ButtonBase(c.Key("set-mode:" + m.id)).Column().Grow(1).Basis(0).AlignItems(ui.Start).Padding(12).Gap(4).Radius(10).Background(p.Card).Label(modes[m.id])
			if on {
				card.Border(1.5, p.Fg)
			} else {
				card.Border(.5, p.Edge2)
			}
			if card.Clicked() && !on {
				s := set
				s.Mode = m.id
				a.do(a.C.SaveSettings(s))
			}
			card.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					sym(c, m.symbol, 15, p.Fg)
					ui.Text(c, modes[m.id]).FontSize(13).FontWeight(600).TextColor(p.Fg)
				})
				ui.Text(c, m.text).FontSize(12).LineHeight(1.45).TextColor(p.Fg2)
			})
		}
	})
}

func group(c *ui.Context, p *Palette, children func()) {
	ui.Column(c).Radius(10).Background(p.Card).Border(.5, p.Edge2).Clip().Children(children)
}

func (a *App) accountRow(c *ui.Context, acc proto.Account) {
	p := a.p
	name := acc.Name
	if name == "" {
		name = acc.ID
	}
	sub := acc.Email
	if sub == "" {
		sub = "API key"
		if acc.Subscription {
			sub = "订阅登录"
		}
	}
	ui.Row(c).MinHeight(52).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(30, 30).Radius(8).Background(p.Chip).Center().Shrink(0).Children(func() {
			sym(c, providerSymbol(acc.Provider), 17, p.Fg)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
				if acc.Subscription {
					ui.Text(c, "订阅").FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
				}
			})
			ui.Text(c, sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
		})
		a.iconButton(c, "account:"+acc.ID, "dots", name+" 的操作").PopoverMenuButton(func(m *ui.Menu) {
			if m.Item("退出登录").ID("logout:" + acc.ID).Danger().Chosen() {
				a.do(a.C.Logout(acc.ID))
			}
		}, a.menuOptions(nil))
	})
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
				a.do(a.C.SaveSettings(proto.Settings{Account: acc.ID, Effort: set.Effort, Mode: set.Mode}))
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

// picker is a settings value that opens a menu: a muted prefix, the value
// and a selector mark.
func (a *App) picker(c *ui.Context, key, prefix, value string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 8, 0, 10).Gap(6).Radius(7).AlignItems(ui.Center).
		Background(p.Card).Border(.5, p.Edge2).Shrink(0).Label(prefix + " " + value)
	if b.Hovered() {
		b.Background(p.Card.Mix(p.Fg, .04))
	}
	b.Children(func() {
		if prefix != "" {
			ui.Text(c, prefix).FontSize(12.5).TextColor(p.Mute).SingleLine()
		}
		ui.Text(c, value).FontSize(12.5).TextColor(p.Fg).SingleLine()
		sym(c, "selector", 13, p.Mute)
	})
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

// roleRow picks the account and model of a role, or none to leave it to
// the session's own.
func (a *App) roleRow(c *ui.Context, key, title, sub string, ref *proto.ModelRef, open *bool, save func(*proto.ModelRef)) {
	p := a.p
	ui.Row(c.Key("role:"+key)).MinHeight(52).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.Fg)
			ui.Text(c, sub).FontSize(12).TextColor(p.Mute)
		})
		name, model := "", "跟主 agent 一样"
		if ref != nil {
			name, model = a.accountName(ref.Account), ref.Model
		}
		a.picker(c, "role-model:"+key, name, model).PopoverMenuButton(func(m *ui.Menu) {
			if m.Item("跟主 agent 一样").ID("role-same:" + key).Checked(ref == nil).Chosen() {
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
		if !*open && !a.setModelMenu && !a.subMenu && !a.lightMenu {
			a.modelsRequested = false
		}
		if ref != nil {
			a.picker(c, "role-effort:"+key, "", efforts[ref.Effort]).PopoverMenuButton(func(m *ui.Menu) {
				for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
					if m.Item(efforts[effort]).ID("role-effort:" + key + ":" + effort).Checked(ref.Effort == effort).Chosen() {
						r := *ref
						r.Effort = effort
						save(&r)
					}
				}
			}, a.menuOptions(nil))
		}
	})
}

func (a *App) accountName(id string) string {
	for _, acc := range a.C.Accounts {
		if acc.ID == id && acc.Name != "" {
			return acc.Name
		}
	}
	return id
}

// fallbackRow turns moving to backup accounts on and off: the others, in
// the order they are listed.
func (a *App) fallbackRow(c *ui.Context, set proto.Settings) {
	p := a.p
	main := set.Account
	if main == "" && len(a.C.Accounts) > 0 {
		main = a.C.Accounts[0].ID
	}
	var names []string
	for _, acc := range a.C.Accounts {
		if acc.ID != main {
			names = append(names, a.accountName(acc.ID))
		}
	}
	sub := "只有一个账号，添加别的账号后才能换"
	switch len(names) {
	case 0:
	case 1:
		sub = "换到 " + names[0] + "；换过会在对话里提示"
	default:
		sub = "先 " + names[0] + "，再 " + strings.Join(names[1:], "、") + "；换过会在对话里提示"
	}
	on := set.Fallback
	ui.Row(c).MinHeight(52).Padding(8, 14, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, "额度用尽时换备用账号").FontSize(13).FontWeight(500).TextColor(p.Fg)
			ui.Text(c, sub).FontSize(12).TextColor(p.Mute)
		})
		ui.Switch(c, &on).Label("换备用账号").Disabled(len(names) == 0)
		if on != set.Fallback {
			s := set
			s.Fallback = on
			a.do(a.C.SaveSettings(s))
		}
	})
}
