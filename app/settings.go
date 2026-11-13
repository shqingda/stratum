package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

var modeCards = []struct{ id, symbol, text string }{
	{"ask", "eye", "只读自动；改文件、跑命令都先问你。"},
	{"auto", "shield-half", "工作区内改文件、沙箱里跑命令不再问。"},
	{"full", "shield-off", "都不问。只在你信任的仓库用。"},
}

func (a *App) openSettings() {
	a.settingsOpen, a.addingAccount = true, false
	a.quiet(a.C.ReadSettings())
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
			a.navItem(c, "settings-accounts", "user-circle", "账号与模型", "").Background(p.Sel)
		})
		ui.Column(c).Grow(1).MinWidth(0).Background(p.Bg).Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).DragWindow()
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Width(600).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(8, 20, 40, 20).Children(func() {
					a.accountsPage(c)
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
	heading("新会话默认模型", func() {})
	group(c, p, func() {
		ui.Row(c).MinHeight(52).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, "主 agent").FontSize(13).FontWeight(500).TextColor(p.Fg)
				ui.Text(c, "你直接对话的那个").FontSize(12).TextColor(p.Mute)
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
	if a.modelsAccount != account {
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
	if !a.setModelMenu {
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
	}
	return "key"
}
