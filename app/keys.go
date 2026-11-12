package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
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
	c.OnWindowShortcut(ui.Cmd, ui.KeyK, func() { a.searchInput.Focus(c) })
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
			if !c.ShortcutRepeated() && (!c.FocusTakesText() || key.key == ui.KeyEscape || key.mods == ui.Cmd && !a.searchInput.Focused(c)) {
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
	if key == ui.KeyEnter && mods == 0 && strings.TrimSpace(a.draft) != "" {
		if !a.keyRepeat {
			a.submit()
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
