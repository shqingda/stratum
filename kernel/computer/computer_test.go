package computer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
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

func TestEnabledOnlyWithTheEnvironment(t *testing.T) {
	t.Setenv("STRATUM_COMPUTER_USE", "")
	if Enabled() {
		t.Error("computer use is offered without STRATUM_COMPUTER_USE")
	}
	t.Setenv("STRATUM_COMPUTER_USE", "1")
	if !Enabled() {
		t.Error("STRATUM_COMPUTER_USE=1 does not offer computer use")
	}
}

func TestRedactScreenshot(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 220, B: 240, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	orig := buf.Bytes()

	allowedApp := App{Name: "Notes", ID: "com.apple.Notes"}
	secretApp := App{Name: "Mail", ID: "com.apple.mail"}
	allowNotes := func(a App) bool { return a.ID == allowedApp.ID }

	// All windows allowed -> returns original bytes untouched.
	out, err := RedactScreenshot(orig, Display{W: 100, H: 100}, 1, []Window{{App: allowedApp, X: 0, Y: 0, W: 50, H: 50}}, allowNotes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, orig) {
		t.Fatal("expected original JPEG bytes when all windows are allowed")
	}

	// Notes (front, 20..60) overlaps Mail (back, 0..80):
	// (10,10) is Mail-only -> redacted dark; (40,40) is Notes in front -> stays bright.
	wins := []Window{
		{App: allowedApp, X: 20, Y: 20, W: 40, H: 40},
		{App: secretApp, X: 0, Y: 0, W: 80, H: 80},
	}
	redacted, err := RedactScreenshot(orig, Display{W: 100, H: 100}, 1, wins, allowNotes)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := jpeg.Decode(bytes.NewReader(redacted))
	if err != nil {
		t.Fatal(err)
	}
	r1, _, _, _ := dec.At(10, 10).RGBA()
	if r1>>8 > 50 {
		t.Fatalf("expected unauthorized window pixel (10,10) to be redacted dark, got R=%d", r1>>8)
	}
	r2, _, _, _ := dec.At(40, 40).RGBA()
	if r2>>8 < 150 {
		t.Fatalf("expected front authorized window pixel (40,40) to remain bright, got R=%d", r2>>8)
	}
}
