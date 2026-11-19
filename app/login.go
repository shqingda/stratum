package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// signIns are the browser sign-ins the list offers, subscriptions first.
func signIns() []struct{ provider, method, symbol, title, sub string } {
	return []struct{ provider, method, symbol, title, sub string }{
		{"codex", "codex", "brand-openai", "Sign in with ChatGPT", locales.SignInChatGPTSub()},
		{"xai", "xai", "brand-x", locales.SignInXTitle(), "SuperGrok、X Premium+"},
		{"copilot", "copilot", "brand-github", "GitHub Copilot", locales.SignInCopilotSub()},
		{"kimi-coding", "kimi", "sparkles", "Kimi Code", locales.SignInKimiSub()},
		{"openrouter", "openrouter", "route", "OpenRouter", locales.SignInOpenRouterSub()},
	}
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
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).AlignItems(ui.Center).Padding(24, 20, 24, 20).Children(func() {
				brandMark(c, 52, p.Fg)
				ui.Text(c, locales.OnboardingTitle()).FontSize(24).FontWeight(650).LetterSpacing(-.33).TextColor(p.Fg).Margin(20, 0, 6, 0)
				ui.Text(c, locales.OnboardingLead()).FontSize(14).TextColor(p.Fg2).Margin(0, 0, 26, 0)
				ui.Column(c).Width(460).MaxWidthPercent(100).Children(func() { a.loginList(c) })
				ui.Row(c).Gap(5).AlignItems(ui.Center).Margin(18, 0, 0, 0).Children(func() {
					sym(c, "lock", 13, p.Mute)
					ui.Text(c, locales.OnboardingKeychain()).FontSize(12.5).TextColor(p.Mute)
				})
			})
		})
	})
}

// loginList is the ways to sign in, with what the one under way needs.
func (a *App) loginList(c *ui.Context) {
	p := a.p
	s := a.C.SignIn
	ui.Column(c).Radius(10).Background(p.Card).Border(.5, p.Edge2).Clip().Children(func() {
		for i, o := range signIns() {
			on := s != nil && s.Method == o.method
			if a.loginRow(c, i, "login:"+o.method, o.symbol, o.title, o.sub, on).Clicked() && !on {
				a.keyOpen = false
				a.do(a.C.Login(proto.Login{Provider: o.provider, Method: o.method}))
			}
			if on {
				a.loginWait(c, o.provider)
			}
		}
		if a.loginRow(c, len(signIns()), "login:key", "key", "API key", locales.ApiKeySub(), a.keyOpen).Clicked() {
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
				ui.Text(c, title).FontSize(14).FontWeight(500).TextColor(p.Fg).SingleLine()
				ui.Text(c, sub).FontSize(13).TextColor(p.Mute).SingleLine().Ellipsis("…")
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
			ui.Text(c, locales.SignInFailed(s.Err)).FontSize(13.5).TextColor(p.Minus).Selectable()
		case s.Code != "":
			ui.Text(c, locales.DeviceCodeVisit(strings.TrimPrefix(s.CodeURL, "https://"))).FontSize(13.5).TextColor(p.Fg2).Selectable()
			ui.Row(c).Gap(6).Children(func() {
				for _, r := range s.Code {
					if r == '-' {
						ui.Text(c, "-").FontSize(17).Font("monospace").TextColor(p.Mute).Width(10).TextAlign(ui.Center).AlignSelf(ui.Center)
						continue
					}
					ui.Box(c).Size(28, 34).Radius(6).Background(p.Chip).Center().Children(func() {
						ui.Text(c, string(r)).FontSize(17).FontWeight(600).Font("monospace").TextColor(p.Fg)
					})
				}
			})
		case s.URL != "":
			ui.Text(c, locales.SignInBrowserOpened()).FontSize(13.5).TextColor(p.Fg2)
		default:
			ui.Text(c, locales.OpeningBrowser()).FontSize(13.5).TextColor(p.Fg2)
		}
		ui.Row(c).Gap(8).Children(func() {
			if s.Code != "" && s.Err == "" {
				if a.barButton(c, "login-copy", "copy", locales.CopyCode(), func() {}).Clicked() {
					c.WriteClipboard(s.Code)
				}
			}
			if s.URL != "" && s.Code == "" && s.Err == "" && s.Method == "codex" {
				if a.button(c, "login-device", locales.UseDeviceCode(), "", false).Clicked() {
					a.do(a.C.Login(proto.Login{Provider: provider, Method: s.Method, Device: true}))
				}
			}
			label := locales.Cancel()
			if s.Err != "" {
				label = locales.Ok()
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
			ui.TextInputBase(c, &a.key).Label("API key").Password().Placeholder(locales.PasteKey()).PlaceholderColor(p.Mute).
				FontSize(13.5).TextColor(p.Fg).SingleLine().Grow(1).MinWidth(0).Height(28).Radius(7).Background(p.Chip).Padding(0, 10)
		})
		if s != nil && s.Method == "" && s.Err != "" {
			ui.Text(c, locales.KeySaveFailed(s.Err)).FontSize(13.5).TextColor(p.Minus).Selectable()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			b := a.button(c, "key-save", locales.Save(), "", true)
			b.Disabled(strings.TrimSpace(a.key) == "" || s != nil && s.Method == "" && s.Err == "")
			if b.Clicked() {
				cmds, err := a.C.Login(proto.Login{Provider: a.keyProvider, Key: a.key})
				a.key = ""
				a.do(cmds, err)
			}
			ui.Text(c, locales.KeyCreateAt(keyURL(providers, a.keyProvider))).FontSize(13).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0).Selectable()
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
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
	})
	return b
}
