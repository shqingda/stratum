package preview

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeDriver answers the page script's calls as a page would.
type fakeDriver struct {
	url   string
	evals []string
	steps []string
}

func (f *fakeDriver) Navigate(_ context.Context, u string) error { f.url = u; return nil }
func (f *fakeDriver) Step(_ context.Context, s string) error {
	f.steps = append(f.steps, s)
	return nil
}
func (f *fakeDriver) Capture(context.Context) ([]byte, error) { return []byte("png"), nil }
func (f *fakeDriver) Eval(_ context.Context, js string) (any, error) {
	f.evals = append(f.evals, js)
	switch {
	case strings.HasSuffix(js, "return location.href"):
		return f.url, nil
	case strings.HasSuffix(js, "return document.readyState"):
		return "complete", nil
	case strings.Contains(js, "__stratum.click(404)"):
		return nil, errors.New("mygo: javascript error: no element with ref 404")
	case strings.Contains(js, "return __stratum."):
		return map[string]any{
			"url": f.url, "title": "Calc", "status": 200.0,
			"log":      []any{map[string]any{"level": "error", "text": "boom", "source": "app.js", "line": 3.0}, map[string]any{"level": "network", "text": "404 /api"}},
			"elements": `[1] button "Add"`, "text": "1 + 2 = 3",
		}, nil
	}
	return 42.0, nil
}

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
