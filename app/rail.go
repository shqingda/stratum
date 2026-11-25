package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
)

const (
	railWidth         = 76
	railExpandedWidth = 180
)

func (a *App) curRailWidth() float32 {
	if a.railExpanded {
		return railExpandedWidth
	}
	return railWidth
}

// activeNavPage returns the active top-level destination in the stage:
// "sessions", "automations", "mcp", or "settings".
func (a *App) activeNavPage() string {
	switch {
	case a.settingsOpen:
		if a.settingsPage == "mcp" && a.navPage == "mcp" {
			return "mcp"
		}
		return "settings"
	case a.automationsOpen:
		return "automations"
	case a.navPage == "mcp":
		return "mcp"
	default:
		return "sessions"
	}
}

func (a *App) setNavPage(page string) {
	switch page {
	case "automations":
		a.openAutomations()
	case "mcp":
		a.navPage = "mcp"
		a.settingsOpen = false
		a.automationsOpen = false
		a.quiet(a.C.ReadMCP())
		a.quiet(a.C.ReadHooks())
		a.quiet(a.C.ReadPlugins())
	case "settings":
		a.openSettings()
	default:
		a.navPage = ""
		a.settingsOpen = false
		a.automationsOpen = false
	}
}

// rail renders FlClash-style collapsible NavigationSidebar on the far left.
func (a *App) rail(c *ui.Context) {
	p := a.p
	active := a.activeNavPage()
	topPad := float32(10)
	if tb := c.TitleBar(); tb.Left > 0 {
		// Compact macOS traffic-light clearance matching FlClash.
		topPad = max(tb.Height, 36)
	}

	w := c.Root().AnimateTo("rail-w", a.curRailWidth(), animStandard)
	showLabels := w > railWidth+24
	labelOp := float32(0)
	if showLabels {
		labelOp = min(1, (w-railWidth-24)/(railExpandedWidth-railWidth-24))
	}

	ui.Column(c.Key("nav-rail")).Width(w).Shrink(0).FillHeight().Background(p.SurfaceContainer).
		Padding(0, 10, 14, 10).Gap(4).AlignItems(ui.Stretch).Clip().Children(func() {
		// Window drag / compact traffic-light clearance
		ui.Box(c).FillWidth().Height(topPad).Shrink(0).DragWindow()

		// Expand / collapse toggle button aligned directly over rail icons
		toggleIcon, toggleTip := "layout-sidebar-left-expand", "展开导航"
		if locales.Sessions() != "会话" {
			toggleTip = "Expand navigation"
		}
		if a.railExpanded {
			toggleIcon, toggleTip = "layout-sidebar-left-collapse", "收起导航"
			if locales.Sessions() != "会话" {
				toggleTip = "Collapse navigation"
			}
		}
		ui.Row(c).FillWidth().Height(40).Margin(0, 0, 6, 0).Padding(0, 8).AlignItems(ui.Center).Children(func() {
			toggle := ui.ButtonBase(c.Key("rail-toggle")).Size(40, 40).Radius(12).Center().
				Label(toggleTip).Tooltip(toggleTip)
			m3Focus(toggle, p)
			m3State(toggle, p, ui.Transparent, false)
			toggle.Children(func() { sym(c, toggleIcon, 21, p.OnSurfaceVariant) })
			if toggle.Clicked() {
				a.railExpanded = !a.railExpanded
			}
		})

		// New Session action row
		a.railRow(c, "rail-fab", "plus", locales.NewSession(), locales.NewSession()+"  ⌘N", false, showLabels, labelOp, func() {
			a.setNavPage("sessions")
			a.do(a.C.NewSession())
		})

		// Primary destinations
		a.railRow(c, "rail:sessions", "message", locales.Sessions(), locales.Sessions(), active == "sessions", showLabels, labelOp, func() {
			a.setNavPage("sessions")
		})
		a.railRow(c, "rail:automations", "clock-play", locales.Automations(), locales.Automations(), active == "automations", showLabels, labelOp, func() {
			a.setNavPage("automations")
		})
		a.railRow(c, "rail:mcp", "puzzle", locales.Extensions(), locales.Extensions(), active == "mcp", showLabels, labelOp, func() {
			a.setNavPage("mcp")
		})

		ui.Spacer(c)

		// Settings destination + subtle status only when connecting/disconnected
		a.railRow(c, "rail:settings", "settings", locales.Settings(), locales.Settings(), active == "settings", showLabels, labelOp, func() {
			a.setNavPage("settings")
		})

		if a.C.Phase != client.Ready {
			statusColor := p.Warning
			statusText := locales.Connecting()
			if a.C.Phase == client.Disconnected {
				statusColor = p.Error
				statusText = locales.Reconnect()
			}
			ui.Row(c.Key("rail-status")).FillWidth().Height(28).Padding(0, 8).Gap(8).AlignItems(ui.Center).
				Label(statusText).Tooltip(statusText).Children(func() {
				ui.Box(c).Size(40, 28).Center().Shrink(0).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(statusColor)
				})
				if showLabels {
					ui.Text(c, statusText).FontSize(12.5).FontWeight(500).TextColor(p.OnSurfaceVariant).
						SingleLine().Opacity(labelOp)
				}
			})
		}
	})
}

func (a *App) railRow(c *ui.Context, key, symbol, label, tip string, active, showLabels bool, labelOp float32, onClick func()) {
	p := a.p
	btn := ui.ButtonBase(c.Key(key)).Row().FillWidth().Height(44).Radius(12).
		AlignItems(ui.Center).Justify(ui.Start).Clip().Label(label).Tooltip(tip)
	m3Focus(btn, p)
	base := ui.Transparent
	if active {
		base = p.SurfaceContainerHighest
		if p.Dark {
			base = p.SurfaceContainerHigh
		}
	}
	m3State(btn, p, base, false)
	if btn.Clicked() {
		onClick()
	}
	btn.Children(func() {
		if active {
			ui.Box(c).Absolute().Left(0).Top(13).Size(3.5, 18).Radius(1.75).Background(p.Primary)
		}
		icCol, fg, weight := p.OnSurfaceVariant, p.OnSurfaceVariant, 450
		if active {
			icCol, fg, weight = p.OnSurface, p.OnSurface, 600
		} else if btn.Hovered() {
			icCol, fg = p.OnSurface, p.OnSurface
		}
		ui.Box(c).Margin(0, 0, 0, 8).Size(40, 44).Center().Shrink(0).Children(func() {
			sym(c, symbol, 22, icCol)
		})
		if showLabels {
			ui.Text(c, label).FontSize(14).FontWeight(weight).TextColor(fg).
				Padding(0, 10, 0, 2).SingleLine().Ellipsis("…").MinWidth(0).Opacity(labelOp)
		}
	})
}
