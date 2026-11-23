package preview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shqingda/stratum/proto"
)

// fakeDriver answers the page script's calls as a page would, and keeps
// tabs as the panel does.
type fakeDriver struct {
	url   string
	evals []string
	steps []string
	tabs  []proto.BrowserTab
	seq   int
	acted int
	body  string
}

func (f *fakeDriver) Navigate(_ context.Context, u string) error {
	if t := f.active(); t != nil {
		t.URL = u
	} else {
		f.add(u)
	}
	f.url = u
	return nil
}
func (f *fakeDriver) Step(_ context.Context, s string) error {
	f.steps = append(f.steps, s)
	return nil
}
func (f *fakeDriver) Capture(context.Context) ([]byte, error) { return []byte("png"), nil }
func (f *fakeDriver) Eval(_ context.Context, js string) (any, error) {
	f.evals = append(f.evals, js)
	switch {
	case strings.HasSuffix(js, "return document.readyState"):
		return "complete", nil
	case strings.HasSuffix(js, "document.body.innerText : ''"):
		return f.body, nil
	case strings.Contains(js, "__stratum.click(404)"):
		return nil, errors.New("mygo: javascript error: no element with ref 404")
	case strings.Contains(js, "return __stratum.network()"):
		return "GET 200 http://localhost:5173/app.js", nil
	case strings.Contains(js, "return __stratum."):
		return map[string]any{
			"url": f.url, "title": "Calc", "status": 200.0,
			"log":      []any{map[string]any{"level": "error", "text": "boom", "source": "app.js", "line": 3.0}, map[string]any{"level": "network", "text": "404 /api"}},
			"elements": `[1] button "Add"`, "text": "1 + 2 = 3",
		}, nil
	case strings.Contains(js, "__stratumLog"):
		return []any{map[string]any{"level": "log", "text": "ready"}, map[string]any{"level": "dialog", "text": "confirm: Delete? → OK"}}, nil
	}
	return 42.0, nil
}

func (f *fakeDriver) active() *proto.BrowserTab {
	for i := range f.tabs {
		if f.tabs[i].Active {
			return &f.tabs[i]
		}
	}
	return nil
}

func (f *fakeDriver) add(u string) {
	for i := range f.tabs {
		f.tabs[i].Active = false
	}
	f.seq++
	f.tabs = append(f.tabs, proto.BrowserTab{ID: f.seq, URL: u, Active: true})
	f.url = u
}

func (f *fakeDriver) Tabs(context.Context) ([]proto.BrowserTab, error) {
	return append([]proto.BrowserTab(nil), f.tabs...), nil
}
func (f *fakeDriver) OpenTab(_ context.Context, u string) error { f.add(u); return nil }
func (f *fakeDriver) SelectTab(_ context.Context, id int) error {
	for i := range f.tabs {
		if f.tabs[i].ID == id {
			for j := range f.tabs {
				f.tabs[j].Active = j == i
			}
			f.url = f.tabs[i].URL
			return nil
		}
	}
	return fmt.Errorf("browser: no tab %d; tabs lists them", id)
}
func (f *fakeDriver) CloseTab(_ context.Context, id int) error {
	for i, t := range f.tabs {
		if t.ID == id {
			f.tabs = append(f.tabs[:i], f.tabs[i+1:]...)
			if t.Active && len(f.tabs) > 0 {
				f.tabs[min(i, len(f.tabs)-1)].Active = true
				f.url = f.active().URL
			}
			return nil
		}
	}
	return fmt.Errorf("browser: no tab %d; tabs lists them", id)
}
func (f *fakeDriver) Acting() { f.acted++ }

func TestBrowserActsInTheDriversPage(t *testing.T) {
	m := &Manager{}
	ctx := context.Background()
	if _, err := m.Act(ctx, Action{Do: "read"}); !errors.Is(err, ErrNoDriver) {
		t.Fatalf("without a driver read gave %v", err)
	}
	d := &fakeDriver{}
	m.SetDriver(d)
	snap, err := m.Open(ctx, "localhost:5173")
	if err != nil || d.url != "http://localhost:5173" || snap.Title != "Calc" || snap.Status != 200 || string(snap.Screenshot) != "png" {
		t.Fatalf("open: %+v, %v, driver at %q", snap, err, d.url)
	}
	if len(snap.Console) != 1 || snap.Console[0].Text != "boom" || len(snap.Network) != 1 {
		t.Errorf("open's log: %+v %+v", snap.Console, snap.Network)
	}
	out, err := m.Act(ctx, Action{Do: "click", Ref: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Page: Calc", "URL: http://localhost:5173 [HTTP 200]", "Console error: boom (app.js:3)", "Network: 404 /api", `[1] button "Add"`} {
		if !strings.Contains(out, want) {
			t.Errorf("click's result lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1 + 2 = 3") {
		t.Error("click's result has the page's text, which only read gives")
	}
	if out, _ := m.Act(ctx, Action{Do: "read"}); !strings.Contains(out, "Text:\n1 + 2 = 3") {
		t.Errorf("read lacks the text:\n%s", out)
	}
	if _, err := m.Act(ctx, Action{Do: "click", Ref: 404}); err == nil || !strings.Contains(err.Error(), "ref 404") {
		t.Errorf("a stale ref gave %v", err)
	}
	if _, err := m.Act(ctx, Action{Do: "type", Ref: 2, Text: `say "hi"`, Submit: true}); err != nil {
		t.Fatal(err)
	}
	if last := d.evals[len(d.evals)-3]; !strings.Contains(last, `__stratum.type(2, "say \"hi\"", true)`) {
		t.Errorf("type sent %q", last[len(last)-60:])
	}
	if _, err := m.Act(ctx, Action{Do: "back"}); err != nil || d.steps[0] != "back" {
		t.Errorf("back: %v %v", err, d.steps)
	}
	if out, err := m.Act(ctx, Action{Do: "eval", Script: "return 6*7"}); err != nil || out != "42" {
		t.Errorf("eval gave %q, %v", out, err)
	}
	if got := m.CurrentURL(ctx); got != "http://localhost:5173" {
		t.Errorf("CurrentURL = %q", got)
	}
	if d.acted == 0 {
		t.Error("the driver never showed the agent at work")
	}
}

func TestBrowserTabs(t *testing.T) {
	m := &Manager{}
	ctx := context.Background()
	d := &fakeDriver{}
	m.SetDriver(d)
	if out, err := m.Act(ctx, Action{Do: "tabs"}); err != nil || out != "No tabs are open." {
		t.Errorf("tabs before any: %q, %v", out, err)
	}
	if _, err := m.Open(ctx, "localhost:5173"); err != nil {
		t.Fatal(err)
	}
	out, err := m.Act(ctx, Action{Do: "new_tab", URL: "localhost:3000"})
	if err != nil || !strings.Contains(out, "2 tabs are open (tabs lists them)") {
		t.Fatalf("new_tab: %q, %v", out, err)
	}
	if got := m.CurrentURL(ctx); got != "http://localhost:3000" {
		t.Errorf("after new_tab CurrentURL = %q", got)
	}
	out, _ = m.Act(ctx, Action{Do: "tabs"})
	for _, want := range []string{"  [1]  — http://localhost:5173", "* [2]  — http://localhost:3000"} {
		if !strings.Contains(out, want) {
			t.Errorf("tabs lacks %q:\n%s", want, out)
		}
	}
	if _, err := m.Act(ctx, Action{Do: "select_tab", Tab: 1}); err != nil || m.CurrentURL(ctx) != "http://localhost:5173" {
		t.Errorf("select_tab 1: %v, at %q", err, m.CurrentURL(ctx))
	}
	if _, err := m.Act(ctx, Action{Do: "select_tab", Tab: 9}); err == nil {
		t.Error("select_tab 9 found a tab")
	}
	out, err = m.Act(ctx, Action{Do: "close_tab", Tab: 1})
	if err != nil || strings.Contains(out, "[1]") || !strings.Contains(out, "* [2]") {
		t.Errorf("close_tab 1: %q, %v", out, err)
	}
	if out, err := m.Act(ctx, Action{Do: "new_tab"}); err != nil || !strings.Contains(out, "* [3]  — ") {
		t.Errorf("an empty new_tab: %q, %v", out, err)
	}
	if got := m.CurrentURL(ctx); got != "" {
		t.Errorf("an empty tab's CurrentURL = %q", got)
	}
}

func TestBrowserKeysDialogsAndWaits(t *testing.T) {
	m := &Manager{}
	ctx := context.Background()
	d := &fakeDriver{}
	m.SetDriver(d)
	if _, err := m.Open(ctx, "localhost:5173"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Act(ctx, Action{Do: "key", Keys: "shift+Tab"}); err != nil {
		t.Fatal(err)
	}
	if last := d.evals[len(d.evals)-3]; !strings.Contains(last, `__stratum.key({"key":"Tab","code":"Tab","shift":true,"ctrl":false,"alt":false,"meta":false})`) {
		t.Errorf("key sent %q", last[len(last)-90:])
	}
	if _, err := m.Act(ctx, Action{Do: "key", Keys: "hyper+x"}); err == nil {
		t.Error("an unknown modifier gave no error")
	}
	if _, err := m.Act(ctx, Action{Do: "click", Ref: 1, AcceptDialog: true, DialogText: "yes"}); err != nil {
		t.Fatal(err)
	}
	if js := d.evals[len(d.evals)-3]; !strings.Contains(js, `window.__stratumAgent = {accept: true, text: "yes", until: Date.now() + 4000};`) {
		t.Error("the click did not arm the dialogs' answer")
	}
	if out, _ := m.Act(ctx, Action{Do: "console"}); out != "[log] ready\n[dialog] confirm: Delete? → OK" {
		t.Errorf("console gave %q", out)
	}
	if out, _ := m.Act(ctx, Action{Do: "network"}); !strings.Contains(out, "GET 200") {
		t.Errorf("network gave %q", out)
	}
	d.body = "Saved at 10:02"
	if out, err := m.Act(ctx, Action{Do: "wait", Text: "Saved"}); err != nil || !strings.Contains(out, "Text:\n1 + 2 = 3") {
		t.Errorf("wait for shown text: %q, %v", out, err)
	}
	if _, err := m.Act(ctx, Action{Do: "wait", Text: "Failed", Seconds: 0.3}); err == nil || !strings.Contains(err.Error(), `"Failed" did not show in 0.3s`) {
		t.Errorf("wait for missing text gave %v", err)
	}
	if _, err := m.Act(ctx, Action{Do: "wait", Seconds: 0.2}); err != nil {
		t.Errorf("wait 0.2s: %v", err)
	}
}

func TestParseKeys(t *testing.T) {
	for in, want := range map[string]pageKey{
		"Enter":       {Key: "Enter", Code: "Enter"},
		"return":      {Key: "Enter", Code: "Enter"},
		"shift+tab":   {Key: "Tab", Code: "Tab", Shift: true},
		"cmd+a":       {Key: "a", Code: "KeyA", Meta: true},
		"ctrl+alt+F5": {Key: "F5", Code: "F5", Ctrl: true, Alt: true},
		"space":       {Key: " ", Code: "Space"},
		"ArrowDown":   {Key: "ArrowDown", Code: "ArrowDown"},
		"/":           {Key: "/"},
	} {
		if got, err := parseKeys(in); err != nil || got != want {
			t.Errorf("parseKeys(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "hyper+x", "enterr"} {
		if _, err := parseKeys(bad); err == nil {
			t.Errorf("parseKeys(%q) gave no error", bad)
		}
	}
}

func TestIsLocalPage(t *testing.T) {
	for raw, want := range map[string]bool{
		"": true, "about:blank": true, "http://localhost:3000/x": true, "http://127.0.0.1:8080": true,
		"https://example.com": false, "file:///tmp/a.html": true,
	} {
		if got := IsLocalPage(raw); got != want {
			t.Errorf("IsLocalPage(%q) = %v", raw, got)
		}
	}
}
