package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/proto"
)

func (a *App) syncMenus() {
	id := ""
	if a.C.Current != nil {
		id = a.C.Current.ID
	}
	if a.modelsLoadedAccount == "" && len(a.C.Models) > 0 {
		a.modelsLoadedAccount = a.modelAccount()
	}
	if a.C.Phase == client.Disconnected {
		a.modelsPending = ""
	}
	if id != a.menuSession || !a.C.CanAct() {
		a.modelMenu, a.effortMenu, a.modeMenu = false, false, false
		a.modelsRequested = false
		a.menuSession = id
	}
}

func (a *App) receive(f proto.Frame) {
	a.send(a.C.Receive(f))
	if f.Type == proto.FrameResponse && a.modelsPending != "" && f.ID == a.modelsPending {
		a.modelsPending = ""
		if f.Error != nil {
			a.modelsError = f.Error.Message
		} else {
			a.modelsLoadedAccount = a.modelsAccount
		}
	}
	if a.C.Phase == client.Disconnected {
		a.modelsPending = ""
	}
}

func (a *App) menuOptions(open *bool) ui.PopoverMenuButtonOptions {
	return ui.PopoverMenuButtonOptions{Open: open, Panel: func(panel ui.Element) {
		panel.Background(a.p.Card).Border(.5, a.p.Edge2).Radius(10)
	}}
}

func (a *App) modelAccount() string {
	if a.C.Current != nil && a.C.Current.Account != "" {
		return a.C.Current.Account
	}
	if acc := a.account(); acc != nil {
		return acc.ID
	}
	return ""
}

func (a *App) modelPill(c *ui.Context, label string) {
	if a.modelsAccount != a.modelAccount() {
		a.modelsRequested = false
	}
	a.pill(c, "model", "", label, true).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		if !a.modelsRequested {
			a.modelsRequested = true
			a.modelsAccount, a.modelsError = a.modelAccount(), ""
			cmds, err := a.C.ListModels(a.modelsAccount)
			if err == nil {
				a.modelsPending = cmds[0].ID
			} else {
				a.modelsError = err.Error()
			}
			a.do(cmds, err)
		}
		switch {
		case a.modelsPending != "":
			m.Item("加载中…").Disabled(true)
		case a.modelsError != "":
			m.Item("加载失败，请重新打开菜单").Subtitle(a.modelsError).Disabled(true)
		case len(a.C.Models) == 0:
			m.Item("这个账号没有可用模型").Disabled(true)
		default:
			for _, model := range a.C.Models {
				name := model.Name
				if name == "" {
					name = model.ID
				}
				selected := model.ID == a.C.Current.Model || a.C.Current.Model == "" && model.Default
				item := m.Item(name).ID(model.ID).Checked(selected).Trailing(contextSize(model.Context))
				if model.Default {
					item.Subtitle("✓ 默认模型")
				}
				if item.Chosen() {
					id := model.ID
					a.do(a.C.Update(proto.SessionUpdate{Model: &id}))
				}
			}
		}
	}, a.menuOptions(&a.modelMenu))
	if !a.modelMenu {
		a.modelsRequested = false
	}
}

func contextSize(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 1_000_000:
		return fmt.Sprintf("%gM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%gK", float64(n)/1_000)
	default:
		return fmt.Sprint(n)
	}
}

func (a *App) reasoningUnsupported() bool {
	if a.modelsLoadedAccount != a.modelAccount() {
		return false
	}
	for _, model := range a.C.Models {
		if model.ID == a.C.Current.Model || a.C.Current.Model == "" && model.Default {
			return !model.Reasoning
		}
	}
	return false
}

func (a *App) effortPill(c *ui.Context, label string) {
	unsupported := a.reasoningUnsupported()
	container := ui.Box(c)
	if unsupported {
		// Disabled controls do not show tooltips in mygo; their container can.
		container.Tooltip("这个模型不支持思考强度")
	}
	container.Children(func() {
		b := a.pill(c, "effort", "brain", label, false).Disabled(!a.C.CanAct() || unsupported)
		if unsupported {
			b.Description("这个模型不支持思考强度")
		}
		b.PopoverMenuButton(func(m *ui.Menu) {
			for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
				if m.Item(efforts[effort]).ID("effort:" + effort).Checked(a.C.Current.Effort == effort).Chosen() {
					a.do(a.C.Update(proto.SessionUpdate{Effort: &effort}))
				}
			}
		}, a.menuOptions(&a.effortMenu))
	})
}

func (a *App) modePill(c *ui.Context, label string) {
	a.pill(c, "mode", "shield-half", label, false).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		for _, mode := range []struct{ id, description string }{
			{"ask", "写文件和运行命令前问你"},
			{"auto", "沙箱内自动允许"},
			{"full", "不再询问，慎用"},
		} {
			if m.Item(modes[mode.id]).ID(mode.id).Subtitle(mode.description).Checked(a.C.Current.Mode == mode.id).Chosen() {
				a.do(a.C.Update(proto.SessionUpdate{Mode: &mode.id}))
			}
		}
	}, a.menuOptions(&a.modeMenu))
}

func phaseText(phase client.Phase) string {
	switch phase {
	case client.Starting:
		return "正在启动"
	case client.Handshaking:
		return "正在连接"
	case client.Syncing:
		return "正在同步"
	case client.Ready:
		return "已连接"
	case client.Disconnected:
		return "已断开连接"
	default:
		return "连接状态未知"
	}
}
