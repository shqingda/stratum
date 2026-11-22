package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

type opener struct {
	label, icon, app string
}

var (
	candidateEditors = []opener{
		{"Cursor", "file-code", "Cursor"},
		{"VS Code", "brand-vscode", "Visual Studio Code"},
		{"VS Code Insiders", "brand-vscode", "Visual Studio Code - Insiders"},
		{"Zed", "file-code", "Zed"},
		{"Xcode", "file-code", "Xcode"},
		{"Sublime Text", "file-code", "Sublime Text"},
	}
	candidateTerminals = []opener{
		{"Terminal", "terminal-2", "Terminal"},
		{"Ghostty", "terminal-2", "Ghostty"},
		{"Warp", "terminal-2", "Warp"},
		{"iTerm2", "terminal-2", "iTerm"},
	}
	installedOnce sync.Once
	installedMap  map[string]bool
)

func detectInstalledApps() map[string]bool {
	installedOnce.Do(func() {
		installedMap = map[string]bool{}
		home, _ := os.UserHomeDir()
		roots := []string{"/Applications", "/System/Applications", "/System/Applications/Utilities"}
		if home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		check := func(appName string) bool {
			for _, r := range roots {
				if _, err := os.Stat(filepath.Join(r, appName+".app")); err == nil {
					return true
				}
			}
			return false
		}
		for _, o := range candidateEditors {
			installedMap[o.app] = check(o.app)
		}
		for _, o := range candidateTerminals {
			installedMap[o.app] = check(o.app)
		}
	})
	return installedMap
}

func (a *App) appInstalled(appName string) bool {
	if appName == "" {
		return true
	}
	if a.hasApp != nil {
		return a.hasApp(appName)
	}
	return detectInstalledApps()[appName]
}

// installedOpeners returns only the apps actually installed on this Mac,
// in order: editors, Finder, terminals.
func (a *App) installedOpeners() []opener {
	var out []opener
	for _, o := range candidateEditors {
		if a.appInstalled(o.app) {
			out = append(out, o)
		}
	}
	out = append(out, opener{locales.Finder(), "folder", ""})
	for _, o := range candidateTerminals {
		if a.appInstalled(o.app) {
			lbl := o.label
			if o.app == "Terminal" {
				lbl = locales.Terminal()
			}
			out = append(out, opener{lbl, o.icon, o.app})
		}
	}
	return out
}

func (a *App) preferredEditor() (label, appName string) {
	for _, o := range candidateEditors {
		if a.appInstalled(o.app) {
			return o.label, o.app
		}
	}
	return "", ""
}

func openIn(app, dir string) error {
	args := []string{dir}
	if app != "" {
		args = []string{"-a", app, dir}
	}
	out, err := exec.Command("open", args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if app == "" {
		// No default app claims this file type (e.g. .go without an editor):
		// fall back to default text editor (-t), then Reveal in Finder (-R).
		if exec.Command("open", "-t", dir).Run() == nil {
			return nil
		}
		if exec.Command("open", "-R", dir).Run() == nil {
			return nil
		}
	}
	return fmt.Errorf("%s", strings.TrimSpace(string(out)))
}

func openBrowser(u string) error {
	if strings.TrimSpace(u) == "" {
		return nil
	}
	return exec.Command("open", u).Start()
}

// barButton is a button of the bar or inspector, lifted slightly off the sheet.
func (a *App) barButton(c *ui.Context, key, symbol, label string, children func()) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 10).Gap(6).Radius(8).AlignItems(ui.Center).
		Label(label).Background(p.Card).Border(.5, p.Edge2)
	rim(b, p)
	press(b)
	if b.Hovered() {
		b.Background(p.Card.Mix(p.Fg, .04))
	}
	b.Children(func() {
		sym(c, symbol, 15, p.Fg2)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Fg).SingleLine()
		children()
	})
	return b
}

// workspaceCapsule groups the project switcher, branch switcher, and installed
// external openers into one compact top-bar capsule.
func (a *App) workspaceCapsule(c *ui.Context, cur *proto.Session) {
	p := a.p
	dir := cur.Cwd
	if cur.Repo != "" {
		dir = cur.Repo
	}
	label := filepath.Base(dir)
	if br := a.C.Workspace.Branch; br != "" {
		label += " · " + br
	}
	b := ui.ButtonBase(c.Key("workspace-capsule")).Row().Height(28).Padding(0, 9).Gap(5).Radius(8).AlignItems(ui.Center).
		Background(p.Chip).Shrink(1).MinWidth(0).Label(label)
	rim(b, p)
	press(b)
	if b.Hovered() {
		b.Background(p.Sel)
	}
	b.Children(func() {
		sym(c, "folder", 13, p.Mute)
		ui.Text(c, label).FontSize(13).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0)
		sym(c, "chevron-down", 11, p.Mute)
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		menuText := func(it *ui.MenuItem, text string) *ui.MenuItem {
			return it.Content(func(mc ui.MenuItemContent) {
				ui.Text(mc.Context, text).FontSize(13).TextColor(mc.Text).SingleLine()
			})
		}
		menuText(m.Submenu(locales.Project(), func(sm *ui.Menu) {
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
				name := filepath.Base(dirPath)
				if menuText(sm.Item(name).ID("proj:"+dirPath).Checked(dirPath == dir), name).Chosen() {
					if dirPath != dir {
						if a.helloShown() {
							a.moveTo(dirPath)
						} else {
							a.do(a.C.NewSessionIn(dirPath))
						}
					}
				}
			}
			if a.pickDir != nil && a.update != nil {
				sm.Separator()
				if menuText(sm.Item(locales.ChooseFolder()).ID("proj:choose"), locales.ChooseFolder()).Chosen() {
					go func() {
						if chosen := a.pickDir(); chosen != "" {
							a.update(func() {
								if a.helloShown() {
									a.moveTo(chosen)
								} else {
									a.do(a.C.NewSessionIn(chosen))
								}
							})
						}
					}()
				}
			}
		}), locales.Project())
		if len(a.C.Workspace.Branches) > 0 && !cur.Worktree && a.C.CanAct() {
			menuText(m.Submenu(locales.Branch(), func(sm *ui.Menu) {
				for _, br := range a.C.Workspace.Branches {
					branch := br
					if menuText(sm.Item(branch).ID("branch:"+branch).Checked(branch == a.C.Workspace.Branch), branch).Chosen() {
						if branch != a.C.Workspace.Branch {
							a.do(a.C.Checkout(branch))
						}
					}
				}
			}), locales.Branch())
		}
		m.Separator()
		for _, o := range a.installedOpeners() {
			lbl := locales.OpenWith(o.label)
			if menuText(m.Item(lbl).ID("open:"+o.label), lbl).Chosen() {
				if err := a.openIn(o.app, cur.Cwd); err != nil {
					a.flash("alert-circle", locales.CouldntOpen(o.label, shortOpenErr(err)))
				}
			}
		}
	}, a.menuOptions(&a.openMenu))
}

// commitField is the commit message; left empty, the session's title.
func (a *App) commitField(c *ui.Context) {
	p := a.p
	ui.TextInputBase(c, &a.commitMsg).Bind(&a.commitInput).Label(locales.CommitMessage()).Placeholder(strings.TrimSpace(a.C.Current.Title)).PlaceholderColor(p.Mute).
		FontSize(13.5).TextColor(p.Fg).SingleLine().Grow(1).MinWidth(0).Height(28).Radius(7).Background(p.Chip).Padding(0, 10)
}

func (a *App) commitGo(c *ui.Context) {
	msg := strings.TrimSpace(a.commitMsg)
	if msg == "" {
		msg = strings.TrimSpace(a.C.Current.Title)
	}
	b := a.button(c, "commit-go", locales.Commit(), "", true).Label(locales.CommitChanges())
	b.Disabled(msg == "" || a.C.Workspace.Committing || a.C.Busy)
	if b.Clicked() {
		cmds, err := a.C.Commit(msg)
		if err == nil {
			a.commitMsg = ""
		}
		a.do(cmds, err)
	}
}

func (a *App) prTitle() string {
	if a.C.Current != nil {
		if t := strings.TrimSpace(a.C.Current.Title); t != "" {
			return t
		}
	}
	return a.C.Workspace.Branch
}

// prButton shows the branch's PR and CI status in the top bar, or offers to
// create a PR once changes are committed on a feature branch.
func (a *App) prButton(c *ui.Context) {
	w := a.C.Workspace
	p := a.p
	if w.PR != nil {
		icon := "brand-github"
		tip := w.PR.Title
		checkNote := ""
		switch w.PR.Checks {
		case "pass":
			icon, checkNote = "check", locales.ChecksPass()
		case "fail":
			icon, checkNote = "alert-circle", locales.ChecksFail()
		case "pending":
			icon, checkNote = "clock", locales.ChecksPending()
		}
		if checkNote != "" {
			if tip != "" {
				tip += " · " + checkNote
			} else {
				tip = checkNote
			}
		}
		b := a.barButton(c, "pr-view", icon, locales.PrNumber(w.PR.Number), func() {})
		if tip != "" {
			b.Tooltip(tip)
		}
		if b.Clicked() && a.openURL != nil {
			if err := a.openURL(w.PR.URL); err != nil {
				a.err = err.Error()
			}
		}
		return
	}
	if w.Dirty || w.Ahead == 0 || w.Branch == "" || w.Branch == w.Base {
		return
	}
	_ = p
	b := a.barButton(c, "pr-create", "git-compare", locales.CreatePr(), func() {})
	b.Disabled(w.Pushing || a.C.Busy)
	if b.Clicked() {
		a.do(a.C.CreatePR(a.prTitle(), ""))
	}
}
