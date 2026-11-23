package computer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseKey(t *testing.T) {
	for in, want := range map[string]Key{
		"return":       {Name: "return"},
		"Enter":        {Name: "return"},
		"cmd+shift+t":  {Name: "t", Cmd: true, Shift: true},
		"ctrl+alt+esc": {Name: "escape", Control: true, Option: true},
		"cmd+space":    {Name: "space", Cmd: true},
		"shift+tab":    {Name: "tab", Shift: true},
	} {
		got, err := ParseKey(in)
		if err != nil || got != want {
			t.Errorf("ParseKey(%q) = %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"hyper+a", "cmd+nokey", ""} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) took it", bad)
		}
	}
}

func TestScaleFitsTheScreenshot(t *testing.T) {
	s, w, h := Scale(1512, 982)
	if w > MaxWidth || h > MaxHeight || w != 1232 || h != 800 || s >= 1 {
		t.Errorf("1512×982 scales by %v to %d×%d", s, w, h)
	}
	if s, w, h := Scale(1024, 640); s != 1 || w != 1024 || h != 640 {
		t.Errorf("a small display scales by %v to %d×%d", s, w, h)
	}
}

type lacking struct {
	Desktop
	missing []string
}

func (l *lacking) Missing(context.Context) []string { return l.missing }

func TestReadyAsksForWhatIsMissing(t *testing.T) {
	d := &lacking{missing: []string{"accessibility", "screen-recording"}}
	defer Use(d)()
	defer SetGuide(nil)
	SetGuide(nil)
	if _, err := Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "accessibility and screen-recording") {
		t.Fatalf("without a guide Ready gave %v", err)
	}
	var asked []string
	SetGuide(func(_ context.Context, m []string) error { asked = m; d.missing = nil; return nil })
	if got, err := Ready(context.Background()); err != nil || got != d || len(asked) != 2 {
		t.Fatalf("after the guide: %v, %v, asked %v", got, err, asked)
	}
	d.missing = []string{"accessibility"}
	SetGuide(func(context.Context, []string) error { return errors.New("closed") })
	if _, err := Ready(context.Background()); err == nil || err.Error() != "closed" {
		t.Errorf("a guide closed gave %v", err)
	}
}
