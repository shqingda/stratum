package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// archivedFor is how long archiving a session can be undone.
const archivedFor = 5 * time.Second

// sessionMenu is what a session's row offers on a right click.
func (a *App) sessionMenu(m *ui.Menu, s proto.Session) {
	if m.Item(locales.Rename()).Chosen() {
		a.rename(s, "side")
	}
	if m.Item(locales.RegenerateTitle()).Chosen() {
		a.do(a.C.Retitle(s.ID))
	}
	if a.state(s) == "unread" || a.state(s) == "fail" {
		if m.Item(locales.MarkRead()).Chosen() {
			a.send(a.C.Seen(s.ID))
		}
	} else if m.Item(locales.MarkUnread()).Chosen() {
		a.markUnread(s.ID)
	}
	pin := locales.Pin()
	if s.Pinned {
		pin = locales.Unpin()
	}
	if m.Item(pin).Chosen() {
		on := !s.Pinned
		a.do(a.C.Flag(proto.SessionFlag{SessionID: s.ID, Pinned: &on}))
	}
	if m.Item(locales.ForkSession()).Chosen() {
		a.do(a.C.Fork(s.ID))
	}
	if a.C.Current != nil && a.C.Current.ID == s.ID && !a.C.Busy && (a.C.Workspace.Branch != "" || s.Worktree) {
		wt := !s.Worktree
		lbl := locales.HandoffToWorktree()
		if !wt {
			lbl = locales.HandoffToLocal()
		}
		if m.Item(lbl).Chosen() {
			a.do(a.C.Update(proto.SessionUpdate{Worktree: &wt}))
		}
	}
	m.Separator()
	if s.Archived {
		if m.Item(locales.Unarchive()).Chosen() {
			a.archive(s, false)
		}
	} else if m.Item(locales.Archive()).Chosen() {
		a.archive(s, true)
	}
	if m.Item(locales.DeleteSession()).Chosen() {
		a.deleting = s.ID
	}
}

func (a *App) markUnread(id string) {
	mark := "unread"
	if cur := a.C.Current; cur != nil && cur.ID == id {
		a.keptUnread = id // looking at it does not read it again
	}
	a.C.Marks[id] = mark
	a.do(a.C.Flag(proto.SessionFlag{SessionID: id, Mark: &mark}))
}

// archive puts a session away, or takes it back; put away, it can be
// undone for a few seconds.
func (a *App) archive(s proto.Session, on bool) {
	cmds, err := a.C.Flag(proto.SessionFlag{SessionID: s.ID, Archived: &on})
	if a.do(cmds, err); err == nil && on {
		a.undone = undo{id: s.ID, title: title(s), at: a.now()}
	}
}

// undo is the archiving that can still be undone.
type undo struct {
	id, title string
	at        time.Time
}

// archivedToast offers to undo the archiving just done, above the footer.
func (a *App) archivedToast(c *ui.Context) {
	u := a.undone
	left := archivedFor - a.now().Sub(u.at)
	if u.id == "" || left <= 0 {
		a.undone = undo{}
		return
	}
	c.After(left)
	p := a.p
	ui.Row(c).Margin(0, 10, 8, 10).Height(38).Padding(0, 8, 0, 12).Gap(8).Radius(12).
		Background(p.InverseSurface).AlignItems(ui.Center).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8, Opacity: 0}, Exit: &ui.Motion{Y: 8, Opacity: 0}}).Children(func() {
		sym(c, "archive", 14, p.InverseOnSurface)
		ui.Text(c, locales.ArchivedSession(u.title)).FontSize(13).TextColor(p.InverseOnSurface).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		undoBtn := ui.ButtonBase(c.Key("undo-archive")).Height(26).Padding(0, 10).Radius(8).Center().
			Label(locales.Undo()).Tooltip(locales.Undo() + "  ⌘Z")
		m3Focus(undoBtn, p)
		undoBtn.Children(func() {
			ui.Text(c, locales.Undo()).FontSize(12.5).FontWeight(600).TextColor(p.InversePrimary).SingleLine()
		})
		if undoBtn.Clicked() {
			a.undoArchive()
		}
	})
}

// undoArchive takes back the archiving just done, from the toast's button
// or ⌘Z, as Mail takes back a message moved away.
func (a *App) undoArchive() bool {
	u := a.undone
	if u.id == "" || a.now().Sub(u.at) >= archivedFor {
		return false
	}
	off := false
	a.do(a.C.Flag(proto.SessionFlag{SessionID: u.id, Archived: &off}))
	a.undone = undo{}
	return true
}

// flash shows a brief Apple-style HUD pill at the top center for keyboard or
// slash actions that otherwise have no visible confirmation.
func (a *App) flash(icon, text string) {
	a.toast.icon = icon
	a.toast.text = text
	a.toast.at = a.now()
	a.toast.said = false
}

const toastFor = 1800 * time.Millisecond

func (a *App) toastHUD(c *ui.Context) {
	t := a.toast
	left := toastFor - a.now().Sub(t.at)
	if t.text == "" || left <= 0 {
		a.toast.text = ""
		return
	}
	c.After(left)
	if !a.toast.said {
		// What the pill shows, VoiceOver says, as macOS's HUDs do.
		c.Announce(t.text)
		a.toast.said = true
	}
	p := a.p
	ui.Row(c).Absolute().Top(barHeight + 14).Left(0).Right(0).Justify(ui.Center).PassThrough().ZIndex(40).Children(func() {
		pill := ui.Row(c.Key("toast-hud")).Height(36).Padding(0, 16).Gap(8).Radius(12).
			Background(p.InverseSurface).AlignItems(ui.Center).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Opacity: 0}, Exit: &ui.Motion{Y: -4, Opacity: 0}})
		elevation2(pill, p).Children(func() {
			if t.icon != "" {
				sym(c, t.icon, 14, p.InversePrimary)
			}
			ui.Text(c, t.text).FontSize(13).FontWeight(500).TextColor(p.InverseOnSurface)
		})
	})
}

// deleteDialog asks before a session is deleted, which cannot be undone.
func (a *App) deleteDialog(c *ui.Context) {
	if len(a.removing) > 0 {
		a.removeProjectDialog(c)
		return
	}
	open := a.deleting != ""
	if !open {
		return
	}
	var s proto.Session
	for _, x := range a.C.Sessions {
		if x.ID == a.deleting {
			s = x
		}
	}
	p := a.p
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.Scrim)
		panel.Width(400).Padding(24).Radius(28).Background(p.SurfaceContainerHigh).Label(locales.DeleteTitle(title(s)))
		elevation3(panel, p).Children(func() {
			ui.Text(c, locales.DeleteTitle(title(s))).FontSize(18).FontWeight(600).TextColor(p.OnSurface)
			ui.Text(c, locales.DeleteBody()).FontSize(14).LineHeight(1.5).TextColor(p.OnSurfaceVariant).Margin(10, 0, 20, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, "delete-cancel", locales.Cancel(), "esc", false).AutoFocus().Clicked() {
					a.deleting = ""
				}
				if a.dangerButton(c, "delete-ok", locales.Delete(), "").Clicked() {
					a.do(a.C.Delete(s.ID))
					a.deleting = ""
				}
			})
		})
	})
	if !open {
		a.deleting = ""
	}
}

// removeProjectDialog asks before a project's sessions are all deleted.
func (a *App) removeProjectDialog(c *ui.Context) {
	open := true
	p := a.p
	n := len(a.removing)
	head := locales.RemoveProjectTitle(a.removingName)
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.Scrim)
		panel.Width(400).Padding(24).Radius(28).Background(p.SurfaceContainerHigh).Label(head)
		elevation3(panel, p).Children(func() {
			ui.Text(c, head).FontSize(18).FontWeight(600).TextColor(p.OnSurface)
			ui.Text(c, locales.RemoveProjectBody(n)).FontSize(14).LineHeight(1.5).TextColor(p.OnSurfaceVariant).Margin(10, 0, 20, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, "remove-cancel", locales.Cancel(), "esc", false).AutoFocus().Clicked() {
					open = false
				}
				if a.dangerButton(c, "remove-ok", locales.RemoveProject(), "").Clicked() {
					for _, id := range a.removing {
						a.do(a.C.Delete(id))
					}
					open = false
				}
			})
		})
	})
	if !open {
		a.removing, a.removingName = nil, ""
	}
}

// rename starts giving a session a title, in the sidebar or the bar.
func (a *App) rename(s proto.Session, in string) {
	a.renaming, a.renameIn, a.renameDraft = s.ID, in, title(s)
	a.renameFocus = true
}

// renameInput is a session's title being written: Enter keeps it, Esc
// does not.
func (a *App) renameInput(c *ui.Context, s proto.Session) ui.Element {
	p := a.p
	in := ui.TextInputBase(c, &a.renameDraft).Bind(&a.renameField).Label(locales.Rename()).TextColor(p.OnSurface).Grow(1).MinWidth(0).SingleLine().
		OnTextKey(func(k ui.TextKey) bool {
			a.renameDraft = k.Text()
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				a.commitRename()
				return true
			}
			return false
		})
	if a.renameFocus {
		a.renameField.Focus(c)
		in.SetTextSelection(0, len([]rune(a.renameDraft)))
		a.renameFocus = false
	} else if !a.renameField.Focused(c) {
		a.commitRename() // clicking away keeps what was written
	}
	return in
}

func (a *App) commitRename() {
	id, t := a.renaming, a.renameDraft
	a.renaming = ""
	for _, s := range a.C.Sessions {
		if s.ID == id && t != "" && t != title(s) {
			a.do(a.C.Rename(id, t))
		}
	}
}

// nextWaiting opens the next session that waits on the user, after the
// one shown, in the sidebar's order.
func (a *App) nextWaiting() {
	cur := ""
	if a.C.Current != nil {
		cur = a.C.Current.ID
	}
	ids := a.visibleSessions
	start := 0
	for i, id := range ids {
		if id == cur {
			start = i + 1
		}
	}
	for k := range ids {
		id := ids[(start+k)%len(ids)]
		if id == cur {
			continue
		}
		for _, s := range a.C.Sessions {
			if s.ID == id && a.state(s) == "wait" {
				a.do(a.C.Open(id))
				return
			}
		}
	}
}

// markSeen clears the mark of the session the user is looking at.
func (a *App) markSeen(c *ui.Context) {
	cur := a.C.Current
	if cur == nil {
		a.keptUnread = ""
		return
	}
	if a.keptUnread != "" && a.keptUnread != cur.ID {
		a.keptUnread = ""
	}
	if c.WindowActive() && !a.C.Busy && len(a.C.Approvals) == 0 && a.keptUnread != cur.ID {
		a.send(a.C.Seen(cur.ID))
	}
}

func (a *App) sessionMarkdown() string {
	var b strings.Builder
	if cur := a.C.Current; cur != nil {
		fmt.Fprintf(&b, "# %s\n\n", title(*cur))
	}
	for _, e := range a.C.Entries {
		switch e.Role {
		case client.User:
			if e.Text != "" {
				fmt.Fprintf(&b, "## User\n\n%s\n\n", e.Text)
			}
		case client.Assistant:
			if e.Text != "" {
				fmt.Fprintf(&b, "## Assistant\n\n%s\n\n", e.Text)
			}
		case client.Tool:
			line := e.Text
			if e.Detail != "" {
				line += " " + e.Detail
			}
			fmt.Fprintf(&b, "- `%s`\n", line)
		case client.Notice:
			if e.Text != "" {
				fmt.Fprintf(&b, "> %s\n\n", e.Text)
			}
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}
