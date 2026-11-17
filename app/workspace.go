package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// openers are the apps the top bar opens a session's folder in.
func openers() []struct{ label, icon, app string } {
	return []struct{ label, icon, app string }{
		{"VS Code", "brand-vscode", "Visual Studio Code"},
		{locales.Finder(), "folder", ""},
		{locales.Terminal(), "terminal-2", "Terminal"},
	}
}

func openIn(app, dir string) error {
	args := []string{dir}
	if app != "" {
		args = []string{"-a", app, dir}
	}
	if out, err := exec.Command("open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
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

// workspaceCapsule groups the folder, active branch, external openers,
// and transcript detail preference into one compact top-bar capsule.
func (a *App) workspaceCapsule(c *ui.Context, cur *proto.Session) {
	p := a.p
	dir := cur.Cwd
	label := filepath.Base(dir)
	if br := a.C.Workspace.Branch; br != "" {
		label += " · " + br
	}
	b := ui.ButtonBase(c.Key("workspace-capsule")).Row().Height(26).Padding(0, 9).Gap(5).Radius(8).AlignItems(ui.Center).
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
	thought := slices.ContainsFunc(a.C.Entries, func(e *client.Entry) bool { return e.Role == client.Thinking })
	set := a.settings()
	b.PopoverMenuButton(func(m *ui.Menu) {
		if len(a.C.Workspace.Branches) > 0 && !cur.Worktree && a.C.CanAct() {
			for _, br := range a.C.Workspace.Branches {
				branch := br
				if m.Item(branch).ID("branch:" + branch).Checked(branch == a.C.Workspace.Branch).Chosen() {
					if branch != a.C.Workspace.Branch {
						a.do(a.C.Checkout(branch))
					}
				}
			}
			m.Separator()
		}
		for _, o := range openers() {
			if m.Item(locales.OpenWith(o.label)).ID("open:" + o.label).Chosen() {
				if err := a.openIn(o.app, dir); err != nil {
					a.err = locales.CouldntOpen(o.label, err.Error())
				}
			}
		}
		m.Separator()
		for _, d := range []struct {
			id   string
			name func() string
		}{{"", locales.DetailPlain}, {"thinking", locales.DetailThinking}, {"all", locales.DetailAll}} {
			if d.id == "thinking" && !thought && set.Detail != "thinking" {
				continue
			}
			if m.Item(locales.DetailTo(d.name())).ID("detail:" + d.id).Checked(set.Detail == d.id).Chosen() {
				s := a.settings()
				s.Detail = d.id
				a.do(a.C.SaveSettings(s))
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
