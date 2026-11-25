package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/llm/auth"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
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
	ui.Column(c).Fill().Background(p.SurfaceContainer).Children(func() {
		ui.Row(c).Height(barHeight).Shrink(0).DragWindow()
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(16, 24, 24, 24).Children(func() {
				ui.Row(c).Width(840).MaxWidthPercent(100).Wrap().Gap(28).AlignItems(ui.Start).Justify(ui.Center).Children(func() {
					ui.Column(c).Width(300).Grow(1).MinWidth(240).Padding(8, 0, 0, 0).Children(func() {
						ui.Box(c).Size(56, 56).Radius(18).Background(p.PrimaryContainer).Center().Children(func() {
							brandMark(c, 32, p.OnPrimaryContainer)
						})
						ui.Text(c, locales.OnboardingTitle()).FontSize(24).FontWeight(650).LetterSpacing(-.4).TextColor(p.OnSurface).Margin(16, 0, 6, 0)
						ui.Text(c, locales.OnboardingLead()).FontSize(14).LineHeight(1.45).TextColor(p.OnSurfaceVariant).Margin(0, 0, 16, 0)
						ui.Row(c).Padding(10, 14).Gap(8).Radius(12).Background(p.SurfaceContainerLow).AlignItems(ui.Center).Children(func() {
							sym(c, "lock", 14, p.Primary)
							ui.Text(c, locales.OnboardingKeychain()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Grow(1).MinWidth(0)
						})
					})
					ui.Column(c).Width(440).MaxWidthPercent(100).Children(func() { a.loginList(c) })
				})
			})
		})
	})
}

// loginList is the ways to sign in, with what the one under way needs.
func (a *App) loginList(c *ui.Context) {
	p := a.p
	s := a.C.SignIn
	ui.Column(c).Radius(18).Background(p.SurfaceContainerLow).Clip().Children(func() {
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
		ui.Box(c).Height(1).Background(p.OutlineVariant)
	}
	row := ui.ButtonBase(c.Key(key)).Row().MinHeight(46).AlignItems(ui.Stretch).Label(title)
	m3Focus(row, p)
	if on {
		row.Background(p.SecondaryContainer.Alpha(.45))
	} else {
		m3State(row, p, ui.Color{}, false)
	}
	row.Children(func() {
		left := float32(14)
		if on && symbol != "key" {
			ui.Box(c).Width(3).Background(p.Primary)
			left = 11
		}
		ui.Row(c).Grow(1).Padding(7, 14, 7, left).Gap(12).AlignItems(ui.Center).Children(func() {
			iconBg, iconFg := p.SurfaceContainerHigh, p.OnSurface
			if on {
				iconBg, iconFg = p.PrimaryContainer, p.OnPrimaryContainer
			}
			ui.Box(c).Size(36, 36).Radius(12).Background(iconBg).Center().Shrink(0).Children(func() {
				sym(c, symbol, 18, iconFg)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(14).FontWeight(500).TextColor(p.OnSurface).SingleLine()
				ui.Text(c, sub).FontSize(12.5).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
			})
			switch {
			case on && symbol != "key":
				slot(c, p, "run")
			case !on:
				sym(c, "chevron-right", 16, p.OnSurfaceVariant)
			}
		})
	})
	return row
}

// loginWait says what the sign-in under way waits for.
func (a *App) loginWait(c *ui.Context, provider string) {
	p := a.p
	s := a.C.SignIn
	ui.Column(c).Padding(4, 16, 16, 64).Gap(8).Children(func() {
		switch {
		case s.Err != "":
			ui.Text(c, locales.SignInFailed(s.Err)).FontSize(13.5).TextColor(p.Minus).Selectable()
		case s.Code != "":
			ui.Text(c, locales.DeviceCodeVisit(strings.TrimPrefix(s.CodeURL, "https://"))).FontSize(13.5).TextColor(p.OnSurfaceVariant).Selectable()
			ui.Row(c).Gap(6).Children(func() {
				for _, r := range s.Code {
					if r == '-' {
						ui.Text(c, "-").FontSize(17).Font("monospace").TextColor(p.OnSurfaceVariant).Width(10).TextAlign(ui.Center).AlignSelf(ui.Center)
						continue
					}
					ui.Box(c).Size(28, 36).Radius(8).Background(p.SurfaceContainerHigh).Center().Children(func() {
						ui.Text(c, string(r)).FontSize(17).FontWeight(600).Font("monospace").TextColor(p.OnSurface)
					})
				}
			})
		case s.URL != "":
			ui.Text(c, locales.SignInBrowserOpened()).FontSize(13.5).TextColor(p.OnSurfaceVariant)
		default:
			ui.Text(c, locales.OpeningBrowser()).FontSize(13.5).TextColor(p.OnSurfaceVariant)
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
	ui.Column(c).Padding(4, 16, 16, 64).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.barButton(c, "key-provider", "selector", name, func() {}).Shrink(0).PopoverMenuButton(func(m *ui.Menu) {
				for _, kp := range providers {
					if m.Item(kp.Name).ID("key:" + kp.ID).Checked(kp.ID == a.keyProvider).Chosen() {
						a.keyProvider = kp.ID
					}
				}
			}, a.menuOptions(&a.keyMenu))
			ui.TextInputBase(c, &a.key).Label("API key").Password().Placeholder(locales.PasteKey()).PlaceholderColor(p.OnSurfaceVariant).
				FontSize(13.5).TextColor(p.OnSurface).SingleLine().Grow(1).MinWidth(0).Height(34).Radius(10).Background(p.SurfaceContainerHighest).Padding(0, 12)
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
			ui.Text(c, locales.KeyCreateAt(keyURL(providers, a.keyProvider))).FontSize(13).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…").MinWidth(0).Selectable()
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
	b := ui.ButtonBase(c.Key(key)).Row().Height(32).Padding(0, 12).Radius(16).AlignItems(ui.Center).Label(label)
	m3Focus(b, p)
	m3State(b, p, ui.Color{}, false)
	b.Children(func() {
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Primary).SingleLine()
	})
	return b
}
