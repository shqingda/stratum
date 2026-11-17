package app

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/locales"
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

// barButton is a button of the top bar, as the mockup draws them.
func (a *App) barButton(c *ui.Context, key, symbol, label string, children func()) ui.Element {
	p := a.p
	b := ui.ButtonBase(c.Key(key)).Row().Height(28).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).
		Label(label).Background(p.Card).Border(.5, p.Edge2)
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

// openButton opens the session's folder in another app.
func (a *App) openButton(c *ui.Context) {
	p := a.p
	dir := a.C.Current.Cwd
	a.barButton(c, "open", "brand-vscode", locales.Open(), func() {
		ui.Box(c).Width(.5).AlignSelf(ui.Stretch).Margin(6, 0, 6, 4).Background(p.Edge2)
		sym(c, "chevron-down", 13, p.Fg2)
	}).PopoverMenuButton(func(m *ui.Menu) {
		for _, o := range openers() {
			if m.Item(o.label).ID("open:" + o.label).Chosen() {
				if err := a.openIn(o.app, dir); err != nil {
					a.err = locales.CouldntOpen(o.label, err.Error())
				}
			}
		}
	}, a.menuOptions(&a.openMenu))
}

// commitButton shows what is not committed and opens the review panel
// at the commit message.
func (a *App) commitButton(c *ui.Context) {
	w := a.C.Workspace
	if !w.Dirty {
		return
	}
	p := a.p
	b := a.barButton(c, "commit", "git-commit", locales.Commit(), func() {
		if w.Added > 0 {
			ui.Text(c, fmt.Sprintf("+%d", w.Added)).Font("monospace").FontSize(13).TextColor(p.Plus)
		}
		if w.Removed > 0 {
			ui.Text(c, fmt.Sprintf("−%d", w.Removed)).Font("monospace").FontSize(13).TextColor(p.Minus)
		}
	})
	if b.Clicked() {
		a.rv.open, a.rv.tab = true, ""
		a.commitInput.Focus(c)
	}
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
