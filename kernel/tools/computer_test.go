package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/perm"
)

// fakeDesk is a 1512×982 display that records what is done to it.
type fakeDesk struct {
	did     []string
	missing []string
}

func (f *fakeDesk) Size(context.Context) (float64, float64, error) { return 1512, 982, nil }
func (f *fakeDesk) Missing(context.Context) []string               { return f.missing }
func (f *fakeDesk) Screenshot(_ context.Context, w, h int) ([]byte, error) {
	f.did = append(f.did, fmt.Sprintf("shot %dx%d", w, h))
	return []byte("jpeg"), nil
}
func (f *fakeDesk) Click(_ context.Context, x, y float64, b string, n int) error {
	f.did = append(f.did, fmt.Sprintf("click %.0f,%.0f %s %d", x, y, b, n))
	return nil
}
func (f *fakeDesk) Move(_ context.Context, x, y float64) error {
	f.did = append(f.did, fmt.Sprintf("move %.0f,%.0f", x, y))
	return nil
}
func (f *fakeDesk) Drag(_ context.Context, x, y, tx, ty float64) error {
	f.did = append(f.did, fmt.Sprintf("drag %.0f,%.0f %.0f,%.0f", x, y, tx, ty))
	return nil
}
func (f *fakeDesk) Type(_ context.Context, s string) error {
	f.did = append(f.did, "type "+s)
	return nil
}
func (f *fakeDesk) Key(_ context.Context, k computer.Key) error {
	f.did = append(f.did, fmt.Sprintf("key %s cmd=%v", k.Name, k.Cmd))
	return nil
}
func (f *fakeDesk) Scroll(_ context.Context, x, y float64, dx, dy int) error {
	f.did = append(f.did, fmt.Sprintf("scroll %.0f,%.0f %d %d", x, y, dx, dy))
	return nil
}
func (f *fakeDesk) Open(_ context.Context, app string) error {
	f.did = append(f.did, "open "+app)
	return nil
}

func TestComputerActsAtTheScreenshotsPixels(t *testing.T) {
	d := &fakeDesk{}
	defer computer.Use(d)()
	defer func(old time.Duration) { computerSettle = old }(computerSettle)
	computerSettle = 0
	env := Env{Root: t.TempDir()}
	run := func(args string) (string, int) {
		t.Helper()
		out, imgs, err := Computer.RunImages(context.Background(), env, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		return out, len(imgs)
	}
	// 1512×982 points show as 1232×800: a pixel is 1512/1232 points.
	out, n := run(`{"action":"screenshot"}`)
	if n != 1 || !strings.Contains(out, "1232×800") {
		t.Errorf("screenshot: %q with %d images", out, n)
	}
	run(`{"action":"double_click","x":616,"y":400}`)
	run(`{"action":"right_click","x":0,"y":0}`)
	run(`{"action":"key","keys":"cmd+c"}`)
	run(`{"action":"type","text":"你好"}`)
	run(`{"action":"scroll","x":616,"y":400,"direction":"up","amount":3}`)
	run(`{"action":"drag","x":0,"y":0,"to_x":1232,"to_y":800}`)
	run(`{"action":"open_app","app":"Notes"}`)
	want := []string{
		"shot 1232x800",
		"click 756,491 left 2", "shot 1232x800",
		"click 0,0 right 1", "shot 1232x800",
		"key c cmd=true", "shot 1232x800",
		"type 你好", "shot 1232x800",
		"scroll 756,491 0 -3", "shot 1232x800",
		"drag 0,0 1512,982", "shot 1232x800",
		"open Notes", "shot 1232x800",
	}
	if strings.Join(d.did, "\n") != strings.Join(want, "\n") {
		t.Errorf("did:\n%s\nwant:\n%s", strings.Join(d.did, "\n"), strings.Join(want, "\n"))
	}
	if _, _, err := Computer.RunImages(context.Background(), env, json.RawMessage(`{"action":"click","x":2000,"y":10}`)); err == nil || !strings.Contains(err.Error(), "outside the 1232×800 screenshot") {
		t.Errorf("a click off the screenshot gave %v", err)
	}
	if _, _, err := Computer.RunImages(context.Background(), env, json.RawMessage(`{"action":"key","keys":"hyper+q"}`)); err == nil {
		t.Error("an unknown modifier ran")
	}
	d.missing = []string{"screen-recording"}
	if _, _, err := Computer.RunImages(context.Background(), env, json.RawMessage(`{"action":"screenshot"}`)); err == nil || !strings.Contains(err.Error(), "screen-recording") {
		t.Errorf("without Screen Recording: %v", err)
	}
}

func TestComputerAlwaysAsks(t *testing.T) {
	req, err := Computer.Access(Env{}, json.RawMessage(`{"action":"type","text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Kind != perm.Computer {
		t.Errorf("access: %+v", req)
	}
	for _, mode := range []perm.Mode{perm.Ask, perm.Auto} {
		if got := perm.Decide(mode, req, true); got != perm.AskUser {
			t.Errorf("in %s computer use is %s", mode, got)
		}
	}
	if got := perm.Decide(perm.Full, req, true); got != perm.Allow {
		t.Errorf("in full access computer use is %s", got)
	}
}
