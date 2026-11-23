package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func (a *App) keys(c *ui.Context) {
	// A shell reads esc, ⏎, ⇧⇥ and ⌥ with the arrows itself: while the
	// keyboard is in the terminal, the window leaves those keys to it.
	inTerm := a.termFocused(c)
	inWeb := a.browserFocused(c)
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
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyP, func() {
		if a.pal {
			a.pal, a.query = false, ""
		} else {
			a.openPalette(c)
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyW, func() {
		if !c.ShortcutRepeated() {
			a.closeOrDismiss(c)
		}
	})
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyA, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal && a.C.Current != nil {
			s := *a.C.Current
			a.archive(s, !s.Archived)
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyL, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			if inWeb {
				a.webFocus = true
			} else {
				a.composerInput.Focus(c)
			}
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeySlash, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal && a.C.Current != nil && a.C.CanAct() {
			a.modelMenu = !a.modelMenu
		}
	})
	c.OnWindowShortcut(ui.Cmd|ui.Alt, ui.KeyC, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			a.copyLastReply(c)
		}
	})
	for _, z := range []struct {
		mods ui.Modifiers
		key  ui.Key
		step int
	}{{ui.Cmd, ui.KeyEqual, 1}, {ui.Cmd | ui.Shift, ui.KeyEqual, 1}, {ui.Cmd, ui.KeyMinus, -1}, {ui.Cmd, ui.Key0, 0}} {
		c.OnWindowShortcut(z.mods, z.key, func() {
			if !c.ShortcutRepeated() {
				a.zoomText(z.step)
			}
		})
	}
	// ⌘Z takes back an archiving while the focus is not in text, which
	// keeps ⌘Z for its own typing.
	c.OnWindowShortcut(ui.Cmd, ui.KeyZ, func() {
		if !c.ShortcutRepeated() && !c.FocusTakesText() && !inTerm {
			a.undoArchive()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyPeriod, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			a.stopTurn()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyBracketLeft, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			if inWeb {
				if b := a.activeWeb(); b != nil && b.back {
					b.page.GoBack()
				}
			} else {
				a.stepSession(-1)
			}
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyBracketRight, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			if inWeb {
				if b := a.activeWeb(); b != nil && b.forward {
					b.page.GoForward()
				}
			} else {
				a.stepSession(1)
			}
		}
	})
	if !inTerm {
		c.OnWindowShortcut(ui.Alt, ui.KeyUp, func() {
			if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal && !c.FocusTakesText() {
				a.stepSession(-1)
			}
		})
		c.OnWindowShortcut(ui.Alt, ui.KeyDown, func() {
			if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal && !c.FocusTakesText() {
				a.stepSession(1)
			}
		})
	}
	// ⌃` shows and hides the terminal, as in Claude's app and Codex; ⌘T
	// opens another shell beside it, as in Terminal.app.
	c.OnWindowShortcut(ui.Ctrl, ui.KeyBackquote, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			a.toggleTerminal()
		}
	})
	// While the browser shows without the terminal, ⌘T opens a tab in it,
	// as in Safari; ⌘R reloads its page.
	web := a.browserShown() && !a.settingsOpen && !a.automationsOpen && !a.pal
	if (a.termShown() || web) && !a.pal {
		c.OnWindowShortcut(ui.Cmd, ui.KeyT, func() {
			switch {
			case c.ShortcutRepeated():
			case inTerm:
				a.addTerminal()
			case inWeb:
				a.newWebTab("")
				a.webFocus = true
			case a.termShown():
				a.addTerminal()
			default:
				a.newWebTab("")
				a.webFocus = true
			}
		})
	}
	if b := a.activeWeb(); web && !inTerm && b != nil && b.shown != "" {
		c.OnWindowShortcut(ui.Cmd, ui.KeyR, func() {
			if !c.ShortcutRepeated() {
				b.loading, b.failed = true, ""
				b.page.Reload()
			}
		})
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() { a.findCommand(c) })
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
	c.OnWindowShortcut(ui.Cmd, ui.KeyB, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen {
			a.toggleSide()
		}
	})
	c.OnWindowShortcut(ui.Cmd|ui.Alt, ui.KeyI, func() {
		if !c.ShortcutRepeated() && !a.settingsOpen && !a.automationsOpen && !a.pal {
			a.rv.open = !a.rv.open
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyG, func() {
		if a.term.find.on && a.termShown() {
			a.termFindStep(1)
		} else if a.find.on {
			a.findStep(1)
		}
	})
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyG, func() {
		if a.term.find.on && a.termShown() {
			a.termFindStep(-1)
		} else if a.find.on {
			a.findStep(-1)
		}
	})
	if !inTerm {
		c.OnWindowShortcut(ui.Shift, ui.KeyTab, func() {
			if !c.ShortcutRepeated() && !a.pal && !a.settingsOpen && a.C.Current != nil && !a.slash.open && !a.mention.open {
				modes := modeRows()
				cur := a.C.Current.Mode
				next := modes[0]
				for i, m := range modes {
					if m.id == cur {
						next = modes[(i+1)%len(modes)]
						break
					}
				}
				a.do(a.C.Update(proto.SessionUpdate{Mode: &next.id}))
				a.flash(next.icon, next.label)
			}
		})
	}
	for i, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if i < len(a.visibleSessions) {
			id := a.visibleSessions[i]
			c.OnWindowShortcut(ui.Cmd, key, func() { a.do(a.C.Open(id)) })
		}
	}
	for _, key := range []struct {
		mods ui.Modifiers
		key  ui.Key
	}{{0, ui.KeyEnter}, {ui.Cmd, ui.KeyEnter}, {ui.Cmd | ui.Alt, ui.KeyEnter}, {0, ui.KeyEscape}} {
		if inTerm && key.mods == 0 {
			continue
		}
		c.OnWindowShortcut(key.mods, key.key, func() {
			if !a.pal && !c.ShortcutRepeated() && (!c.FocusTakesText() || key.key == ui.KeyEscape || key.mods&ui.Cmd != 0 && !a.searchInput.Focused(c)) {
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
	if key == ui.KeyEscape && mods == 0 && a.automationsOpen {
		if a.automationsUI.deletingID != "" {
			a.automationsUI.deletingID, a.automationsUI.deletingName = "", ""
		} else {
			a.automationsOpen = false
		}
		return true
	}
	if key == ui.KeyEscape && mods == 0 && (a.renaming != "" || a.deleting != "" || len(a.removing) > 0) {
		a.renaming, a.deleting, a.removing = "", "", nil
		return true
	}
	if key == ui.KeyEscape && mods == 0 && a.term.closing != "" {
		a.term.closingSession, a.term.closing, a.term.job = "", "", ""
		a.term.focus = true
		return true
	}
	if key == ui.KeyEscape && mods == 0 && a.term.find.on && a.termShown() {
		a.closeTermFind()
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
		allow, always, forever := false, false, false
		switch {
		case key == ui.KeyEnter && mods == 0:
			allow = true
		case key == ui.KeyEnter && mods == ui.Cmd:
			allow, always = true, true
		case key == ui.KeyEnter && mods == ui.Cmd|ui.Alt:
			// Option adds the stronger choice, as Option does to a Mac's
			// menu items: allowed from now on, in every session.
			allow, forever = true, true
		case key == ui.KeyEscape && mods == 0:
		default:
			return false
		}
		if ap.Rule == nil {
			always, forever = false, false // nothing to keep: it is allowed once
		}
		if !a.C.Answering[ap.ID] && !a.keyRepeat {
			if forever {
				a.do(a.C.Respond(proto.ApprovalAnswer{ApprovalID: ap.ID, Allow: true, Forever: true}))
			} else {
				a.do(a.C.Answer(ap.ID, allow, always))
			}
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
	// The editor takes Shift+Cmd+A as Cmd+A, select all; in the composer
	// it archives the session, as it does wherever else the focus is.
	if k.Key == ui.KeyA && k.Mods == ui.Cmd|ui.Shift {
		if a.C.Current != nil && !a.settingsOpen && !a.automationsOpen && !a.pal {
			s := *a.C.Current
			a.archive(s, !s.Archived)
		}
		return true
	}
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
	// Tab moves the session to the next primary agent, as in OpenCode;
	// ⇧Tab stays the permission mode's, as in Claude Code.
	if k.Key == ui.KeyTab && k.Mods == 0 && a.C.Current != nil && a.C.CanAct() {
		a.cycleAgent(1)
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

// lastAssistantReply returns the latest non-empty assistant response in the session.
func (a *App) lastAssistantReply() string {
	for i := len(a.C.Entries) - 1; i >= 0; i-- {
		if e := a.C.Entries[i]; e.Role == client.Assistant && strings.TrimSpace(e.Text) != "" {
			return e.Text
		}
	}
	return ""
}

func (a *App) copyLastReply(c *ui.Context) bool {
	if reply := a.lastAssistantReply(); reply != "" {
		c.WriteClipboard(reply)
		a.flash("check", locales.CopyLastReply())
		return true
	}
	return false
}

// stepSession switches to the previous (-1) or next (+1) visible session in the sidebar.
func (a *App) stepSession(delta int) {
	ids := a.visibleSessions
	if len(ids) < 2 {
		return
	}
	cur := ""
	if a.C.Current != nil {
		cur = a.C.Current.ID
	}
	idx := 0
	for i, id := range ids {
		if id == cur {
			idx = i
			break
		}
	}
	next := ids[(idx+delta+len(ids))%len(ids)]
	if next != cur {
		a.do(a.C.Open(next))
	}
}

// findCommand is ⌘F: it finds in the terminal while the keyboard is in
// it, as Find goes to what has the focus in Apple's apps, and in the
// conversation otherwise.
func (a *App) findCommand(c *ui.Context) {
	switch {
	case a.termFocused(c) || a.term.find.on && a.termShown():
		a.openTermFind(c)
	case a.C.Current != nil && len(a.C.Entries) > 0:
		a.openFind(c)
	}
}
