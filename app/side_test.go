package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func TestSideAskShortcutAndSubmit(t *testing.T) {
	a := task8App()
	tt := task8Tester(a, false)

	// ⌘; opens the side question dialog.
	tt.Key(ui.Cmd, ui.KeySemicolon)
	tt.Frame()
	if !a.btwOpen {
		t.Fatal("expected ⌘; to open side question dialog")
	}
	if !tt.HasText(locales.SideAskBadge()) {
		t.Fatalf("expected side question badge %q in dialog", locales.SideAskBadge())
	}

	// Typing and pressing Enter sends MethodSideAsk without touching main Prompt.
	tt.Type("why did this fail?")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	cmds, ok := next(a)
	if !ok || len(cmds) != 1 || cmds[0].Method != proto.MethodSideAsk {
		t.Fatalf("expected MethodSideAsk command, got ok=%v cmds=%+v", ok, cmds)
	}
	var p proto.SideAsk
	if err := json.Unmarshal(cmds[0].Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != a.C.Current.ID || p.Text != "why did this fail?" {
		t.Fatalf("unexpected SideAsk params: %+v", p)
	}
	if !a.C.SidePending {
		t.Fatal("expected SidePending=true while awaiting response")
	}

	// ⌘W closes the side question dialog.
	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if a.btwOpen {
		t.Fatal("expected ⌘W to close side question dialog")
	}

	// Typing "/btw <question>" in composer opens side card and sends MethodSideAsk.
	a.draft = "/btw explain this"
	a.submit()
	tt.Frame()
	if !a.btwOpen || a.draft != "" {
		t.Fatalf("expected /btw submit to open card and clear draft, got open=%v draft=%q", a.btwOpen, a.draft)
	}
	cmds, ok = next(a)
	if !ok || len(cmds) != 1 || cmds[0].Method != proto.MethodSideAsk {
		t.Fatalf("expected MethodSideAsk from /btw, got ok=%v cmds=%+v", ok, cmds)
	}

	// Non-modal: while /btw is open, the user can still interact with the main window
	// (e.g. toggling the inspector panel or submitting a main composer prompt).
	wasRV := a.rv.open
	tt.Key(ui.Cmd|ui.Alt, ui.KeyI)
	tt.Frame()
	if a.rv.open == wasRV {
		t.Fatal("expected main window shortcut ⌘⌥I to work while /btw card is open")
	}
	for {
		if _, ok := next(a); !ok {
			break
		}
	}
	if !a.btwOpen {
		t.Fatal("expected /btw card to stay open during main window interaction")
	}
	a.draft = "main task"
	a.submit()
	tt.Frame()
	cmds, ok = next(a)
	if !ok || len(cmds) != 1 || cmds[0].Method != proto.MethodPrompt {
		t.Fatalf("expected main composer Prompt while /btw is open, got ok=%v cmds=%+v", ok, cmds)
	}
}
