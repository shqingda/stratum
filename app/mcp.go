package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/proto"
)

// mcpPage lists the MCP servers of the current session's folder.
func (a *App) mcpPage(c *ui.Context) {
	p := a.p
	ui.Text(c, "技能与 MCP").FontSize(20).FontWeight(650).LetterSpacing(-.3).TextColor(p.Fg).Margin(0, 0, 4, 0)
	ui.Text(c, "全局的 MCP 服务器写在 ~/.talos/mcp.json，项目的写在它的 .mcp.json。项目里的要你启用才会运行。").
		FontSize(13).LineHeight(1.5).TextColor(p.Fg2).Margin(0, 0, 22, 0).Selectable()
	l := a.C.MCP
	ui.Row(c).Margin(0, 0, 8, 0).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "MCP 服务器").FontSize(13).FontWeight(600).TextColor(p.Fg).Shrink(0)
		ui.Spacer(c)
		if a.C.MCPFor != "" {
			ui.Text(c, home(a.C.MCPFor)).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
	if l != nil && l.Error != "" {
		ui.Text(c, "配置没读成："+l.Error).FontSize(12.5).TextColor(p.Minus).Margin(0, 0, 8, 0).Selectable()
	}
	switch {
	case l == nil:
		ui.Text(c, "正在读取…").FontSize(12.5).TextColor(p.Mute)
	case len(l.Servers) == 0:
		group(c, p, func() {
			ui.Column(c).Padding(14).Gap(8).Children(func() {
				ui.Text(c, "还没有 MCP 服务器。配置文件这样写：").FontSize(12.5).TextColor(p.Fg2)
				ui.Text(c, `{"mcpServers": {"github": {"command": "github-mcp-server", "args": ["stdio"]}}}`).
					Font("monospace").FontSize(12).TextColor(p.Fg).Padding(8, 10).Radius(7).Background(p.Chip).Selectable()
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
				ui.Text(c, map[bool]string{true: "项目", false: "全局"}[s.Project]).FontSize(11).FontWeight(500).TextColor(p.Fg2).Background(p.Chip).Padding(1, 6).Radius(5)
			})
			if s.State == "fail" {
				ui.Text(c, "连接失败："+s.Error).FontSize(12).TextColor(p.Minus).SingleLine().Ellipsis("…").Selectable()
			} else {
				ui.Text(c, s.Target).Font("monospace").FontSize(11.5).TextColor(p.Mute).SingleLine().Ellipsis("…")
			}
		})
		switch s.State {
		case "untrusted":
			if a.button(c, "mcp-trust:"+s.Name, "启用", "", true).Clicked() {
				a.do(a.C.TrustMCP(s.Name, true))
			}
		case "connecting":
			slot(c, p, "run")
			ui.Text(c, "连接中").FontSize(12).TextColor(p.Mute).Shrink(0)
		case "ok":
			ui.Text(c, fmt.Sprintf("%d 个工具", len(s.Tools))).FontSize(12).TextColor(p.Mute).Shrink(0).Tooltip(strings.Join(s.Tools, "、"))
		}
		if s.Project && s.State != "untrusted" {
			a.iconButton(c, "mcp-menu:"+s.Name, "dots", s.Name+" 的操作").PopoverMenuButton(func(m *ui.Menu) {
				if m.Item("停用").ID("mcp-untrust:" + s.Name).Danger().Chosen() {
					a.do(a.C.TrustMCP(s.Name, false))
				}
			}, a.menuOptions(nil))
		}
	})
}
