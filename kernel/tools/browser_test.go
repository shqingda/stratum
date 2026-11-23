package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/preview"
)

type pageAt string

func (p pageAt) Navigate(context.Context, string) error { return nil }
func (p pageAt) Step(context.Context, string) error     { return nil }
func (p pageAt) Capture(context.Context) ([]byte, error) {
	return nil, nil
}
func (p pageAt) Eval(context.Context, string) (any, error) { return string(p), nil }

func TestBrowserAsksBeforeActingOnTheWeb(t *testing.T) {
	defer preview.Default.SetDriver(nil)
	env := Env{Root: t.TempDir()}
	kind := func(args string) perm.Kind {
		t.Helper()
		req, err := Browser.Access(env, json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return req.Kind
	}
	if k := kind(`{"url":"localhost:3000"}`); k != perm.Read {
		t.Errorf("opening localhost is %s", k)
	}
	if k := kind(`{"action":"open","url":"https://example.com"}`); k != perm.Net {
		t.Errorf("opening a site is %s", k)
	}
	preview.Default.SetDriver(pageAt("http://localhost:3000/"))
	if k := kind(`{"action":"click","ref":1}`); k != perm.Read {
		t.Errorf("a click on a local page is %s", k)
	}
	preview.Default.SetDriver(pageAt("https://example.com/"))
	for _, args := range []string{`{"action":"click","ref":1}`, `{"action":"type","ref":1,"text":"x"}`, `{"action":"eval","script":"return 1"}`} {
		if k := kind(args); k != perm.Net {
			t.Errorf("%s on a site is %s", args, k)
		}
	}
	if k := kind(`{"action":"read"}`); k != perm.Read {
		t.Errorf("reading the page shown is %s", k)
	}
}
