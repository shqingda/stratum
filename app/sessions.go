package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
	"github.com/shqingda/talos/proto"
)

// archivedFor is how long archiving a session can be undone.
const archivedFor = 5 * time.Second

// sessionMenu is what a session's row offers on a right click.
func (a *App) sessionMenu(m *ui.Menu, s proto.Session) {
	if m.Item(locales.Rename()).Chosen() {
		a.rename(s, "side")
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
	ui.Row(c).Margin(0, 10, 8, 10).Height(36).Padding(0, 6, 0, 12).Gap(8).Radius(10).Background(p.Card).AlignItems(ui.Center).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8, Opacity: 0}, Exit: &ui.Motion{Y: 8, Opacity: 0}}).Children(func() {
		sym(c, "archive", 14, p.Mute)
		ui.Text(c, locales.ArchivedSession(u.title)).FontSize(13).TextColor(p.Fg2).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if a.textButton(c, "undo-archive", locales.Undo()).Clicked() {
			off := false
			a.do(a.C.Flag(proto.SessionFlag{SessionID: u.id, Archived: &off}))
			a.undone = undo{}
		}
	})
}

// flash shows a brief Apple-style HUD pill at the top center for keyboard or
// slash actions that otherwise have no visible confirmation.
func (a *App) flash(icon, text string) {
	a.toast.icon = icon
	a.toast.text = text
	a.toast.at = a.now()
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
	p := a.p
	ui.Row(c).Absolute().Top(barHeight+8).Left(0).Right(0).Justify(ui.Center).PassThrough().ZIndex(40).Children(func() {
		pill := ui.Row(c.Key("toast-hud")).Height(32).Padding(0, 12).Gap(7).Radius(16).
			Material(tintedGlass(p)).Border(.5, p.Edge2).AlignItems(ui.Center).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Opacity: 0}, Exit: &ui.Motion{Y: -4, Opacity: 0}})
		lift(pill, p).Children(func() {
			if t.icon != "" {
				sym(c, t.icon, 13, p.Fg2)
			}
			ui.Text(c, t.text).FontSize(12.5).FontWeight(500).TextColor(p.Fg)
		})
	})
}

// deleteDialog asks before a session is deleted, which cannot be undone.
func (a *App) deleteDialog(c *ui.Context) {
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
		scrim := ui.RGBA(12, 12, 14, .18)
		if p == dark {
			scrim = ui.RGBA(0, 0, 0, .42)
		}
		backdrop.Background(scrim)
		panel.Width(380).Padding(18, 18, 14, 18).Radius(14).Background(p.Card).Label(locales.DeleteTitle(title(s)))
		lift(panel, p).Children(func() {
			ui.Text(c, locales.DeleteTitle(title(s))).FontSize(15).FontWeight(600).TextColor(p.Fg)
			ui.Text(c, locales.DeleteBody()).FontSize(13.5).LineHeight(1.5).TextColor(p.Fg2).Margin(8, 0, 16, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, "delete-cancel", locales.Cancel(), "esc", false).Clicked() {
					a.deleting = ""
				}
				if a.button(c, "delete-ok", locales.Delete(), "", true).Background(p.Minus).Clicked() {
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

// rename starts giving a session a title, in the sidebar or the bar.
func (a *App) rename(s proto.Session, in string) {
	a.renaming, a.renameIn, a.renameDraft = s.ID, in, title(s)
	a.renameFocus = true
}

// renameInput is a session's title being written: Enter keeps it, Esc
// does not.
func (a *App) renameInput(c *ui.Context, s proto.Session) ui.Element {
	p := a.p
	in := ui.TextInputBase(c, &a.renameDraft).Bind(&a.renameField).Label(locales.Rename()).TextColor(p.Fg).Grow(1).MinWidth(0).SingleLine().
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
