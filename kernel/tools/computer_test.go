package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/perm"
)

var (
	notes    = computer.App{Name: "Notes", ID: "com.apple.Notes"}
	safari   = computer.App{Name: "Safari", ID: "com.apple.Safari"}
	terminal = computer.App{Name: "Terminal", ID: "com.apple.Terminal"}
	finder   = computer.App{Name: "Finder", ID: "com.apple.finder"}
	stratum  = computer.App{Name: "Stratum", ID: "dev.stratum.app", Self: true}
)

// fakeDesk is a 1512×982 main display, and a 1920×1080 one to its right
// when two is set, that records what is done to it. Every point shows
// Notes unless at says otherwise; Notes is in front unless front is set.
type fakeDesk struct {
	did     []string
	missing []string
	two     bool
	at      func(x, y float64) computer.Target
	front   computer.App
}

func (f *fakeDesk) Displays(context.Context) ([]computer.Display, error) {
	ds := []computer.Display{{ID: 1, Name: "Built-in Retina Display", W: 1512, H: 982, Main: true}}
	if f.two {
		ds = append(ds, computer.Display{ID: 7, Name: "LG UltraFine", X: 1512, W: 1920, H: 1080})
	}
	return ds, nil
}
func (f *fakeDesk) Missing(context.Context) []string { return f.missing }
func (f *fakeDesk) Screenshot(_ context.Context, d computer.Display, w, h int) ([]byte, error) {
	f.did = append(f.did, fmt.Sprintf("shot %d %dx%d", d.ID, w, h))
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
func (f *fakeDesk) Open(_ context.Context, app computer.App) error {
	f.did = append(f.did, "open "+app.ID)
	return nil
}
func (f *fakeDesk) FindApp(_ context.Context, name string) (computer.App, error) {
	for _, a := range []computer.App{notes, safari, terminal, finder, stratum, {Name: "Mail", ID: "com.apple.mail"}} {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	return computer.App{}, fmt.Errorf("computer: there is no app named %q", name)
}
func (f *fakeDesk) FrontApp(context.Context) (computer.App, error) {
	if f.front.Name == "" {
		return notes, nil
	}
	return f.front, nil
}
func (f *fakeDesk) AppAt(_ context.Context, x, y float64) (computer.Target, error) {
	if f.at != nil {
		return f.at(x, y), nil
	}
	return computer.Target{App: notes}, nil
}

// session is a session of its own for a test, forgotten at its end.
func session(t *testing.T) Env {
	t.Cleanup(func() { computer.Forget(t.Name()) })
	return Env{Root: t.TempDir(), SessionID: t.Name()}
}

func noSettle(t *testing.T) {
	old := computerSettle
	computerSettle = 0
	t.Cleanup(func() { computerSettle = old })
}

func use(t *testing.T, env Env, args string) (string, error) {
	t.Helper()
	out, _, err := Computer.RunImages(context.Background(), env, json.RawMessage(args))
	return out, err
}

func must(t *testing.T, env Env, args string) string {
	t.Helper()
	out, err := use(t, env, args)
	if err != nil {
		t.Fatalf("%s: %v", args, err)
	}
	return out
}

func TestComputerActsAtTheScreenshotsPixels(t *testing.T) {
	d := &fakeDesk{}
	defer computer.Use(d)()
	noSettle(t)
	env := session(t)
	must(t, env, `{"action":"request_access","apps":["Notes"]}`)
	// 1512×982 points show as 1232×800: a pixel is 1512/1232 points.
	out, imgs, err := Computer.RunImages(context.Background(), env, json.RawMessage(`{"action":"screenshot"}`))
	if err != nil || len(imgs) != 1 || !strings.Contains(out, "Built-in Retina Display: 1232×800") || strings.Contains(out, "Displays:") {
		t.Errorf("screenshot: %q with %d images, %v", out, len(imgs), err)
	}
	must(t, env, `{"action":"double_click","x":616,"y":400}`)
	must(t, env, `{"action":"right_click","x":0,"y":0}`)
	must(t, env, `{"action":"key","keys":"cmd+c"}`)
	must(t, env, `{"action":"type","text":"你好"}`)
	must(t, env, `{"action":"scroll","x":616,"y":400,"direction":"up","amount":3}`)
	must(t, env, `{"action":"drag","x":0,"y":0,"to_x":1232,"to_y":800}`)
	must(t, env, `{"action":"open_app","app":"notes"}`)
	want := []string{
		"shot 1 1232x800",
		"click 756,491 left 2", "shot 1 1232x800",
		"click 0,0 right 1", "shot 1 1232x800",
		"key c cmd=true", "shot 1 1232x800",
		"type 你好", "shot 1 1232x800",
		"scroll 756,491 0 -3", "shot 1 1232x800",
		"drag 0,0 1511,981", "shot 1 1232x800",
		"open com.apple.Notes", "shot 1 1232x800",
	}
	if strings.Join(d.did, "\n") != strings.Join(want, "\n") {
		t.Errorf("did:\n%s\nwant:\n%s", strings.Join(d.did, "\n"), strings.Join(want, "\n"))
	}
	if _, err := use(t, env, `{"action":"click","x":2000,"y":10}`); err == nil || !strings.Contains(err.Error(), "outside the 1232×800 screenshot") {
		t.Errorf("a click off the screenshot gave %v", err)
	}
	if _, err := use(t, env, `{"action":"key","keys":"hyper+q"}`); err == nil {
		t.Error("an unknown modifier ran")
	}
	d.missing = []string{"screen-recording"}
	if _, err := use(t, env, `{"action":"screenshot"}`); err == nil || !strings.Contains(err.Error(), "screen-recording") {
		t.Errorf("without Screen Recording: %v", err)
	}
}

func TestComputerAsksForAppsFirst(t *testing.T) {
	d := &fakeDesk{}
	defer computer.Use(d)()
	noSettle(t)
	env := session(t)
	for _, args := range []string{`{"action":"screenshot"}`, `{"action":"click","x":1,"y":1}`, `{"action":"wait","seconds":1}`} {
		if _, err := use(t, env, args); err == nil || !strings.Contains(err.Error(), "call request_access") {
			t.Errorf("%s before any grant: %v", args, err)
		}
	}
	if len(d.did) != 0 {
		t.Errorf("did %q before any grant", d.did)
	}

	req, err := Computer.Access(env, json.RawMessage(`{"action":"request_access","apps":["notes","Safari","Terminal","Notes"],"reason":"to write"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []perm.App{{Name: "Notes", Tier: "full"}, {Name: "Safari", Tier: "read"}, {Name: "Terminal", Tier: "click"}}
	if req.Kind != perm.Computer || req.Granted || fmt.Sprint(req.Apps) != fmt.Sprint(want) {
		t.Errorf("request_access asks %+v", req)
	}
	// Granting apps asks in every mode, full access too.
	for _, mode := range perm.Modes {
		if got := perm.Decide(mode, req, true); got != perm.AskUser {
			t.Errorf("in %s request_access is %s", mode, got)
		}
	}
	for _, bad := range []string{`{"action":"request_access","apps":["Stratum"]}`, `{"action":"request_access","apps":["Nope"]}`, `{"action":"request_access"}`} {
		if _, err := Computer.Access(env, json.RawMessage(bad)); err == nil {
			t.Errorf("%s asks the user", bad)
		}
	}
	// What it does in the apps granted runs unasked: the tool checks it.
	act, err := Computer.Access(env, json.RawMessage(`{"action":"type","text":"hi"}`))
	if err != nil || !act.Granted || perm.Decide(perm.Ask, act, true) != perm.Allow {
		t.Errorf("an action asks: %+v, %v", act, err)
	}

	out := must(t, env, `{"action":"request_access","apps":["Notes","Safari","Terminal"]}`)
	for _, w := range []string{"Notes: tier full", "Safari: tier read", "Terminal: tier click"} {
		if !strings.Contains(out, w) {
			t.Errorf("the grant says %q, without %q", out, w)
		}
	}
	if got := computer.Granted(env.SessionID); len(got) != 3 || got[0].Name != "Notes" {
		t.Errorf("granted %+v", got)
	}
	if got := computer.Granted("another session"); len(got) != 0 {
		t.Errorf("another session has %+v", got)
	}
}

func TestComputerKeepsToEachAppsTier(t *testing.T) {
	// Safari left, Terminal in the middle, Finder for the desktop right,
	// the menu bar on top; and Stratum in a corner.
	d := &fakeDesk{}
	d.at = func(x, y float64) computer.Target {
		switch {
		case y < 30:
			front, _ := d.FrontApp(context.Background())
			return computer.Target{App: front, MenuBar: true}
		case x < 100 && y > 900:
			return computer.Target{App: stratum}
		case x < 500:
			return computer.Target{App: safari}
		case x < 1000:
			return computer.Target{App: terminal}
		}
		return computer.Target{App: finder}
	}
	defer computer.Use(d)()
	noSettle(t)
	env := session(t)
	must(t, env, `{"action":"request_access","apps":["Notes","Safari","Terminal"]}`)
	refused := func(args, why string) {
		t.Helper()
		n := len(d.did)
		if _, err := use(t, env, args); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: %v, want %q", args, err, why)
		}
		if len(d.did) != n {
			t.Errorf("%s did %q", args, d.did[n:])
		}
	}
	// Pixels are 1512/1232 points: x 400 is in Safari, 600 in Terminal.
	refused(`{"action":"click","x":200,"y":400}`, "Safari is granted at tier read")
	refused(`{"action":"scroll","x":200,"y":400}`, "tier read")
	must(t, env, `{"action":"move","x":200,"y":400}`)
	must(t, env, `{"action":"click","x":600,"y":400}`)
	must(t, env, `{"action":"scroll","x":600,"y":400}`)
	refused(`{"action":"right_click","x":600,"y":400}`, "Terminal is granted at tier click")
	refused(`{"action":"drag","x":600,"y":400,"to_x":610,"to_y":400}`, "tier click")
	refused(`{"action":"click","x":1100,"y":400}`, "Finder is not granted: call request_access")
	refused(`{"action":"click","x":10,"y":790}`, "Stratum's own window")

	d.front = terminal
	refused(`{"action":"type","text":"rm -rf"}`, "Terminal is granted at tier click")
	refused(`{"action":"key","keys":"return"}`, "tier click")
	refused(`{"action":"click","x":600,"y":10}`, "Terminal is granted at tier click") // its menu bar
	d.front = safari
	refused(`{"action":"type","text":"x"}`, "Safari is granted at tier read")
	d.front = notes
	must(t, env, `{"action":"click","x":600,"y":10}`)
	must(t, env, `{"action":"type","text":"ok"}`)
	d.front = finder
	refused(`{"action":"key","keys":"cmd+q"}`, "Finder is not granted")

	must(t, env, `{"action":"open_app","app":"Safari"}`)
	refused(`{"action":"open_app","app":"Mail"}`, "Mail is not granted")
	must(t, env, `{"action":"request_access","apps":["Finder"]}`)
	must(t, env, `{"action":"click","x":1100,"y":400}`)
}

func TestComputerSwitchesDisplays(t *testing.T) {
	d := &fakeDesk{two: true}
	defer computer.Use(d)()
	noSettle(t)
	env := session(t)
	must(t, env, `{"action":"request_access","apps":["Notes"]}`)
	out := must(t, env, `{"action":"screenshot"}`)
	if !strings.Contains(out, "Displays: 1 Built-in Retina Display (shown), 2 LG UltraFine; switch_display") {
		t.Errorf("one of two displays: %q", out)
	}
	// 1920×1080 shows as 1280×720: a pixel is 1.5 points.
	out = must(t, env, `{"action":"switch_display","display":"2"}`)
	if !strings.Contains(out, "Screenshot of LG UltraFine: 1280×720") || !strings.Contains(out, "2 LG UltraFine (shown)") {
		t.Errorf("switched: %q", out)
	}
	must(t, env, `{"action":"click","x":100,"y":100}`)
	must(t, env, `{"action":"switch_display","display":"built-in"}`)
	must(t, env, `{"action":"click","x":616,"y":400}`)
	want := []string{"shot 1 1232x800", "shot 7 1280x720", "click 1662,150 left 1", "shot 7 1280x720", "shot 1 1232x800", "click 756,491 left 1", "shot 1 1232x800"}
	if strings.Join(d.did, "\n") != strings.Join(want, "\n") {
		t.Errorf("did:\n%s\nwant:\n%s", strings.Join(d.did, "\n"), strings.Join(want, "\n"))
	}
	if _, err := use(t, env, `{"action":"switch_display","display":"3"}`); err == nil || !strings.Contains(err.Error(), "there are 1 Built-in Retina Display, 2 LG UltraFine") {
		t.Errorf("a third display: %v", err)
	}
	// The display unplugged, the main one shows.
	d.two = false
	if out := must(t, env, `{"action":"switch_display","display":"1"}`); !strings.Contains(out, "Built-in") {
		t.Errorf("after unplugging: %q", out)
	}
}

type fakeIndicator struct {
	mu                  sync.Mutex
	shows, hides, holds int
	stop                func()
}

func (f *fakeIndicator) Show(stop func()) {
	f.mu.Lock()
	f.shows++
	f.stop = stop
	f.mu.Unlock()
}
func (f *fakeIndicator) Hide() { f.mu.Lock(); f.hides++; f.mu.Unlock() }
func (f *fakeIndicator) Hold() func() {
	f.mu.Lock()
	f.holds++
	f.mu.Unlock()
	return func() {}
}
func (f *fakeIndicator) count() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shows, f.hides, f.holds
}

func TestComputerShowsItIsInControlAndStopsOnEsc(t *testing.T) {
	d := &fakeDesk{}
	defer computer.Use(d)()
	noSettle(t)
	ind := &fakeIndicator{}
	computer.SetIndicator(ind)
	defer computer.SetIndicator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := false
	env := session(t)
	env.Stop = func() { stopped = true; cancel() }
	run := func(args string) {
		t.Helper()
		if _, _, err := Computer.RunImages(ctx, env, json.RawMessage(args)); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
	}
	run(`{"action":"request_access","apps":["Notes"]}`)
	if s, _, _ := ind.count(); s != 0 {
		t.Error("granting apps shows the indicator")
	}
	run(`{"action":"screenshot"}`)
	run(`{"action":"click","x":1,"y":1}`)
	run(`{"action":"key","keys":"esc"}`)
	if s, h, holds := ind.count(); s != 1 || h != 0 || holds != 1 {
		t.Errorf("during the turn: %d shows, %d hides, %d holds", s, h, holds)
	}
	ind.stop() // esc
	if !stopped {
		t.Error("esc did not stop the turn")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, h, _ := ind.count(); h == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the indicator stays after the turn ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
