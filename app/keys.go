package app

import (
	"strings"
	"time"

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
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() {
		if a.C.Current != nil && len(a.C.Entries) > 0 {
			a.openFind(c)
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyJ, func() {
		if !c.ShortcutRepeated() {
			a.nextWaiting()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen {
			a.openSettings()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyBackslash, func() {
		if !c.ShortcutRepeated() {
			a.toggleSide()
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
	if key == ui.KeyEscape && mods == 0 && (a.renaming != "" || a.deleting != "") {
		a.renaming, a.deleting = "", ""
		return true
	}
	if a.editing != "" {
		// The command being changed has Enter; Esc puts it back.
		if key == ui.KeyEscape && mods == 0 {
			a.editing = ""
		}
		return key == ui.KeyEscape || key == ui.KeyEnter
	}
	if a.rv.writing != "" && (key == ui.KeyEscape || key == ui.KeyEnter) {
		// The remark being written has Enter; Esc drops it.
		if key == ui.KeyEscape && mods == 0 {
			a.rv.writing = ""
		}
		return true
	}
	if a.find.on && key == ui.KeyEscape && mods == 0 {
		a.closeFind()
		return true
	}
	if key == ui.KeyEscape && mods == 0 && a.rewind.open {
		a.escAt = time.Time{}
		if a.rewind.step == "actions" && !a.rewind.bubble {
			a.rewind.step, a.rewind.sel = "prompts", 0
		} else {
			a.rewind = rewind{}
		}
		return true
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
	// Words written while a call waits decline it and say why.
	if key == ui.KeyEnter && mods == 0 && a.denyWithWords() {
		if ap := a.C.Approvals[0]; !a.C.Answering[ap.ID] && !a.keyRepeat {
			a.deny(ap.ID)
		}
		return true
	}
	// Return sends the draft, queued while a turn runs; ⌘Return steers the
	// running turn: the words join it after its current tool calls. The
	// setting swaps the two while a turn runs.
	if key == ui.KeyEnter && a.ready() && (mods == 0 || mods == ui.Cmd && len(a.C.Approvals) == 0) {
		steer := mods == ui.Cmd
		if a.C.Busy && a.settings().EnterSteers && len(a.C.Approvals) == 0 {
			steer = !steer
		}
		if _, cmd := a.command(); cmd {
			steer = false
		}
		if !a.keyRepeat {
			if steer {
				a.steer()
			} else {
				a.submit()
			}
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
		if ap.Rule == nil {
			always = false // nothing to keep: it is allowed once
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
	if key == ui.KeyEscape && mods == 0 && a.doubleEsc() {
		return true
	}
	return key == ui.KeyEnter && mods == 0
}

func (a *App) composerKey(k ui.TextKey) bool {
	text := k.Text()
	prev := a.draft
	a.draft = text
	if a.rewind.open {
		a.draft = prev
		if a.rewindKey(k) && a.draft != prev {
			k.SetText(a.draft)
		} else {
			k.SetText(prev)
		}
		return true
	}
	if a.mentionKey(k) || a.slashKey(k) {
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

// doubleEsc opens rewind on a second Esc in an empty composer, as Claude
// Code does. The first Esc is left for whatever else handles it.
func (a *App) doubleEsc() bool {
	if a.rewind.open || a.pal || a.C.Busy {
		return false
	}
	if strings.TrimSpace(a.draft) != "" || len(a.rewindPrompts()) == 0 {
		a.escAt = time.Time{}
		return false
	}
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	if !a.escAt.IsZero() && now.Sub(a.escAt) < 400*time.Millisecond {
		a.escAt = time.Time{}
		a.openRewind()
		return true
	}
	a.escAt = now
	return false
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
