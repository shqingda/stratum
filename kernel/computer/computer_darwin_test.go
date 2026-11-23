package computer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScriptsCompile compiles each action's script, which posts no event.
func TestScriptsCompile(t *testing.T) {
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("no osacompile")
	}
	for name, body := range map[string]string{"click": clickJS, "drag": dragJS, "type": typeJS, "scroll": scrollJS, "key": `key(36, 0, true);`} {
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

// TestSizeReadsTheDisplay runs the one script that only reads.
func TestSizeReadsTheDisplay(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("no display")
	}
	w, h, err := (&mac{}).Size(context.Background())
	if err != nil {
		t.Skip(err)
	}
	if w < 100 || h < 100 {
		t.Errorf("the main display is %v×%v", w, h)
	}
}
