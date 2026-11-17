package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
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
		a.modelMenu, a.effortMenu, a.modeMenu, a.detailMenu, a.worktreeMenu, a.branchMenu = false, false, false, false, false, false
		a.modelsRequested = false
		a.menuSession = id
	}
}

func (a *App) receive(f proto.Frame) {
	a.send(a.C.Receive(f))
	a.watch()
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
		panel.Background(a.p.Card).Border(.5, a.p.Edge2).Radius(12).NoShadow()
		lift(panel, a.p)
		rim(panel, a.p)
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
	p := a.p
	effort := effortLabel(a.C.Current.Effort)
	b := ui.ButtonBase(c.Key("pill:model")).Row().Height(28).Padding(0, 9).Gap(6).Radius(14).AlignItems(ui.Center).
		Label(label).Disabled(!a.C.CanAct())
	press(b)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(p.Fg2).SingleLine()
		ui.Text(c, effort).FontSize(13).TextColor(p.Mute).SingleLine()
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		a.modelItems(m, a.modelAccount(), a.C.Current.Model, func(id string) {
			a.do(a.C.Update(proto.SessionUpdate{Model: &id}))
		})
		m.Separator()
		unsupported := a.reasoningUnsupported()
		sub := m.Submenu(locales.ReasoningEffort(), func(sm *ui.Menu) {
			for _, ef := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
				if sm.Item(effortLabel(ef)).ID("effort:" + ef).Checked(a.C.Current.Effort == ef).Chosen() {
					e := ef
					a.do(a.C.Update(proto.SessionUpdate{Effort: &e}))
				}
			}
		}).Trailing(effort)
		if unsupported {
			sub.Disabled(true).Tooltip(locales.NoEffortSupport())
		}
	}, a.menuOptions(&a.modelMenu))
	if !a.modelMenu {
		a.modelsRequested = false
	}
}

// modelItems lists an account's models in a menu, asking for them once
// per opening, with current checked.
func (a *App) modelItems(m *ui.Menu, account, current string, choose func(id string)) {
	if !a.modelsRequested {
		a.modelsRequested = true
		a.modelsAccount, a.modelsError = account, ""
		cmds, err := a.C.ListModels(account)
		if err == nil {
			a.modelsPending = cmds[0].ID
		} else {
			a.modelsError = err.Error()
		}
		a.do(cmds, err)
	}
	switch {
	case a.modelsPending != "":
		m.Item(locales.Loading()).Disabled(true)
	case a.modelsError != "":
		m.Item(locales.ModelsLoadFailed()).Subtitle(a.modelsError).Disabled(true)
	case len(a.C.Models) == 0:
		m.Item(locales.NoModels()).Disabled(true)
	default:
		for _, model := range a.C.Models {
			name := model.Name
			if name == "" {
				name = model.ID
			}
			selected := model.ID == current || current == "" && model.Default
			item := m.Item(name).ID(model.ID).Checked(selected).Trailing(contextSize(model.Context))
			if model.Default {
				item.Subtitle(locales.DefaultModelMark())
			}
			if item.Chosen() {
				choose(model.ID)
			}
		}
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
		container.Tooltip(locales.NoEffortSupport())
	}
	container.Children(func() {
		b := a.pill(c, "effort", "", label, false).Disabled(!a.C.CanAct() || unsupported)
		if unsupported {
			b.Description(locales.NoEffortSupport())
		}
		b.PopoverMenuButton(func(m *ui.Menu) {
			for _, effort := range []string{"", "minimal", "low", "medium", "high", "xhigh"} {
				if m.Item(effortLabel(effort)).ID("effort:" + effort).Checked(a.C.Current.Effort == effort).Chosen() {
					a.do(a.C.Update(proto.SessionUpdate{Effort: &effort}))
				}
			}
		}, a.menuOptions(&a.effortMenu))
	})
}

func (a *App) modePill(c *ui.Context, label string) {
	a.pill(c, "mode", "", label, false).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		for _, mode := range []struct{ id, description string }{
			{"ask", locales.ModeAskShort()},
			{"auto", locales.ModeAutoShort()},
			{"full", locales.ModeFullShort()},
		} {
			if m.Item(modeName(mode.id)).ID(mode.id).Subtitle(mode.description).Checked(a.C.Current.Mode == mode.id).Chosen() {
				a.do(a.C.Update(proto.SessionUpdate{Mode: &mode.id}))
			}
		}
	}, a.menuOptions(&a.modeMenu))
}

func phaseText(phase client.Phase) string {
	switch phase {
	case client.Starting:
		return locales.PhaseStarting()
	case client.Handshaking:
		return locales.PhaseConnecting()
	case client.Syncing:
		return locales.PhaseSyncing()
	case client.Ready:
		return locales.PhaseReady()
	case client.Disconnected:
		return locales.PhaseDisconnected()
	default:
		return locales.PhaseUnknown()
	}
}

// folderPill moves a session not yet begun to another folder: one the
// sidebar knows, or one chosen with the open panel.
func (a *App) folderPill(c *ui.Context, cur *proto.Session) {
	curDir := cur.Cwd
	if cur.Repo != "" {
		curDir = cur.Repo
	}
	a.pill(c, "folder", "folder", filepath.Base(curDir), true).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		seen := map[string]bool{}
		for _, s := range a.C.Sessions {
			dirPath := s.Cwd
			if s.Repo != "" {
				dirPath = s.Repo
			}
			if seen[dirPath] {
				continue
			}
			seen[dirPath] = true
			dir, _ := a.splitPath(dirPath)
			if m.Item(filepath.Base(dirPath)).ID("folder:" + dirPath).Subtitle(dir).Checked(dirPath == curDir).Chosen() {
				a.moveTo(dirPath)
			}
		}
		if a.pickDir != nil && a.update != nil {
			m.Separator()
			if m.Item(locales.ChooseFolder()).ID("folder:choose").Chosen() {
				go func() {
					if dir := a.pickDir(); dir != "" {
						a.update(func() { a.moveTo(dir) })
					}
				}()
			}
		}
	}, a.menuOptions(&a.folderMenu))
}

// worktreePill toggles whether a session not yet begun runs in an isolated git worktree.
func (a *App) worktreePill(c *ui.Context, cur *proto.Session) {
	icon, label := "device-laptop", locales.Local()
	if cur.Worktree {
		icon, label = "git-branch", locales.Worktree()
	}
	a.pill(c, "worktree", icon, label, true).Disabled(!a.C.CanAct()).PopoverMenuButton(func(m *ui.Menu) {
		for _, opt := range []struct {
			on          bool
			label, desc string
		}{
			{false, locales.Local(), locales.LocalSub()},
			{true, locales.Worktree(), locales.WorktreeSub()},
		} {
			wt := opt.on
			if m.Item(opt.label).ID(fmt.Sprintf("worktree:%v", wt)).Subtitle(opt.desc).Checked(cur.Worktree == wt).Chosen() {
				if cur.Worktree != wt {
					a.do(a.C.Update(proto.SessionUpdate{Worktree: &wt}))
				}
			}
		}
	}, a.menuOptions(&a.worktreeMenu))
}

// branchPill switches the git branch of a new session before it starts.
func (a *App) branchPill(c *ui.Context, cur *proto.Session) {
	if a.C.Workspace.Branch == "" || cur.Worktree {
		return
	}
	a.pill(c, "branch", "git-branch", a.C.Workspace.Branch, true).Disabled(!a.C.CanAct() || len(a.C.Workspace.Branches) == 0).PopoverMenuButton(func(m *ui.Menu) {
		for _, br := range a.C.Workspace.Branches {
			branch := br
			if m.Item(branch).ID("branch:" + branch).Checked(branch == a.C.Workspace.Branch).Chosen() {
				if branch != a.C.Workspace.Branch {
					a.do(a.C.Checkout(branch))
				}
			}
		}
	}, a.menuOptions(&a.branchMenu))
}

// moveTo starts the session over in dir: a new one there, and the empty
// one here goes.
func (a *App) moveTo(dir string) {
	cur := a.C.Current
	if cur == nil || dir == cur.Cwd {
		return
	}
	// Both are asked while the client is ready; the new one goes first.
	var drop []proto.Command
	var dropErr error
	if len(a.C.Entries) == 0 {
		drop, dropErr = a.C.Delete(cur.ID)
	}
	cmds, err := a.C.NewSessionIn(dir)
	if a.do(cmds, err); err == nil && drop != nil {
		a.do(drop, dropErr)
	}
}

// pickFolder is the system's open panel, for one folder.
func pickFolder() string {
	paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: locales.ChooseFolder(), Directory: true, CreateDirectories: true})
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
