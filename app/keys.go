package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
)

func (a *App) keys(c *ui.Context) {
	c.Root().ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputKeyDown {
			a.keyRepeat = ev.Repeat
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() {
		if !c.ShortcutRepeated() {
			a.do(a.C.NewSession())
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyK, func() {
		if a.pal {
			a.pal, a.query = false, ""
		} else {
			a.openPalette(c)
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen {
			a.openSettings()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyBackslash, func() {
		if !c.ShortcutRepeated() {
			a.noSide = !a.noSide
		}
	})
	for i, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if i < len(a.visibleSessions) {
			id := a.visibleSessions[i]
			c.OnWindowShortcut(ui.Cmd, key, func() { a.do(a.C.Open(id)) })
		}
	}
	for _, key := range []struct {
		mods ui.Modifiers
		key  ui.Key
	}{{0, ui.KeyEnter}, {ui.Cmd, ui.KeyEnter}, {0, ui.KeyEscape}} {
		c.OnWindowShortcut(key.mods, key.key, func() {
			if !a.pal && !c.ShortcutRepeated() && (!c.FocusTakesText() || key.key == ui.KeyEscape || key.mods == ui.Cmd && !a.searchInput.Focused(c)) {
				a.keyRepeat = false
				a.actionKey(key.mods, key.key)
			}
		})
	}
}

// The oldest approval keeps its keys until answered; a repeated key never
// advances to another approval while its response is still in flight.
func (a *App) actionKey(mods ui.Modifiers, key ui.Key) bool {
	if a.modelMenu || a.effortMenu || a.modeMenu {
		return false
	}
	if m := &a.mention; m.open && key == ui.KeyEscape && mods == 0 {
		m.shut, m.open = m.at+1, false
		return true
	}
	if a.questionFirst() {
		// The composer answers the model's question: Enter sends, Esc skips.
		ap := a.C.Approvals[0]
		switch {
		case key == ui.KeyEnter && mods == 0 && strings.TrimSpace(a.draft) != "":
			if !a.C.Answering[ap.ID] && !a.keyRepeat {
				cmds, err := a.C.Reply(ap.ID, a.draft)
				if err == nil {
					a.draft = ""
				}
				a.do(cmds, err)
			}
			return true
		case key == ui.KeyEscape && mods == 0:
			if !a.C.Answering[ap.ID] && !a.keyRepeat {
				a.do(a.C.Reply(ap.ID, ""))
			}
			return true
		}
		return key == ui.KeyEnter
	}
	if key == ui.KeyEnter && mods == 0 && a.ready() {
		if !a.keyRepeat {
			a.submit()
		}
		return true
	}
	// ⌘Enter steers the running turn: the words join it after its
	// current tool calls rather than waiting for it to end.
	if key == ui.KeyEnter && mods == ui.Cmd && a.ready() && len(a.C.Approvals) == 0 {
		if !a.keyRepeat {
			a.steer()
		}
		return true
	}
	if len(a.C.Approvals) > 0 {
		ap := a.C.Approvals[0]
		allow, always := false, false
		switch {
		case key == ui.KeyEnter && mods == 0:
			allow = true
		case key == ui.KeyEnter && mods == ui.Cmd:
			allow, always = true, true
		case key == ui.KeyEscape && mods == 0:
		default:
			return false
		}
		if !a.C.Answering[ap.ID] && !a.keyRepeat {
			a.do(a.C.Answer(ap.ID, allow, always))
		}
		return true
	}
	// Enter in an empty composer sends the oldest waiting prompt now.
	if key == ui.KeyEnter && mods == 0 && a.C.Busy {
		for _, q := range a.C.Queued {
			if !q.Steer {
				if !a.keyRepeat {
					a.do(a.C.SendNow(q.ID))
				}
				return true
			}
		}
	}
	if key == ui.KeyEscape && mods == 0 && a.C.Busy {
		if !a.keyRepeat {
			a.do(a.C.Cancel())
		}
		return true
	}
	return key == ui.KeyEnter && mods == 0
}

func (a *App) composerKey(k ui.TextKey) bool {
	text := k.Text()
	a.draft = text
	if a.mentionKey(k) {
		return true
	}
	// ↑ in an empty composer brings back the last thing sent, to send
	// again or change.
	if k.Key == ui.KeyUp && k.Mods == 0 && strings.TrimSpace(text) == "" {
		if last := a.lastSent(); last != "" {
			a.recall = last // the view sets it, as the app sets a draft
			return true
		}
	}
	handled := a.actionKey(k.Mods, k.Key)
	if a.draft != text {
		k.SetText(a.draft)
	}
	return handled
}

func (a *App) approvalHint(c *ui.Context, id, hint string) string {
	if len(a.C.Approvals) == 0 || a.C.Approvals[0].ID != id || a.C.Answering[id] || !a.C.CanAct() {
		return ""
	}
	if hint != "esc" && a.searchInput.Focused(c) || hint == "⏎" && strings.TrimSpace(a.draft) != "" {
		return ""
	}
	return hint
}

// questionFirst is the model's question waiting for the user before any
// approval.
func (a *App) questionFirst() bool {
	return len(a.C.Approvals) > 0 && a.C.Approvals[0].Kind == "question"
}

// lastSent is the text of the user's last message in the session.
func (a *App) lastSent() string {
	for i := len(a.C.Entries) - 1; i >= 0; i-- {
		if e := a.C.Entries[i]; e.Role == client.User && e.Text != "" {
			return e.Text
		}
	}
	return ""
}
