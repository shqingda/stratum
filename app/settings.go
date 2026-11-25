package app

import (
	"fmt"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// settingsPages are the pages of the settings, in the order of the list,
// with their search keywords and page descriptions.
var settingsPages = []struct {
	id, symbol         string
	label, words, lede func(locales.Messages) string
}{
	{"general", "adjustments-horizontal", locales.Messages.SettingsGeneral, locales.Messages.SettingsGeneralWords, locales.Messages.SettingsGeneralLede},
	{"accounts", "sparkles", locales.Messages.SettingsAccountsAndModels, locales.Messages.SettingsAccountsAndModelsWords, locales.Messages.SettingsAccountsAndModelsLede},
	{"permissions", "shield-half", locales.Messages.SettingsPermissions, locales.Messages.SettingsPermissionsWords, locales.Messages.SettingsPermissionsLede},
	{"appearance", "palette", locales.Messages.Appearance, locales.Messages.SettingsAppearanceWords, locales.Messages.SettingsAppearanceLede},
	{"browser", "world", locales.Messages.SettingsBrowser, locales.Messages.SettingsBrowserWords, locales.Messages.SettingsBrowserLede},
	{"shortcuts", "keyboard", locales.Messages.SettingsShortcuts, locales.Messages.SettingsShortcutsWords, locales.Messages.SettingsShortcutsLede},
	{"mcp", "puzzle", locales.Messages.SettingsMcp, locales.Messages.SettingsMcpWords, locales.Messages.SettingsMcpLede},
}

// computerUseShown says whether General shows computer use's setting:
// only with STRATUM_COMPUTER_USE set, as the kernel offers it
// (computer.Enabled), until it is as good as other agents'.
func computerUseShown() bool { return os.Getenv("STRATUM_COMPUTER_USE") != "" }

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
	a.quiet(a.C.ReadHooks())
	a.quiet(a.C.ReadPlugins())
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

// settingsView renders the Material You two-pane Settings destination inside
// the main rounded stage (adjacent to the persistent Navigation Rail).
func (a *App) settingsView(c *ui.Context) {
	p := a.p
	curPage := a.settingsPage
	if curPage == "models" || curPage == "" {
		curPage = "accounts"
		if a.settingsPage == "" {
			curPage = "general"
		}
	}
	title := ""
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(220).Shrink(0).Background(p.SurfaceContainerLow)
		side.Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 18).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, locales.Settings()).FontSize(16).FontWeight(600).LetterSpacing(-.2).TextColor(p.OnSurface)
			})
			ui.Row(c).Height(34).Margin(0, 12, 10, 12).Padding(0, 12).Gap(8).Radius(17).
				Background(p.SurfaceContainerHigh).AlignItems(ui.Center).Children(func() {
				sym(c, "search", 14, p.OnSurfaceVariant)
				ui.TextInputBase(c.Key("settings-search"), &a.settingsQuery).Label(locales.SearchSettings()).
					Placeholder(locales.Search()).PlaceholderColor(p.OnSurfaceVariant).
					FontSize(13).TextColor(p.OnSurface).SingleLine().Grow(1).MinWidth(0)
			})
			ui.Column(c).Grow(1).Padding(0, 10, 8, 10).Gap(2).Children(func() {
				q := strings.ToLower(strings.TrimSpace(a.settingsQuery))
				for _, pg := range settingsPages {
					label := pg.label(locales.In(i18n.Locale()))
					if pg.id == curPage {
						title = label
					}
					if q != "" && !finds(pg.label, pg.words, q) || pg.id == "browser" && a.newBrowser == nil {
						continue
					}
					active := pg.id == curPage
					it := ui.ButtonBase(c.Key("settings-"+pg.id)).Row().AlignSelf(ui.Stretch).Justify(ui.Start).
						Height(38).Padding(0, 12).Gap(10).Radius(19).AlignItems(ui.Center).Label(label)
					m3Focus(it, p)
					iconColor, textColor, weight := p.OnSurfaceVariant, p.OnSurfaceVariant, 500
					if active {
						m3State(it, p, p.SecondaryContainer, true)
						iconColor, textColor, weight = p.OnSecondaryContainer, p.OnSecondaryContainer, 600
					} else {
						m3State(it, p, ui.Color{}, false)
					}
					if it.Clicked() {
						a.openSettingsPage(pg.id)
					}
					it.Children(func() {
						ui.Box(c).Width(18).Center().Children(func() { sym(c, pg.symbol, 17, iconColor) })
						ui.Text(c, label).FontSize(13.5).FontWeight(weight).TextColor(textColor).SingleLine().Ellipsis("…").MinWidth(0)
					})
				}
			})
			a.settingsBuildFoot(c)
		})
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Background(p.Surface).Clip().Children(func() {
			ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 20).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(17).FontWeight(600).LetterSpacing(-.2).TextColor(p.OnSurface).SingleLine()
				ui.Spacer(c)
				done := ui.ButtonBase(c.Key("settings-close")).Row().Shrink(0).Size(32, 32).Radius(16).Center().
					Label(locales.CloseSettings()).Tooltip(locales.CloseSettings()).KeyShortcuts(0, ui.KeyEscape)
				m3Focus(done, p)
				m3State(done, p, ui.Color{}, false)
				done.Children(func() { sym(c, "x", 16, p.OnSurfaceVariant) })
				if done.Clicked() {
					a.settingsOpen = false
					a.navPage = "sessions"
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Width(620).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(14, 24, 36, 24).Children(func() {
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
					case "browser":
						a.browserSettingsPage(c)
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

// section is the title above an M3 tonal group card; the first sits tighter.
func section(c *ui.Context, p *Palette, label string, first bool) {
	top := float32(16)
	if first {
		top = 2
	}
	ui.Text(c, label).FontSize(12.5).FontWeight(600).LetterSpacing(.15).TextColor(p.Primary).Margin(top, 4, 6, 4)
}

// note is the supporting copy under a group card.
func note(c *ui.Context, p *Palette, text string) {
	ui.Text(c, text).FontSize(12).LineHeight(1.45).TextColor(p.OnSurfaceVariant).Margin(8, 6, 0, 6)
}

func group(c *ui.Context, p *Palette, children func()) {
	ui.Column(c).Radius(18).Background(p.SurfaceContainerLow).Clip().Children(children)
}

// rows draws the rows of an M3 tonal group card, with a subtle divider between two.
type rows struct {
	p *Palette
	n int
}

func (r *rows) sep(c *ui.Context) {
	if r.n > 0 {
		ui.Box(c).Height(1).Shrink(0).Background(r.p.OutlineVariant.Alpha(.20))
	}
	r.n++
}

// row is a label, with a line under it if sub is set, and its control on the right.
func (r *rows) row(c *ui.Context, key, label, sub string, control func()) {
	r.sep(c)
	p := r.p
	ui.Row(c.Key(key)).MinHeight(42).Padding(8, 12, 8, 14).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.OnSurface)
			if sub != "" {
				ui.Text(c, sub).FontSize(12.5).LineHeight(1.45).TextColor(p.OnSurfaceVariant)
			}
		})
		control()
	})
}

// toggle is a row with a switch; set saves the new value.
func (r *rows) toggle(c *ui.Context, key, label string, on, disabled bool, set func(bool)) {
	r.row(c, key, label, "", func() {
		v := on
		ui.Switch(c, &v).Label(label).Disabled(disabled)
		if v != on {
			set(v)
		}
	})
}

// m3Segmented renders a borderless Material You tonal pill segmented track.
func (a *App) m3Segmented(c *ui.Context, key, label string, options []string, selected int) (int, bool) {
	p := a.p
	chosen := selected
	changed := false
	ui.Row(c.Key(key)).Height(32).Padding(3).Radius(16).Background(p.SurfaceContainerHigh).Gap(2).
		AlignItems(ui.Center).Shrink(0).Label(label).Children(func() {
		for i, opt := range options {
			on := i == selected
			b := ui.ButtonBase(c.Key(fmt.Sprintf("%s:%d", key, i))).Row().Height(26).Padding(0, 11).Gap(5).Radius(13).
				AlignItems(ui.Center).Label(opt).Checked(on)
			m3Focus(b, p)
			fg, weight := p.OnSurfaceVariant, 500
			if on {
				selBg := p.Surface
				if p.Dark {
					selBg = p.SurfaceContainerHighest
				}
				b.Background(selBg)
				fg, weight = p.OnSurface, 600
			} else {
				m3State(b, p, ui.Color{}, false)
			}
			b.Children(func() {
				if on {
					sym(c, "check", 13, fg)
				}
				ui.Text(c, opt).FontSize(12.5).FontWeight(weight).TextColor(fg).SingleLine()
			})
			if b.Clicked() && i != selected {
				chosen = i
				changed = true
			}
		}
	})
	return chosen, changed
}

// languages are the app's languages, each named in itself.
func languages() []struct{ tag, label string } {
	return []struct{ tag, label string }{{"", locales.LanguageSystem()}, {"zh-Hans", "简体中文"}, {"en", "English"}}
}

func (a *App) generalPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	group(c, p, func() {
		r := rows{p: p}
		r.row(c, "set-lang", locales.Language(), "", func() {
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
		r := rows{p: p}
		r.row(c, "set-enter", locales.ReturnKey(), "", func() {
			i := map[bool]int{false: 0, true: 1}[set.EnterSteers]
			if next, ok := a.m3Segmented(c, "set-enter-seg", locales.ReturnKey(), []string{locales.QueueIt(), locales.SendRightAway()}, i); ok {
				s := set
				s.EnterSteers = next == 1
				a.do(a.C.SaveSettings(s))
			}
		})
		r.toggle(c, "set-prevent-sleep", locales.PreventSleep(), set.PreventSleep, false, func(on bool) {
			s := set
			s.PreventSleep = on
			a.do(a.C.SaveSettings(s))
		})
		r.toggle(c, "set-suggestions", locales.PromptSuggestions(), !set.NoSuggestions, false, func(on bool) {
			s := set
			s.NoSuggestions = !on
			a.do(a.C.SaveSettings(s))
		})
	})
	note(c, p, map[bool]string{false: locales.CmdReturnSteers(), true: locales.CmdReturnQueues()}[set.EnterSteers])

	if computerUseShown() {
		section(c, p, locales.ComputerUse(), false)
		group(c, p, func() {
			r := rows{p: p}
			r.toggle(c, "set-computer", locales.ComputerUseToggle(), set.ComputerUse, false, func(on bool) {
				s := set
				s.ComputerUse = on
				a.do(a.C.SaveSettings(s))
			})
		})
		note(c, p, locales.ComputerUseNote())
	}

	section(c, p, locales.Notifications(), false)
	group(c, p, func() {
		r := rows{p: p}
		for _, n := range []struct{ kind, label string }{
			{"wait", locales.NotifyWaitSetting()}, {"done", locales.NotifyDoneSetting()}, {"fail", locales.NotifyFailSetting()}, {"badge", locales.DockBadge()},
		} {
			r.toggle(c, "set-quiet:"+n.kind, n.label, !slices.Contains(set.Quiet, n.kind), false, func(on bool) {
				s := set
				s.Quiet = slices.DeleteFunc(slices.Clone(set.Quiet), func(k string) bool { return k == n.kind })
				if !on {
					s.Quiet = append(s.Quiet, n.kind)
				}
				a.do(a.C.SaveSettings(s))
			})
		}
		r.row(c, "set-notify-when", locales.NotifyWhen(), "", func() {
			whens := []string{"", "always", "never"}
			i := max(slices.Index(whens, set.NotifyWhen), 0)
			if next, ok := a.m3Segmented(c, "set-notify-seg", locales.NotifyWhen(), []string{locales.NotifyInBackground(), locales.NotifyAlways(), locales.NotifyNever()}, i); ok {
				s := set
				s.NotifyWhen = whens[next]
				a.do(a.C.SaveSettings(s))
			}
		})
	})
}

func mineralSeedLabel(id string) string {
	switch id {
	case "basalt":
		return locales.PresetBasalt()
	case "malachite":
		return locales.PresetMalachite()
	case "ochre":
		return locales.PresetOchre()
	case "garnet":
		return locales.PresetGarnet()
	case "feldspar":
		return locales.PresetFeldspar()
	case "obsidian":
		return locales.PresetObsidian()
	}
	return id
}

func (a *App) themeLivePreview(c *ui.Context, set proto.Settings) {
	p := a.p
	style := NormalizeColorStyle(set.ColorStyle)
	seedHex := set.SeedColor
	if col, ok := ParseSeedHex(seedHex); ok {
		seedHex = FormatHex(col)
	} else {
		seedHex = DefaultSeedHex
	}
	ui.Column(c.Key("theme-live-preview")).Radius(18).Background(p.SurfaceContainerLow).
		Padding(12).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ThemePreview()).FontSize(12.5).FontWeight(600).TextColor(p.OnSurfaceVariant)
			ui.Spacer(c)
			ui.Row(c).Height(22).Padding(0, 8).Gap(5).Radius(11).Background(p.SurfaceContainerHigh).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(p.Primary)
				ui.Text(c, fmt.Sprintf("%s · %s", strings.ToUpper(string(style)), seedHex)).Font("monospace").FontSize(10.5).FontWeight(600).TextColor(p.OnSurfaceVariant)
			})
		})
		ui.Row(c).Height(72).Radius(12).Background(p.SurfaceContainer).Clip().AlignItems(ui.Stretch).Children(func() {
			// Miniature Navigation Rail
			ui.Column(c).Width(28).Padding(8, 0).Gap(6).AlignItems(ui.Center).Background(p.SurfaceContainer).Children(func() {
				ui.Box(c).Size(14, 14).Radius(5).Background(p.PrimaryContainer)
				ui.Box(c).Size(18, 10).Radius(5).Background(p.SecondaryContainer)
				ui.Box(c).Size(10, 10).Radius(5).Background(p.OutlineVariant)
			})
			// Miniature Session Sidebar
			ui.Column(c).Width(92).Padding(8, 6).Gap(5).Background(p.SurfaceContainerLow).Children(func() {
				ui.Box(c).FillWidth().Height(14).Radius(7).Background(p.SecondaryContainer)
				ui.Box(c).WidthPercent(78).Height(8).Radius(4).Background(p.OutlineVariant)
				ui.Box(c).WidthPercent(64).Height(8).Radius(4).Background(p.OutlineVariant)
			})
			// Miniature Conversation Stage
			ui.Column(c).Grow(1).Padding(10, 12).Gap(8).Background(p.Surface).Children(func() {
				ui.Row(c).Justify(ui.End).Children(func() {
					ui.Box(c).Width(96).Height(18).Radius(9, 3, 9, 9).Background(p.SecondaryContainer)
				})
				ui.Column(c).FillWidth().Padding(8).Gap(5).Radius(10).Background(p.SurfaceContainerLow).Children(func() {
					ui.Box(c).WidthPercent(82).Height(7).Radius(3.5).Background(p.OnSurface.Alpha(.75))
					ui.Box(c).WidthPercent(56).Height(7).Radius(3.5).Background(p.OnSurfaceVariant.Alpha(.5))
				})
				ui.Row(c).Justify(ui.End).Children(func() {
					ui.Box(c).Width(42).Height(18).Radius(9).Background(p.Primary).Center().Children(func() {
						ui.Box(c).Size(16, 4).Radius(2).Background(p.OnPrimary)
					})
				})
			})
		})
	})
}

func (a *App) appearancePage(c *ui.Context) {
	p := a.p
	set := a.settings()
	activeHex := DefaultSeedHex
	if col, ok := ParseSeedHex(set.SeedColor); ok {
		activeHex = FormatHex(col)
	}
	if a.seedDraft == "" {
		a.seedDraft = activeHex
	}
	a.themeLivePreview(c, set)

	section(c, p, locales.Appearance(), false)
	group(c, p, func() {
		r := &rows{p: p}
		r.row(c, "set-theme", locales.Appearance(), "", func() {
			modes := []string{"", "light", "dark"}
			i := max(slices.Index(modes, set.Theme), 0)
			if next, ok := a.m3Segmented(c, "set-theme-seg", locales.Appearance(), []string{locales.ThemeAuto(), locales.ThemeLight(), locales.ThemeDark()}, i); ok {
				s := set
				s.Theme = modes[next]
				a.do(a.C.SaveSettings(s))
			}
		})
		r.row(c, "set-pure-black", locales.PureBlack(), locales.PureBlackNote(), func() {
			v := set.PureBlack
			ui.Switch(c, &v).Label(locales.PureBlack())
			if v != set.PureBlack {
				s := set
				s.PureBlack = v
				a.do(a.C.SaveSettings(s))
			}
		})
		r.row(c, "set-color-style", locales.ColorStyle(), "", func() {
			styles := []ColorStyle{ColorStyleSoft, ColorStyleVivid, ColorStyleTinted, ColorStyleMonochrome}
			curStyle := NormalizeColorStyle(set.ColorStyle)
			i := max(slices.Index(styles, curStyle), 0)
			labels := []string{locales.ColorStyleSoft(), locales.ColorStyleVivid(), locales.ColorStyleTinted(), locales.ColorStyleMonochrome()}
			if next, ok := a.m3Segmented(c, "set-style-seg", locales.ColorStyle(), labels, i); ok {
				s := set
				s.ColorStyle = string(styles[next])
				a.do(a.C.SaveSettings(s))
			}
		})
		r.row(c, "set-seed-color", locales.SeedColor(), "", func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				for _, ms := range MineralSeeds {
					swatchCol, _ := ParseSeedHex(ms.Hex)
					on := strings.EqualFold(activeHex, ms.Hex)
					label := mineralSeedLabel(ms.ID)
					sw := ui.ButtonBase(c.Key("seed:"+ms.ID)).Size(32, 32).Radius(16).Center().
						Background(swatchCol).Label(label).Tooltip(label + " " + ms.Hex).Checked(on)
					m3Focus(sw, p)
					if on {
						sw.Border(2, p.OnSurface)
						sw.Children(func() {
							fg := ui.RGB(255, 255, 255)
							darkInk := ui.RGB(0, 0, 0)
							if ContrastRatio(darkInk, swatchCol) > ContrastRatio(fg, swatchCol) {
								fg = darkInk
							}
							sym(c, "check", 15, fg)
						})
					}
					if sw.Clicked() {
						s := set
						s.SeedColor = ms.Hex
						a.seedDraft = ms.Hex
						a.do(a.C.SaveSettings(s))
					}
				}
			})
		})
		r.row(c, "set-seed-hex", locales.CustomHexColor(), "", func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				inp := ui.TextInputBase(c.Key("seed-hex-input"), &a.seedDraft).Label(locales.CustomHexColor()).
					Placeholder(DefaultSeedHex).PlaceholderColor(p.OnSurfaceVariant).
					Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().
					Width(104).Height(32).Padding(0, 10).Radius(10).Background(p.SurfaceContainerHighest)
				parsed, valid := ParseSeedHex(a.seedDraft)
				apply := a.button(c, "seed-hex-apply", locales.Save(), "", valid && !strings.EqualFold(FormatHex(parsed), activeHex))
				apply.Disabled(!valid)
				if (inp.Submitted() || apply.Clicked()) && valid {
					norm := FormatHex(parsed)
					a.seedDraft = norm
					s := set
					s.SeedColor = norm
					a.do(a.C.SaveSettings(s))
				}
			})
		})
		r.row(c, "set-size", locales.TextSize(), "", func() {
			sizes := []string{"small", "", "large"}
			i := max(slices.Index(sizes, set.TextSize), 0)
			if next, ok := a.m3Segmented(c, "set-size-seg", locales.TextSize(), []string{locales.TextSmall(), locales.TextDefault(), locales.TextLarge()}, i); ok {
				s := set
				s.TextSize = sizes[next]
				a.do(a.C.SaveSettings(s))
			}
		})
		r.toggle(c, "set-reduce-motion", locales.ReduceMotion(), set.ReduceMotion, false, func(on bool) {
			s := set
			s.ReduceMotion = on
			a.do(a.C.SaveSettings(s))
		})
	})
	note(c, p, locales.ReduceMotionNoteM3())
	section(c, p, locales.Conversation(), false)
	group(c, p, func() {
		r := &rows{p: p}
		r.row(c, "set-detail", locales.FinishedTurns(), "", func() {
			details := []string{"", "thinking", "all"}
			i := max(slices.Index(details, set.Detail), 0)
			if next, ok := a.m3Segmented(c, "set-detail-seg", locales.FinishedTurns(), []string{locales.DetailPlain(), locales.DetailThinking(), locales.DetailAll()}, i); ok {
				s := set
				s.Detail = details[next]
				a.do(a.C.SaveSettings(s))
			}
		})
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
	ui.Row(c).Height(42).Shrink(0).Padding(0, 14).Gap(6).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.OutlineVariant).Children(func() {
		v := ui.Text(c, "Stratum "+version).FontSize(11.5).TextColor(p.OnSurfaceVariant).SingleLine()
		if tip != "" {
			v.Tooltip(tip)
		}
		ui.Spacer(c)
		if a.checkUpdates != nil {
			b := ui.ButtonBase(c.Key("check-updates")).MinHeight(26).Padding(4, 10).Radius(13).Center().Label(locales.CheckForUpdates())
			m3Focus(b, p)
			m3State(b, p, ui.Color{}, false)
			b.Children(func() { ui.Text(c, locales.CheckForUpdates()).FontSize(11.5).FontWeight(500).TextColor(p.Primary).SingleLine() })
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
	if len(a.C.Accounts) > 0 {
		group(c, p, func() {
			r := rows{p: p}
			for _, acc := range a.C.Accounts {
				r.sep(c)
				a.accountRow(c, acc)
			}
		})
		ui.Box(c).Height(12).Shrink(0)
	}
	a.loginList(c)
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
	ui.Row(c.Key("account-row:"+acc.ID)).MinHeight(50).Padding(8, 10, 8, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(32, 32).Radius(10).Background(p.SurfaceContainerHigh).Center().Shrink(0).Children(func() {
			sym(c, providerSymbol(acc.Provider), 16, p.OnSurface)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, name).FontSize(13.5).FontWeight(500).TextColor(p.OnSurface).SingleLine()
			ui.Text(c, sub).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
		})
		if qText, qCol := formatQuota(a.now(), p, acc.Quota); qText != "" {
			ui.Text(c, qText).FontSize(11.5).FontWeight(500).TextColor(qCol).
				Background(p.SurfaceContainerHigh).Padding(3, 9).Radius(8).SingleLine()
		}
		outBtn := ui.ButtonBase(c.Key("logout:"+acc.ID)).Height(30).Padding(0, 12).Radius(15).Center().
			Label(locales.SignOut() + " " + name)
		m3Focus(outBtn, p)
		m3State(outBtn, p, ui.Transparent, false)
		outBtn.Children(func() {
			ui.Text(c, locales.SignOut()).FontSize(12.5).FontWeight(500).TextColor(p.Error).SingleLine()
		})
		if outBtn.Clicked() {
			a.do(a.C.Logout(acc.ID))
		}
	})
}

func formatQuotaReset(now time.Time, q *proto.AccountQuota) string {
	if q == nil || q.ResetAt.IsZero() || !q.ResetAt.After(now) {
		return ""
	}
	d := q.ResetAt.Sub(now)
	var dur string
	switch {
	case d >= 24*time.Hour:
		dur = q.ResetAt.Local().Format("01-02")
	case d >= time.Hour:
		dur = locales.AgoHours(int(d / time.Hour))
	default:
		dur = locales.AgoMinutes(max(1, int(d/time.Minute)))
	}
	return locales.QuotaResetsIn(dur)
}

func formatQuota(now time.Time, p *Palette, q *proto.AccountQuota) (string, ui.Color) {
	if q == nil || q.RemainingPct < 0 {
		return "", p.OnSurfaceVariant
	}
	col := p.Plus
	if q.RemainingPct < 15 {
		col = p.Minus
	} else if q.RemainingPct < 35 {
		col = p.Primary
	}
	text := locales.QuotaRemaining(q.RemainingPct)
	if reset := formatQuotaReset(now, q); reset != "" {
		text += " · " + reset
	}
	return text, col
}

func (a *App) modelsPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	section(c, p, locales.NewSessions(), true)
	group(c, p, func() {
		r := rows{p: p}
		r.row(c, "set-model-row", locales.DefaultModel(), locales.DefaultModelNote(), func() { a.defaultModel(c, set) })
		r.row(c, "set-effort-row", locales.ReasoningEffort(), "", func() {
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
		r := rows{p: p}
		r.row(c, "role:sub", locales.Subtasks(), locales.SubtasksNote(), func() {
			a.roleModel(c, "sub", set.Sub, &a.subMenu, func(ref *proto.ModelRef) { s := set; s.Sub = ref; a.do(a.C.SaveSettings(s)) })
		})
		r.row(c, "role:light", locales.TitlesAndSummaries(), locales.LightNote(), func() {
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
		r := rows{p: p}
		r.toggle(c, "set-fallback", locales.SwitchAccount(), set.Fallback && len(names) > 0, len(names) == 0, func(on bool) {
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
	if a.menu.modelsAccount != account && a.setModelMenu {
		a.menu.modelsRequested = false
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
		a.menu.modelsRequested = false
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
			if a.menu.modelsAccount != ref.Account {
				a.menu.modelsRequested = false
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
		ui.Row(c).Gap(10).AlignItems(ui.Stretch).Children(func() {
			for _, m := range modeRows() {
				chosen := mode
				it := ui.RadioBase(c.Key("set-mode:"+m.id), &chosen, m.id).Column().Grow(1).Basis(0).MinWidth(0).Padding(14).Gap(6).Radius(16).AlignItems(ui.Start).Justify(ui.Start).
					Label(m.label)
				m3Focus(it, p)
				fg, subCol := p.OnSurface, p.OnSurfaceVariant
				if m.id == mode {
					it.Background(p.SecondaryContainer)
					fg, subCol = p.OnSecondaryContainer, p.OnSecondaryContainer
				} else {
					it.Background(p.SurfaceContainerLow)
				}
				if chosen != mode {
					s := set
					s.Mode = chosen
					a.do(a.C.SaveSettings(s))
				}
				it.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						sym(c, m.icon, 16, fg)
						ui.Text(c, m.label).FontSize(13.5).FontWeight(600).TextColor(fg)
					})
					ui.Text(c, m.sub).FontSize(12).LineHeight(1.45).TextColor(subCol)
				})
			}
		})
	}).Label(locales.NewSessionPermissions())
	section(c, p, locales.AlwaysAllowed(), false)
	group(c, p, func() {
		r := rows{p: p}
		if len(set.Rules) == 0 {
			ui.Text(c, locales.NoRules()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Padding(12, 16)
		}
		for i, rule := range set.Rules {
			what := ruleText(rule)
			r.row(c, fmt.Sprintf("rule:%d:%s:%s", i, rule.Tool, rule.Prefix), what, "", func() {
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
	dot := ui.Box(c).Size(18, 18).Radius(9).Shrink(0).Center()
	if !on {
		dot.Border(1.5, p.Outline)
		return
	}
	dot.Background(p.Primary).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(p.OnPrimary)
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
			{locales.NewSession(), "⌘N"},
			{locales.CommandPalette(), "⌘K / ⇧⌘P"},
			{locales.FocusComposer(), "⌘L"},
			{locales.SwitchModel(), "⌘/"},
			{locales.SwitchPermissionMode(), "⇧Tab"},
			{locales.FindInSession(), "⌘F"},
			{locales.FindNext(), "⌘G / ⇧⌘G"},
			{locales.CopyLastReply(), "⌥⌘C"},
			{locales.Rewind(), "esc esc"},
			{locales.RecallLastPrompt(), "↑"},
			{locales.SwitchSessionPrevNext(), "⌘[ / ⌘]"},
			{locales.SessionsOneToNine(), "⌘1 – ⌘9"},
			{locales.NextWaiting(), "⌘J"},
			{locales.Archive() + " / " + locales.Unarchive(), "⇧⌘A"},
			{locales.ToggleSidebar(), "⌘B / ⌘\\"},
			{locales.ToggleInspector(), "⌘⌥I"},
			{locales.Settings(), "⌘,"},
			{locales.CloseSheetOrWindow(), "⌘W"},
			{locales.ZoomIn() + " / " + locales.ZoomOut(), "⌘+ / ⌘−"},
			{locales.ActualSize(), "⌘0"},
		}},
		{locales.Approvals(), []key{
			{locales.Allow(), "↩"}, {locales.AllowForSession(), "⌘↩"}, {locales.AllowAlways(), "⌥⌘↩"}, {locales.Deny(), "esc"},
		}},
		{locales.WhileWorking(), []key{
			{locales.Stop(), "esc / ⌘."}, {locales.SendRightAway(), now}, {locales.QueueIt(), queue},
		}},
	}
}

func (a *App) shortcutsPage(c *ui.Context) {
	p := a.p
	ui.Row(c).Height(38).Margin(4, 0, 8, 0).Padding(0, 14).Gap(8).Radius(19).
		Background(p.SurfaceContainerHigh).AlignItems(ui.Center).Children(func() {
		sym(c, "search", 15, p.OnSurfaceVariant)
		ui.TextInputBase(c.Key("keys-search"), &a.keysQuery).Label(locales.SearchShortcuts()).
			Placeholder(locales.Search()).PlaceholderColor(p.OnSurfaceVariant).
			FontSize(13.5).TextColor(p.OnSurface).SingleLine().Grow(1).MinWidth(0)
	})
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
			r := rows{p: p}
			for _, k := range keys {
				r.row(c, "key:"+k.label, k.label, "", func() {
					ui.Text(c, k.key).Font("monospace").FontSize(12).FontWeight(500).TextColor(p.OnSurfaceVariant).
						Background(p.SurfaceContainerHigh).Padding(3, 8).Radius(8).SingleLine()
				})
			}
		})
	}
	if !found {
		note(c, p, locales.NoShortcutMatches())
	}
}

// picker is a value that opens a menu: a muted prefix, the value and a selector mark.
func (a *App) picker(c *ui.Context, key, prefix, value string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(32).Padding(0, 10, 0, 12).Gap(6).Radius(10).AlignItems(ui.Center).
		Shrink(0).Label(strings.TrimSpace(prefix + " " + value))
	m3Focus(b, p)
	m3State(b, p, p.SurfaceContainerHigh, false)
	b.Children(func() {
		if prefix != "" {
			ui.Text(c, prefix).FontSize(12.5).TextColor(p.OnSurfaceVariant).SingleLine()
		}
		ui.Text(c, value).FontSize(13).FontWeight(500).TextColor(p.OnSurface).SingleLine()
		sym(c, "selector", 12, p.OnSurfaceVariant)
	})
	return b
}

// pushButton is an M3 tonal action pill button used in Settings.
func (a *App) pushButton(c *ui.Context, key, label string) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Height(32).Padding(0, 14).Radius(16).Center().Label(label)
	m3Focus(b, p)
	m3State(b, p, p.SecondaryContainer, false)
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.OnSecondaryContainer).SingleLine() })
	return b
}

func providerSymbol(provider string) string {
	switch provider {
	case "codex", "openai":
		return "brand-openai"
	case "xai":
		return "brand-x"
	case "openrouter", "fireworks", "vercel-ai-gateway", "opencode", "opencode-go":
		return "route"
	case "copilot":
		return "brand-github"
	case "kimi-coding", "kimi-coding-global":
		return "sparkles"
	}
	return "key"
}
