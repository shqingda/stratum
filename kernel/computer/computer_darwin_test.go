package computer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScriptsCompile compiles each action's script, which posts no event.
func TestScriptsCompile(t *testing.T) {
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("no osacompile")
	}
	for name, body := range map[string]string{"click": clickJS, "drag": dragJS, "type": typeJS, "scroll": scrollJS, "key": `key(36, 0, true);`,
		"displays": displaysJS, "find": findAppJS, "at": appAtJS} {
		script, err := jxaScript(map[string]any{"x": 1, "text": "中文 \"q\"\n"}, body)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), name+".scpt")
		if b, err := exec.Command("osacompile", "-l", "JavaScript", "-o", out, "-e", script).CombinedOutput(); err != nil {
			t.Errorf("%s does not compile: %s", name, b)
		}
	}
}

// TestReadsTheDesktop runs the scripts that only read: the displays and
// the apps, none of the screen's pixels.
func TestReadsTheDesktop(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("no display")
	}
	m, ctx := &mac{}, context.Background()
	ds, err := m.Displays(ctx)
	if err != nil {
		t.Skip(err)
	}
	if !ds[0].Main || ds[0].W < 100 || ds[0].H < 100 || ds[0].Name == "" {
		t.Errorf("the displays: %+v", ds)
	}
	if a, err := m.FindApp(ctx, "finder"); err != nil || a.ID != "com.apple.finder" || a.Self {
		t.Errorf("Finder is %+v, %v", a, err)
	}
	if _, err := m.FindApp(ctx, "No Such App 9f3"); err == nil || !strings.Contains(err.Error(), "no app named") {
		t.Errorf("a missing app gave %v", err)
	}
	if a, err := m.FrontApp(ctx); err != nil || a.Name == "" {
		t.Errorf("the app in front is %+v, %v", a, err)
	}
	if bar, err := m.AppAt(ctx, ds[0].X+ds[0].W/2, ds[0].Y+3); err != nil || bar.Name == "" {
		t.Errorf("the top of the display is %+v, %v", bar, err)
	}
}
