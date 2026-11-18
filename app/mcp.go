package app

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

type mcpState struct {
	tab              int // 0: MCP & Skills, 1: Hooks, 2: Plugins
	mcpFormOpen      bool
	mcpProject       bool
	mcpName          string
	mcpTarget        string
	skillFormOpen    bool
	skillInstallOpen bool
	skillProject     bool
	skillName        string
	skillDesc        string
	skillBody        string
	skillSource      string
	mcpLogsOpen      map[string]bool
	hookFormOpen     bool
	hookProject      bool
	hookEventIdx     int // 0: PreToolUse, 1: PostToolUse, 2: Stop
	hookMatcher      string
	hookCommand      string
	pluginFormOpen   bool
	pluginProject    bool
	pluginSource     string
}

// mcpPage lists the MCP servers, skills, hooks, and plugins of the current session's folder.
func (a *App) mcpPage(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	ui.Row(c).Margin(0, 0, 12, 0).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &st.tab, locales.McpServers(), locales.Hooks(), locales.Plugins()).Label(locales.SettingsMcp())
	})
	switch st.tab {
	case 1:
		a.hooksSection(c)
		return
	case 2:
		a.pluginsSection(c)
		return
	}
	l := a.C.MCP
	ui.Row(c).Margin(6, 0, 7, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.McpServers()).FontSize(13).FontWeight(600).TextColor(p.Fg).Shrink(0)
		ui.Spacer(c)
		if a.C.MCPFor != "" {
			ui.Text(c, home(a.C.MCPFor)).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
		}
		if a.pushButton(c, "mcp-add", locales.AddMcpServer()).Clicked() {
			a.mcpUI.mcpFormOpen = !a.mcpUI.mcpFormOpen
		}
	})
	if a.mcpUI.mcpFormOpen {
		a.mcpAddForm(c)
	}
	if l != nil && l.Error != "" {
		ui.Text(c, locales.McpConfigError(l.Error)).FontSize(12.5).TextColor(p.Minus).Margin(0, 0, 8, 0).Selectable()
	}
	switch {
	case l == nil:
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.Mute)
	case len(l.Servers) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(14).Gap(8).Children(func() {
				ui.Text(c, locales.McpEmpty()).FontSize(12.5).TextColor(p.Fg2)
				ui.ScrollHorizontal(c).Radius(7).Background(p.Chip).Children(func() {
					ui.Text(c, `{"mcpServers": {"github": {"command": "github-mcp-server", "args": ["stdio"]}}}`).
						Font("monospace").FontSize(12).TextColor(p.Fg).Padding(8, 10).SingleLine().Selectable()
				})
			})
		})
	default:
		group(c, p, func() {
			for i, s := range l.Servers {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("mcp-sep:", i))).Height(.5).Shrink(0).Background(p.Edge)
				}
				a.mcpRow(c, s)
			}
		})
	}
	note(c, p, locales.McpNote())

	if l != nil {
		a.skillsSection(c, l.Skills)
	}
}

func (a *App) mcpAddForm(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
		group(c, p, func() {
			ui.Column(c).Padding(12).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInputBase(c, &st.mcpName).Label(locales.ServerName()).Placeholder(locales.ServerName()).PlaceholderColor(p.Mute).
						FontSize(13).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip).Width(150)
					ui.TextInputBase(c, &st.mcpTarget).Label(locales.CommandOrUrl()).Placeholder(locales.CommandOrUrl()).PlaceholderColor(p.Mute).
						Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip).Grow(1).MinWidth(0)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.mcpProject]
					if ui.Segmented(c, &scope, locales.Global(), locales.Project()).Label(locales.McpServers()).Changed() {
						st.mcpProject = scope == 1
					}
					ui.Spacer(c)
					if a.button(c, "mcp-form-cancel", locales.Cancel(), "", false).Clicked() {
						st.mcpFormOpen = false
					}
					canSave := strings.TrimSpace(st.mcpName) != "" && strings.TrimSpace(st.mcpTarget) != ""
					if a.button(c, "mcp-form-save", locales.Save(), "", true).Disabled(!canSave).Clicked() {
						req := parseMCPTarget(st.mcpName, st.mcpTarget, st.mcpProject)
						cmds, err := a.C.SaveMCP(req)
						if a.do(cmds, err); err == nil {
							st.mcpFormOpen, st.mcpName, st.mcpTarget = false, "", ""
						}
					}
				})
			})
		})
	})
}

func parseMCPTarget(name, target string, project bool) proto.MCPSave {
	req := proto.MCPSave{Name: strings.TrimSpace(name), Project: project}
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
		req.URL = t
		return req
	}
	parts := splitCmdWords(t)
	if len(parts) > 0 {
		req.Command = parts[0]
		if len(parts) > 1 {
			req.Args = parts[1:]
		}
	}
	return req
}

func splitCmdWords(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inWord := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inWord = true
		case unicode.IsSpace(r):
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

func (a *App) mcpRow(c *ui.Context, s proto.MCPServer) {
	p := a.p
	ui.Row(c.Key("mcp:"+s.Name)).MinHeight(52).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(30, 30).Radius(8).Background(p.Chip).Center().Shrink(0).Children(func() {
			sym(c, map[bool]string{true: "plug-connected", false: "plug"}[s.State == "ok"], 17, p.Fg)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.Name).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[s.Project]).FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
			})
			if s.State == "fail" {
				ui.Text(c, locales.McpConnectFailed(s.Error)).FontSize(12).TextColor(p.Minus).SingleLine().Ellipsis("…").Selectable()
			} else {
				ui.Text(c, s.Target).Font("monospace").FontSize(11.5).TextColor(p.Mute).SingleLine().Ellipsis("…")
			}
		})
		switch s.State {
		case "untrusted":
			if a.button(c, "mcp-trust:"+s.Name, locales.Enable(), "", true).Clicked() {
				a.do(a.C.TrustMCP(s.Name, true))
			}
		case "connecting":
			slot(c, p, "run")
			ui.Text(c, locales.Connecting()).FontSize(12).TextColor(p.Mute).Shrink(0)
		case "ok":
			ui.Text(c, locales.ToolCount(len(s.Tools))).FontSize(12).TextColor(p.Mute).Shrink(0).Tooltip(strings.Join(s.Tools, locales.ListSeparator()))
		}
		if len(s.Logs) > 0 && a.textButton(c, "mcp-logs:"+s.Name, locales.Logs()).Clicked() {
			if a.mcpUI.mcpLogsOpen == nil {
				a.mcpUI.mcpLogsOpen = map[string]bool{}
			}
			a.mcpUI.mcpLogsOpen[s.Name] = !a.mcpUI.mcpLogsOpen[s.Name]
		}
		a.iconButton(c, "mcp-menu:"+s.Name, "dots", locales.ItemActions(s.Name)).PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locales.Edit()).ID("mcp-edit:" + s.Name).Chosen() {
				a.mcpUI.mcpFormOpen = true
				a.mcpUI.mcpName = s.Name
				a.mcpUI.mcpTarget = s.Target
				a.mcpUI.mcpProject = s.Project
			}
			if s.Project && s.State != "untrusted" {
				if m.Item(locales.Disable()).ID("mcp-untrust:" + s.Name).Chosen() {
					a.do(a.C.TrustMCP(s.Name, false))
				}
			}
			m.Separator()
			if m.Item(locales.Remove()).ID("mcp-remove:" + s.Name).Danger().Chosen() {
				a.do(a.C.RemoveMCP(s.Name, s.Project))
			}
		}, a.menuOptions(nil))
	})
	if len(s.Logs) > 0 && a.mcpUI.mcpLogsOpen[s.Name] {
		ui.Scroll(c.Key("mcp-logs-scroll:"+s.Name)).MaxHeight(140).Margin(0, 12, 10, 14).Radius(7).Background(p.Chip).Children(func() {
			ui.ScrollHorizontal(c).Children(func() {
				ui.Text(c, strings.Join(s.Logs, "\n")).Font("monospace").FontSize(11.5).LineHeight(1.45).TextColor(p.Fg2).Padding(8, 10).Selectable()
			})
		})
	}
}

func (a *App) skillsSection(c *ui.Context, skills []proto.SkillInfo) {
	p := a.p
	st := &a.mcpUI
	ui.Row(c).Margin(20, 0, 7, 2).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Skills()).FontSize(13).FontWeight(600).TextColor(p.Fg).Shrink(0)
		ui.Spacer(c)
		if a.pushButton(c, "skill-install-btn", locales.InstallSkill()).Clicked() {
			st.skillInstallOpen = !st.skillInstallOpen
			st.skillFormOpen = false
		}
		if a.pushButton(c, "skill-new-btn", locales.NewSkill()).Clicked() {
			st.skillFormOpen = !st.skillFormOpen
			st.skillInstallOpen = false
		}
	})
	if st.skillInstallOpen {
		a.skillInstallForm(c)
	}
	if st.skillFormOpen {
		a.skillNewForm(c)
	}
	if len(skills) == 0 {
		group(c, p, func() {
			ui.Text(c, locales.SkillsEmpty()).FontSize(12.5).TextColor(p.Mute).Padding(12)
		})
		return
	}
	group(c, p, func() {
		for i, sk := range skills {
			if i > 0 {
				ui.Box(c.Key(fmt.Sprint("skill-sep:", i))).Height(.5).Shrink(0).Background(p.Edge)
			}
			a.skillRow(c, sk)
		}
	})
}

func (a *App) skillInstallForm(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
		group(c, p, func() {
			ui.Column(c).Padding(12).Gap(10).Children(func() {
				ui.TextInputBase(c, &st.skillSource).Label(locales.SkillSource()).Placeholder(locales.SkillSource()).PlaceholderColor(p.Mute).
					Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.skillProject]
					if ui.Segmented(c, &scope, locales.Global(), locales.Project()).Label(locales.Skills()).Changed() {
						st.skillProject = scope == 1
					}
					ui.Spacer(c)
					if a.button(c, "skill-install-cancel", locales.Cancel(), "", false).Clicked() {
						st.skillInstallOpen = false
					}
					canInstall := strings.TrimSpace(st.skillSource) != ""
					if a.button(c, "skill-install-go", locales.Install(), "", true).Disabled(!canInstall).Clicked() {
						cmds, err := a.C.SaveSkill(proto.SkillSave{Project: st.skillProject, Source: st.skillSource})
						if a.do(cmds, err); err == nil {
							st.skillInstallOpen, st.skillSource = false, ""
						}
					}
				})
			})
		})
	})
}

func (a *App) skillNewForm(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
		group(c, p, func() {
			ui.Column(c).Padding(12).Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInputBase(c, &st.skillName).Label(locales.SkillName()).Placeholder(locales.SkillName()).PlaceholderColor(p.Mute).
						FontSize(13).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip).Width(150)
					ui.TextInputBase(c, &st.skillDesc).Label(locales.SkillDesc()).Placeholder(locales.SkillDesc()).PlaceholderColor(p.Mute).
						FontSize(13).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip).Grow(1).MinWidth(0)
				})
				ui.TextAreaBase(c, &st.skillBody).Lines(3, 6).Placeholder(locales.SkillBody()).PlaceholderColor(p.Mute).
					Font("monospace").FontSize(12.5).TextColor(p.Fg).Padding(8, 10).Radius(7).Background(p.Chip)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.skillProject]
					if ui.Segmented(c, &scope, locales.Global(), locales.Project()).Label(locales.Skills()).Changed() {
						st.skillProject = scope == 1
					}
					ui.Spacer(c)
					if a.button(c, "skill-new-cancel", locales.Cancel(), "", false).Clicked() {
						st.skillFormOpen = false
					}
					canSave := strings.TrimSpace(st.skillName) != "" && strings.TrimSpace(st.skillDesc) != ""
					if a.button(c, "skill-new-save", locales.Save(), "", true).Disabled(!canSave).Clicked() {
						cmds, err := a.C.SaveSkill(proto.SkillSave{Name: st.skillName, Description: st.skillDesc, Body: st.skillBody, Project: st.skillProject})
						if a.do(cmds, err); err == nil {
							st.skillFormOpen, st.skillName, st.skillDesc, st.skillBody = false, "", "", ""
						}
					}
				})
			})
		})
	})
}

func (a *App) skillRow(c *ui.Context, sk proto.SkillInfo) {
	p := a.p
	key := "skill-open:" + sk.Name
	open := a.opened[key]
	ui.Column(c.Key("skill:"+sk.Name)).Children(func() {
		ui.Row(c).MinHeight(50).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			row := ui.ButtonBase(c.Key("skill-toggle-view:"+sk.Name)).Row().Grow(1).MinWidth(0).Gap(12).AlignItems(ui.Center).Justify(ui.Start).Label(sk.Name).Expanded(open)
			if row.Clicked() {
				a.opened[key] = !open
			}
			row.Children(func() {
				ui.Box(c).Size(30, 30).Radius(8).Background(p.Chip).Center().Shrink(0).Children(func() {
					sym(c, "sparkles", 16, p.Fg)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, sk.Name).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine()
						ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[sk.Project]).FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
					})
					ui.Text(c, sk.Description).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
				})
			})
			enabled := !sk.Disabled
			ui.Switch(c.Key("skill-sw:"+sk.Name), &enabled).Label(sk.Name)
			if enabled != !sk.Disabled {
				a.do(a.C.ToggleSkill(sk.Name, enabled))
			}
			a.iconButton(c, "skill-menu:"+sk.Name, "dots", locales.ItemActions(sk.Name)).PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(locales.Remove()).ID("skill-remove:" + sk.Name).Danger().Chosen() {
					a.do(a.C.RemoveSkill(sk.Name, sk.Project))
				}
			}, a.menuOptions(nil))
		})
		if open && strings.TrimSpace(sk.Body) != "" {
			ui.Text(c, strings.TrimSpace(sk.Body)).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(p.Fg2).
				Padding(9, 11).Radius(8).Background(p.Chip).Margin(0, 12, 10, 56).Selectable()
		}
	})
}

func (a *App) hooksSection(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	if a.C.Hooks == nil {
		a.quiet(a.C.ReadHooks())
	}
	ui.Row(c).Margin(6, 0, 7, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Hooks()).FontSize(13).FontWeight(600).TextColor(p.Fg).Shrink(0)
		ui.Spacer(c)
		if a.pushButton(c, "hook-add", locales.AddHook()).Clicked() {
			st.hookFormOpen = !st.hookFormOpen
		}
	})
	if st.hookFormOpen {
		a.hookAddForm(c)
	}
	hl := a.C.Hooks
	if hl != nil && hl.Error != "" {
		ui.Text(c, hl.Error).FontSize(12.5).TextColor(p.Minus).Margin(0, 0, 8, 0).Selectable()
	}
	switch {
	case hl == nil:
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.Mute)
	case len(hl.Hooks) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(16).Gap(8).Children(func() {
				ui.Text(c, locales.HooksEmpty()).FontSize(12.5).TextColor(p.Fg2)
			})
		})
	default:
		group(c, p, func() {
			for i, h := range hl.Hooks {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("hook-sep:", i))).Height(.5).Shrink(0).Background(p.Edge)
				}
				a.hookRow(c, i, h)
			}
		})
	}
}

func (a *App) hookAddForm(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	events := []string{"PreToolUse", "PostToolUse", "Stop"}
	ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
		group(c, p, func() {
			ui.Column(c).Padding(12).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Segmented(c, &st.hookEventIdx, events...).Label(locales.Hooks())
					if st.hookEventIdx < 2 {
						ui.TextInputBase(c.Key("hook-matcher"), &st.hookMatcher).
							Label(locales.HookMatcher()).Placeholder(locales.HookMatcher()).PlaceholderColor(p.Mute).
							Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip).Grow(1).MinWidth(0)
					} else {
						ui.Spacer(c)
					}
				})
				ui.TextInputBase(c.Key("hook-cmd"), &st.hookCommand).
					Label(locales.HookCommand()).Placeholder(locales.HookCommand()).PlaceholderColor(p.Mute).
					Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.hookProject]
					if ui.Segmented(c, &scope, locales.Global(), locales.Project()).Label(locales.Hooks()).Changed() {
						st.hookProject = scope == 1
					}
					ui.Spacer(c)
					if a.button(c, "hook-cancel", locales.Cancel(), "", false).Clicked() {
						st.hookFormOpen = false
					}
					canSave := strings.TrimSpace(st.hookCommand) != ""
					if a.button(c, "hook-save", locales.Save(), "", true).Disabled(!canSave).Clicked() {
						var next []proto.HookEntry
						if a.C.Hooks != nil {
							for _, existing := range a.C.Hooks.Hooks {
								if existing.Editable && existing.Project == st.hookProject {
									next = append(next, existing)
								}
							}
						}
						matcher := strings.TrimSpace(st.hookMatcher)
						if st.hookEventIdx == 2 {
							matcher = ""
						}
						next = append(next, proto.HookEntry{
							Event:   events[st.hookEventIdx],
							Matcher: matcher,
							Command: strings.TrimSpace(st.hookCommand),
							Project: st.hookProject,
						})
						a.do(a.C.SaveHooks(st.hookProject, next))
						st.hookFormOpen, st.hookMatcher, st.hookCommand = false, "", ""
					}
				})
			})
		})
	})
}

func (a *App) hookRow(c *ui.Context, idx int, h proto.HookEntry) {
	p := a.p
	ui.Row(c.Key(fmt.Sprint("hook:", idx))).Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, h.Event).FontSize(12.5).FontWeight(600).TextColor(p.Fg)
				if h.Matcher != "" {
					ui.Text(c, h.Matcher).Font("monospace").FontSize(11.5).TextColor(p.Fg2).Padding(1, 6).Radius(5).Background(p.Chip)
				}
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[h.Project]).
					FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
			})
			ui.Text(c, h.Command).Font("monospace").FontSize(12).TextColor(p.Fg2).SingleLine().Ellipsis("…")
		})
		if h.Editable {
			if a.iconButton(c, fmt.Sprint("hook-del:", idx), "trash", locales.Delete()).Clicked() && a.C.Hooks != nil {
				var next []proto.HookEntry
				for i, existing := range a.C.Hooks.Hooks {
					if i != idx && existing.Editable && existing.Project == h.Project {
						next = append(next, existing)
					}
				}
				a.do(a.C.SaveHooks(h.Project, next))
			}
		}
	})
}

func (a *App) pluginsSection(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	if a.C.Plugins == nil {
		a.quiet(a.C.ReadPlugins())
	}
	ui.Row(c).Margin(6, 0, 7, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Plugins()).FontSize(13).FontWeight(600).TextColor(p.Fg).Shrink(0)
		ui.Spacer(c)
		if a.pushButton(c, "plugin-add", locales.InstallPlugin()).Clicked() {
			st.pluginFormOpen = !st.pluginFormOpen
		}
	})
	if st.pluginFormOpen {
		ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
			group(c, p, func() {
				ui.Column(c).Padding(12).Gap(10).Children(func() {
					ui.TextInputBase(c.Key("plugin-src"), &st.pluginSource).
						Label(locales.PluginSource()).Placeholder(locales.PluginSource()).PlaceholderColor(p.Mute).
						Font("monospace").FontSize(12.5).TextColor(p.Fg).SingleLine().Height(30).Padding(0, 10).Radius(7).Background(p.Chip)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						scope := map[bool]int{false: 0, true: 1}[st.pluginProject]
						if ui.Segmented(c, &scope, locales.Global(), locales.Project()).Label(locales.Plugins()).Changed() {
							st.pluginProject = scope == 1
						}
						ui.Spacer(c)
						if a.button(c, "plugin-cancel", locales.Cancel(), "", false).Clicked() {
							st.pluginFormOpen = false
						}
						canSave := strings.TrimSpace(st.pluginSource) != ""
						if a.button(c, "plugin-save", locales.Install(), "", true).Disabled(!canSave).Clicked() {
							a.do(a.C.InstallPlugin(strings.TrimSpace(st.pluginSource), st.pluginProject))
							st.pluginFormOpen, st.pluginSource = false, ""
						}
					})
				})
			})
		})
	}
	pl := a.C.Plugins
	switch {
	case pl == nil:
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.Mute)
	case len(pl.Plugins) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(16).Gap(8).Children(func() {
				ui.Text(c, locales.PluginsEmpty()).FontSize(12.5).TextColor(p.Fg2)
			})
		})
	default:
		group(c, p, func() {
			for i, pk := range pl.Plugins {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("plug-sep:", i))).Height(.5).Shrink(0).Background(p.Edge)
				}
				a.pluginRow(c, pk)
			}
		})
	}
}

func (a *App) pluginRow(c *ui.Context, pk proto.PluginInfo) {
	p := a.p
	ui.Row(c.Key("plug:"+pk.Name)).Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(32, 32).Radius(8).Background(p.Chip).Center().Children(func() {
			sym(c, "puzzle", 16, p.Fg)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, pk.Name).FontSize(13).FontWeight(600).TextColor(p.Fg).SingleLine()
				if pk.Version != "" {
					ui.Text(c, "v"+pk.Version).FontSize(11).TextColor(p.Mute).SingleLine()
				}
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[pk.Project]).
					FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
			})
			sub := pk.Description
			if sub == "" {
				sub = fmt.Sprintf("%d skills · %d hooks · %d MCP", pk.Skills, pk.Hooks, len(pk.MCPServers))
			}
			ui.Text(c, sub).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
		})
		en := pk.Enabled
		if ui.Switch(c.Key("plug-sw:"+pk.Name), &en).Label(pk.Name).Changed() {
			a.do(a.C.TogglePlugin(pk.Name, en))
		}
		if a.iconButton(c, "plug-del:"+pk.Name, "trash", locales.Remove()).Clicked() {
			a.do(a.C.RemovePlugin(pk.Name))
		}
	})
}
