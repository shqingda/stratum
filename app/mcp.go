package app

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
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
	hookEditing      bool
	hookEditIdx      int
	hookProject      bool
	hookEventIdx     int // 0: PreToolUse, 1: PostToolUse, 2: Stop
	hookMatcher      string
	hookCommand      string
	pluginFormOpen   bool
	pluginProject    bool
	pluginSource     string
}

// extensionsView renders the first-class Extensions destination (MCP, Skills,
// Agents, Hooks, Plugins, Formatters) inside the main rounded stage.
func (a *App) extensionsView(c *ui.Context) {
	p := a.p
	ui.Column(c).Fill().Background(p.Surface).Clip().Children(func() {
		ui.Row(c).Height(barHeight).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).
			DragWindow().Children(func() {
			sym(c, "puzzle", 18, p.Primary)
			ui.Text(c, locales.Extensions()).FontSize(17).FontWeight(600).LetterSpacing(-.2).TextColor(p.OnSurface).SingleLine()
			ui.Spacer(c)
			if a.C.MCPFor != "" {
				ui.Text(c, home(a.C.MCPFor)).Font("monospace").FontSize(11.5).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…").MaxWidth(260)
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Width(680).MaxWidthPercent(100).AlignSelf(ui.Center).Padding(20, 28, 52, 28).Children(func() {
				a.mcpPage(c)
			})
		})
	})
}

// mcpPage lists the MCP servers, skills, hooks, and plugins of the current session's folder.
func (a *App) mcpPage(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	ui.Row(c).Margin(0, 0, 14, 0).AlignItems(ui.Center).Children(func() {
		if next, ok := a.m3Segmented(c, "ext-tabs", locales.SettingsMcp(),
			[]string{locales.McpServers(), locales.Agents(), locales.Hooks(), locales.Plugins(), locales.Formatters()}, st.tab); ok {
			st.tab = next
		}
	})
	switch st.tab {
	case 1:
		a.agentsSection(c)
		return
	case 2:
		a.hooksSection(c)
		return
	case 3:
		a.pluginsSection(c)
		return
	case 4:
		a.formattersSection(c)
		return
	}
	l := a.C.MCP
	ui.Row(c).Margin(6, 0, 8, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.McpServers()).FontSize(13).FontWeight(600).TextColor(p.OnSurface).Shrink(0)
		ui.Spacer(c)
		if a.C.MCPFor != "" {
			ui.Text(c, home(a.C.MCPFor)).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…").MinWidth(0)
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
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
	case len(l.Servers) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(16).Gap(8).Children(func() {
				ui.Text(c, locales.McpEmpty()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
				ui.ScrollHorizontal(c).Radius(10).Background(p.SurfaceContainerHigh).Children(func() {
					ui.Text(c, `{"mcpServers": {"github": {"command": "github-mcp-server", "args": ["stdio"]}}}`).
						Font("monospace").FontSize(12).TextColor(p.OnSurface).Padding(8, 12).SingleLine().Selectable()
				})
			})
		})
	default:
		group(c, p, func() {
			for i, s := range l.Servers {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("mcp-sep:", i))).Height(1).Shrink(0).Background(p.OutlineVariant)
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
			ui.Column(c).Padding(14).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInputBase(c, &st.mcpName).Label(locales.ServerName()).Placeholder(locales.ServerName()).PlaceholderColor(p.OnSurfaceVariant).
						FontSize(13).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Width(150)
					ui.TextInputBase(c, &st.mcpTarget).Label(locales.CommandOrUrl()).Placeholder(locales.CommandOrUrl()).PlaceholderColor(p.OnSurfaceVariant).
						Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Grow(1).MinWidth(0)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.mcpProject]
					if next, ok := a.m3Segmented(c, "mcp-scope", locales.McpServers(), []string{locales.Global(), locales.Project()}, scope); ok {
						st.mcpProject = next == 1
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
	ui.Row(c.Key("mcp:"+s.Name)).MinHeight(56).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(34, 34).Radius(10).Background(p.SurfaceContainerHigh).Center().Shrink(0).Children(func() {
			sym(c, map[bool]string{true: "plug-connected", false: "plug"}[s.State == "ok"], 17, p.OnSurface)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.Name).FontSize(13.5).FontWeight(500).TextColor(p.OnSurface).SingleLine()
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[s.Project]).FontSize(11).FontWeight(500).TextColor(p.OnSecondaryContainer).Background(p.SecondaryContainer).Padding(1, 7).Radius(6)
			})
			if s.State == "fail" {
				ui.Text(c, locales.McpConnectFailed(s.Error)).FontSize(12).TextColor(p.Minus).SingleLine().Ellipsis("…").Selectable()
			} else {
				ui.Text(c, s.Target).Font("monospace").FontSize(11.5).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
			}
		})
		switch s.State {
		case "untrusted":
			if a.button(c, "mcp-trust:"+s.Name, locales.Enable(), "", true).Clicked() {
				a.do(a.C.TrustMCP(s.Name, true))
			}
		case "connecting":
			slot(c, p, "run")
			ui.Text(c, locales.Connecting()).FontSize(12).TextColor(p.OnSurfaceVariant).Shrink(0)
		case "ok":
			ui.Text(c, locales.ToolCount(len(s.Tools))).FontSize(12).TextColor(p.OnSurfaceVariant).Shrink(0).Tooltip(strings.Join(s.Tools, locales.ListSeparator()))
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
		ui.Scroll(c.Key("mcp-logs-scroll:"+s.Name)).MaxHeight(140).Margin(0, 12, 10, 14).Radius(10).Background(p.SurfaceContainerHigh).Children(func() {
			ui.ScrollHorizontal(c).Children(func() {
				ui.Text(c, strings.Join(s.Logs, "\n")).Font("monospace").FontSize(11.5).LineHeight(1.45).TextColor(p.OnSurfaceVariant).Padding(8, 10).Selectable()
			})
		})
	}
}

func (a *App) skillsSection(c *ui.Context, skills []proto.SkillInfo) {
	p := a.p
	st := &a.mcpUI
	ui.Row(c).Margin(20, 0, 8, 2).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Skills()).FontSize(13).FontWeight(600).TextColor(p.OnSurface).Shrink(0)
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
			ui.Text(c, locales.SkillsEmpty()).FontSize(12.5).TextColor(p.OnSurfaceVariant).Padding(14)
		})
		return
	}
	group(c, p, func() {
		for i, sk := range skills {
			if i > 0 {
				ui.Box(c.Key(fmt.Sprint("skill-sep:", i))).Height(1).Shrink(0).Background(p.OutlineVariant)
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
			ui.Column(c).Padding(14).Gap(10).Children(func() {
				ui.TextInputBase(c, &st.skillSource).Label(locales.SkillSource()).Placeholder(locales.SkillSource()).PlaceholderColor(p.OnSurfaceVariant).
					Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.skillProject]
					if next, ok := a.m3Segmented(c, "skill-install-scope", locales.Skills(), []string{locales.Global(), locales.Project()}, scope); ok {
						st.skillProject = next == 1
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
			ui.Column(c).Padding(14).Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInputBase(c, &st.skillName).Label(locales.SkillName()).Placeholder(locales.SkillName()).PlaceholderColor(p.OnSurfaceVariant).
						FontSize(13).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Width(150)
					ui.TextInputBase(c, &st.skillDesc).Label(locales.SkillDesc()).Placeholder(locales.SkillDesc()).PlaceholderColor(p.OnSurfaceVariant).
						FontSize(13).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Grow(1).MinWidth(0)
				})
				ui.TextAreaBase(c, &st.skillBody).Lines(3, 6).Placeholder(locales.SkillBody()).PlaceholderColor(p.OnSurfaceVariant).
					Font("monospace").FontSize(12.5).TextColor(p.OnSurface).Padding(10, 12).Radius(10).Background(p.SurfaceContainerHighest)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.skillProject]
					if next, ok := a.m3Segmented(c, "skill-new-scope", locales.Skills(), []string{locales.Global(), locales.Project()}, scope); ok {
						st.skillProject = next == 1
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
	ui.Column(c.Key("skill:" + sk.Name)).Children(func() {
		ui.Row(c).MinHeight(54).Padding(8, 12, 8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			row := ui.ButtonBase(c.Key("skill-toggle-view:" + sk.Name)).Row().Grow(1).MinWidth(0).Gap(12).AlignItems(ui.Center).Justify(ui.Start).Label(sk.Name).Expanded(open)
			if row.Clicked() {
				a.opened[key] = !open
			}
			row.Children(func() {
				ui.Box(c).Size(34, 34).Radius(10).Background(p.SurfaceContainerHigh).Center().Shrink(0).Children(func() {
					sym(c, "sparkles", 16, p.OnSurface)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, sk.Name).FontSize(13.5).FontWeight(500).TextColor(p.OnSurface).SingleLine()
						ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[sk.Project]).FontSize(11).FontWeight(500).TextColor(p.OnSecondaryContainer).Background(p.SecondaryContainer).Padding(1, 7).Radius(6)
					})
					ui.Text(c, sk.Description).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
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
			ui.Text(c, strings.TrimSpace(sk.Body)).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(p.OnSurfaceVariant).
				Padding(10, 12).Radius(10).Background(p.SurfaceContainerHigh).Margin(0, 12, 10, 58).Selectable()
		}
	})
}

func (a *App) hooksSection(c *ui.Context) {
	p := a.p
	st := &a.mcpUI
	if a.C.Hooks == nil {
		a.quiet(a.C.ReadHooks())
	}
	ui.Row(c).Margin(6, 0, 8, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Hooks()).FontSize(13).FontWeight(600).TextColor(p.OnSurface).Shrink(0)
		ui.Spacer(c)
		if a.pushButton(c, "hook-add", locales.AddHook()).Clicked() {
			if st.hookFormOpen && !st.hookEditing {
				st.hookFormOpen = false
			} else {
				st.hookFormOpen, st.hookEditing, st.hookEditIdx = true, false, -1
				st.hookEventIdx, st.hookMatcher, st.hookCommand = 0, "", ""
			}
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
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
	case len(hl.Hooks) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(16).Gap(8).Children(func() {
				ui.Text(c, locales.HooksEmpty()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
			})
		})
	default:
		group(c, p, func() {
			for i, h := range hl.Hooks {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("hook-sep:", i))).Height(1).Shrink(0).Background(p.OutlineVariant)
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
			ui.Column(c).Padding(14).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if next, ok := a.m3Segmented(c, "hook-event-seg", locales.Hooks(), events, st.hookEventIdx); ok {
						st.hookEventIdx = next
					}
					if st.hookEventIdx < 2 {
						ui.TextInputBase(c.Key("hook-matcher"), &st.hookMatcher).
							Label(locales.HookMatcher()).Placeholder(locales.HookMatcher()).PlaceholderColor(p.OnSurfaceVariant).
							Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest).Grow(1).MinWidth(0)
					} else {
						ui.Spacer(c)
					}
				})
				ui.TextInputBase(c.Key("hook-cmd"), &st.hookCommand).
					Label(locales.HookCommand()).Placeholder(locales.HookCommand()).PlaceholderColor(p.OnSurfaceVariant).
					Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					scope := map[bool]int{false: 0, true: 1}[st.hookProject]
					if next, ok := a.m3Segmented(c, "hook-scope", locales.Hooks(), []string{locales.Global(), locales.Project()}, scope); ok {
						st.hookProject = next == 1
					}
					ui.Spacer(c)
					if a.button(c, "hook-cancel", locales.Cancel(), "", false).Clicked() {
						st.hookFormOpen, st.hookEditing, st.hookEditIdx = false, false, -1
					}
					canSave := strings.TrimSpace(st.hookCommand) != ""
					if a.button(c, "hook-save", locales.Save(), "", true).Disabled(!canSave).Clicked() {
						matcher := strings.TrimSpace(st.hookMatcher)
						if st.hookEventIdx == 2 {
							matcher = ""
						}
						entry := proto.HookEntry{
							Event:   events[st.hookEventIdx],
							Matcher: matcher,
							Command: strings.TrimSpace(st.hookCommand),
							Project: st.hookProject,
						}
						var next []proto.HookEntry
						replaced := false
						if a.C.Hooks != nil {
							for i, existing := range a.C.Hooks.Hooks {
								if !existing.Editable {
									continue
								}
								if st.hookEditing && i == st.hookEditIdx {
									if existing.Project == st.hookProject {
										next = append(next, entry)
										replaced = true
									}
									continue
								}
								if existing.Project == st.hookProject {
									next = append(next, existing)
								}
							}
						}
						if !replaced {
							next = append(next, entry)
						}
						a.do(a.C.SaveHooks(st.hookProject, next))
						st.hookFormOpen, st.hookEditing, st.hookEditIdx = false, false, -1
						st.hookMatcher, st.hookCommand = "", ""
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
				ui.Text(c, h.Event).FontSize(12.5).FontWeight(600).TextColor(p.OnSurface)
				if h.Matcher != "" {
					ui.Text(c, h.Matcher).Font("monospace").FontSize(11.5).TextColor(p.OnSurfaceVariant).Padding(1, 7).Radius(6).Background(p.SurfaceContainerHigh)
				}
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[h.Project]).
					FontSize(11).FontWeight(500).TextColor(p.OnSecondaryContainer).Background(p.SecondaryContainer).Padding(1, 7).Radius(6)
			})
			ui.Text(c, h.Command).Font("monospace").FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
		})
		if h.Editable {
			if a.iconButton(c, fmt.Sprint("hook-edit:", idx), "pencil", locales.Edit()).Clicked() {
				st := &a.mcpUI
				st.hookFormOpen, st.hookEditing, st.hookEditIdx = true, true, idx
				st.hookProject, st.hookMatcher, st.hookCommand = h.Project, h.Matcher, h.Command
				switch h.Event {
				case "PostToolUse":
					st.hookEventIdx = 1
				case "Stop":
					st.hookEventIdx = 2
				default:
					st.hookEventIdx = 0
				}
			}
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
	ui.Row(c).Margin(6, 0, 8, 2).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Plugins()).FontSize(13).FontWeight(600).TextColor(p.OnSurface).Shrink(0)
		ui.Spacer(c)
		if a.pushButton(c, "plugin-add", locales.InstallPlugin()).Clicked() {
			st.pluginFormOpen = !st.pluginFormOpen
		}
	})
	if st.pluginFormOpen {
		ui.Column(c).Margin(0, 0, 10, 0).Children(func() {
			group(c, p, func() {
				ui.Column(c).Padding(14).Gap(10).Children(func() {
					ui.TextInputBase(c.Key("plugin-src"), &st.pluginSource).
						Label(locales.PluginSource()).Placeholder(locales.PluginSource()).PlaceholderColor(p.OnSurfaceVariant).
						Font("monospace").FontSize(12.5).TextColor(p.OnSurface).SingleLine().Height(32).Padding(0, 12).Radius(10).Background(p.SurfaceContainerHighest)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						scope := map[bool]int{false: 0, true: 1}[st.pluginProject]
						if next, ok := a.m3Segmented(c, "plugin-scope", locales.Plugins(), []string{locales.Global(), locales.Project()}, scope); ok {
							st.pluginProject = next == 1
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
		ui.Text(c, locales.Reading()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
	case len(pl.Plugins) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(16).Gap(8).Children(func() {
				ui.Text(c, locales.PluginsEmpty()).FontSize(12.5).TextColor(p.OnSurfaceVariant)
			})
		})
	default:
		group(c, p, func() {
			for i, pk := range pl.Plugins {
				if i > 0 {
					ui.Box(c.Key(fmt.Sprint("plug-sep:", i))).Height(1).Shrink(0).Background(p.OutlineVariant)
				}
				a.pluginRow(c, pk)
			}
		})
	}
}

func (a *App) pluginRow(c *ui.Context, pk proto.PluginInfo) {
	p := a.p
	ui.Row(c.Key("plug:"+pk.Name)).Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(34, 34).Radius(10).Background(p.SurfaceContainerHigh).Center().Children(func() {
			sym(c, "puzzle", 16, p.OnSurface)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, pk.Name).FontSize(13.5).FontWeight(600).TextColor(p.OnSurface).SingleLine()
				if pk.Version != "" {
					ui.Text(c, "v"+pk.Version).FontSize(11).TextColor(p.OnSurfaceVariant).SingleLine()
				}
				ui.Text(c, map[bool]string{true: locales.Project(), false: locales.Global()}[pk.Project]).
					FontSize(11).FontWeight(500).TextColor(p.OnSecondaryContainer).Background(p.SecondaryContainer).Padding(1, 7).Radius(6)
			})
			sub := pk.Description
			if sub == "" {
				sub = fmt.Sprintf("%d skills · %d hooks · %d MCP", pk.Skills, pk.Hooks, len(pk.MCPServers))
			}
			ui.Text(c, sub).FontSize(12).TextColor(p.OnSurfaceVariant).SingleLine().Ellipsis("…")
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
