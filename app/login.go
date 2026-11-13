package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

// signIns are the browser sign-ins the list offers, subscriptions first.
var signIns = []struct{ provider, method, symbol, title, sub string }{
	{"codex", "codex", "brand-openai", "Sign in with ChatGPT", "Plus、Pro、Business 订阅"},
	{"xai", "xai", "brand-x", "X 订阅", "SuperGrok、X Premium+"},
	{"openrouter", "openrouter", "route", "OpenRouter", "浏览器授权，换一把 key"},
}

// keyProviders are the providers that take an API key.
func keyProviders() []auth.Provider {
	var out []auth.Provider
	for _, p := range auth.Providers {
		if p.KeyURL != "" {
			out = append(out, p)
		}
	}
	return out
}

// needsLogin is a kernel with no account yet: the window is the sign-in.
func (a *App) needsLogin() bool {
	return a.C.Accounts != nil && len(a.C.Accounts) == 0
}

func (a *App) onboarding(c *ui.Context) {
	p := a.p
	ui.Column(c).Fill().Background(p.Bg).Children(func() {
		ui.Row(c).Height(barHeight).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).AlignItems(ui.Center).Padding(70, 20, 20, 20).Children(func() {
			ui.Image(c, mark).Size(58, 58).Margin(-5, 0, -5, 0)
			ui.Text(c, "连接一个模型").FontSize(22).FontWeight(650).LetterSpacing(-.33).TextColor(p.Fg).Margin(20, 0, 6, 0)
			ui.Text(c, "用你已有的订阅登录最省事。之后随时可以在设置里加别的。").FontSize(13).TextColor(p.Fg2).Margin(0, 0, 26, 0)
			ui.Column(c).Width(460).MaxWidthPercent(100).Children(func() { a.loginList(c) })
			ui.Row(c).Gap(5).AlignItems(ui.Center).Margin(18, 0, 0, 0).Children(func() {
				sym(c, "lock", 13, p.Mute)
				ui.Text(c, "凭据只存在本机钥匙串，Talos 没有服务器。").FontSize(11.5).TextColor(p.Mute)
			})
		})
	})
}

// loginList is the ways to sign in, with what the one under way needs.
func (a *App) loginList(c *ui.Context) {
	p := a.p
	s := a.C.SignIn
	ui.Column(c).Radius(10).Background(p.Card).Border(.5, p.Edge2).Clip().Children(func() {
		for i, o := range signIns {
			on := s != nil && s.Method == o.method
			if a.loginRow(c, i, "login:"+o.method, o.symbol, o.title, o.sub, on).Clicked() && !on {
				a.keyOpen = false
				a.do(a.C.Login(proto.Login{Provider: o.provider, Method: o.method}))
			}
			if on {
				a.loginWait(c, o.provider)
			}
		}
		if a.loginRow(c, len(signIns), "login:key", "key", "API key", "Anthropic、Gemini、DeepSeek、Kimi、智谱、通义、MiniMax 等", a.keyOpen).Clicked() {
			a.keyOpen = !a.keyOpen
		}
		if a.keyOpen {
			a.keyForm(c)
		}
	})
}

func (a *App) loginRow(c *ui.Context, i int, key, symbol, title, sub string, on bool) ui.Element {
	p := a.p
	if i > 0 {
		ui.Box(c).Height(.5).Background(p.Edge)
	}
	row := ui.ButtonBase(c.Key(key)).Row().MinHeight(50).AlignItems(ui.Stretch).Label(title)
	if row.Hovered() && !on {
		row.Background(p.Hover)
	}
	row.Children(func() {
		left := float32(14)
		if on && symbol != "key" {
			ui.Box(c).Width(2).Background(p.Live)
			left = 12
		}
		ui.Row(c).Grow(1).Padding(8, 12, 8, left).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(30, 30).Radius(8).Background(p.Chip).Center().Shrink(0).Children(func() {
				sym(c, symbol, 17, p.Fg)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
				ui.Text(c, sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
			})
			switch {
			case on && symbol != "key":
				slot(c, p, "run")
			case !on:
				sym(c, "chevron-right", 15, p.Mute)
			}
		})
	})
	return row
}

// loginWait says what the sign-in under way waits for.
func (a *App) loginWait(c *ui.Context, provider string) {
	p := a.p
	s := a.C.SignIn
	ui.Column(c).Padding(4, 14, 14, 56).Gap(8).Children(func() {
		switch {
		case s.Err != "":
			ui.Text(c, "登录失败："+s.Err).FontSize(12.5).TextColor(p.Minus).Selectable()
		case s.Code != "":
			ui.Text(c, "访问 "+strings.TrimPrefix(s.CodeURL, "https://")+" 输入：").FontSize(12.5).TextColor(p.Fg2).Selectable()
			ui.Row(c).Gap(6).Children(func() {
				for _, r := range s.Code {
					if r == '-' {
						ui.Text(c, "-").FontSize(16).Font("monospace").TextColor(p.Mute).Width(10).TextAlign(ui.Center).AlignSelf(ui.Center)
						continue
					}
					ui.Box(c).Size(28, 34).Radius(6).Background(p.Chip).Center().Children(func() {
						ui.Text(c, string(r)).FontSize(16).FontWeight(600).Font("monospace").TextColor(p.Fg)
					})
				}
			})
		case s.URL != "":
			ui.Text(c, "已在浏览器打开登录页，完成后自动回来。").FontSize(12.5).TextColor(p.Fg2)
		default:
			ui.Text(c, "正在打开浏览器…").FontSize(12.5).TextColor(p.Fg2)
		}
		ui.Row(c).Gap(8).Children(func() {
			if s.Code != "" && s.Err == "" {
				if a.barButton(c, "login-copy", "copy", "复制代码", func() {}).Clicked() {
					c.WriteClipboard(s.Code)
				}
			}
			if s.URL != "" && s.Code == "" && s.Err == "" && s.Method == "codex" {
				if a.button(c, "login-device", "浏览器打不开？用设备码", "", false).Clicked() {
					a.do(a.C.Login(proto.Login{Provider: provider, Method: s.Method, Device: true}))
				}
			}
			label := "取消"
			if s.Err != "" {
				label = "知道了"
			}
			if a.ghost(c, "login-cancel", label).Clicked() {
				a.do(a.C.CancelLogin())
			}
		})
	})
}

// keyForm takes an API key for a provider.
func (a *App) keyForm(c *ui.Context) {
	p := a.p
	providers := keyProviders()
	if a.keyProvider == "" {
		a.keyProvider = providers[0].ID
	}
	name := a.keyProvider
	for _, kp := range providers {
		if kp.ID == a.keyProvider {
			name = kp.Name
		}
	}
	s := a.C.SignIn
	ui.Column(c).Padding(4, 14, 14, 56).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.barButton(c, "key-provider", "selector", name, func() {}).Shrink(0).PopoverMenuButton(func(m *ui.Menu) {
				for _, kp := range providers {
					if m.Item(kp.Name).ID("key:" + kp.ID).Checked(kp.ID == a.keyProvider).Chosen() {
						a.keyProvider = kp.ID
					}
				}
			}, a.menuOptions(&a.keyMenu))
			ui.TextInputBase(c, &a.key).Label("API key").Password().Placeholder("粘贴 key").PlaceholderColor(p.Mute).
				FontSize(12.5).TextColor(p.Fg).SingleLine().Grow(1).MinWidth(0).Height(28).Radius(7).Background(p.Chip).Padding(0, 10)
		})
		if s != nil && s.Method == "" && s.Err != "" {
			ui.Text(c, "没存上："+s.Err).FontSize(12.5).TextColor(p.Minus).Selectable()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			b := a.button(c, "key-save", "保存", "", true)
			b.Disabled(strings.TrimSpace(a.key) == "" || s != nil && s.Method == "" && s.Err == "")
			if b.Clicked() {
				cmds, err := a.C.Login(proto.Login{Provider: a.keyProvider, Key: a.key})
				a.key = ""
				a.do(cmds, err)
			}
			ui.Text(c, "在 "+keyURL(providers, a.keyProvider)+" 创建").FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0).Selectable()
		})
	})
}

func keyURL(providers []auth.Provider, id string) string {
	for _, kp := range providers {
		if kp.ID == id {
			return strings.TrimPrefix(kp.KeyURL, "https://")
		}
	}
	return ""
}

func (a *App) ghost(c *ui.Context, key, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 10).Radius(7).AlignItems(ui.Center).Label(label)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
	})
	return b
}
