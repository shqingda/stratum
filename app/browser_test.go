package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakePage loads at once: each load counts as the browser's next one.
type fakePage struct {
	b      *browser
	mu     sync.Mutex
	loaded []string
	backs  int
}

func (f *fakePage) LoadURL(u string) error {
	f.mu.Lock()
	f.loaded = append(f.loaded, u)
	f.mu.Unlock()
	go func() {
		f.b.mu.Lock()
		f.b.loads++
		f.b.mu.Unlock()
	}()
	return nil
}
func (f *fakePage) GoBack()    { f.backs++ }
func (f *fakePage) GoForward() {}
func (f *fakePage) Reload()    {}
func (f *fakePage) Stop()      {}
func (f *fakePage) EvalContext(context.Context, string) (any, error) {
	return "http://localhost:5173/", nil
}

func (f *fakePage) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.loaded...)
}

func browserApp(t *testing.T) (*App, *fakePage) {
	a := screen1()
	page := &fakePage{}
	made := 0
	a.newBrowser = func() (*browser, error) {
		made++
		if made > 1 {
			t.Error("the panel made a second browser")
		}
		b := &browser{page: page, capture: func() ([]byte, error) { return []byte("png"), nil }}
		page.b = b
		return b, nil
	}
	return a, page
}

func TestBrowserLoadsWhatTheAddressSays(t *testing.T) {
	a, page := browserApp(t)
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	for _, want := range []string{"浏览器", "输入网址，或让代理打开你的本地应用。"} {
		if !tt.HasText(want) {
			t.Errorf("the empty browser lacks %q: %q", want, tt.Texts())
		}
	}
	if a.web != nil {
		t.Fatal("showing the tab made a browser before anything was asked")
	}
	if err := tt.Click("后退"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("搜索或输入网站名称"); err != nil {
		t.Fatal(err)
	}
	tt.Type("localhost:5173")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := page.urls(); len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Fatalf("the address loaded %q", got)
	}
	if page.backs != 0 {
		t.Error("back on a first page went back")
	}
	a.web.shown, a.web.loading = "http://localhost:5173/", true
	tt.Frame()
	snapshot(t, tt, "pane-browser")
	a.web.back = true
	tt.Frame()
	if err := tt.Click("后退"); err != nil {
		t.Fatal(err)
	}
	if page.backs != 1 {
		t.Error("back did not go back")
	}
}

func TestBrowserAddressReadsAsSafariDoes(t *testing.T) {
	for in, want := range map[string]string{
		"localhost:3000":          "http://localhost:3000",
		"127.0.0.1:8080/a":        "http://127.0.0.1:8080/a",
		"example.com":             "https://example.com",
		"https://go.dev/doc":      "https://go.dev/doc",
		"golang generics":         "https://www.google.com/search?q=golang+generics",
		"stratum":                 "https://www.google.com/search?q=stratum",
		"   ":                     "",
		"app.localhost:5173/x?y=": "http://app.localhost:5173/x?y=",
	} {
		if got := addressTarget(in); got != want {
			t.Errorf("addressTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBrowserDriverShowsWhatTheAgentOpens(t *testing.T) {
	a, page := browserApp(t)
	var mu sync.Mutex
	var queue []func()
	a.update = func(f func()) { mu.Lock(); queue = append(queue, f); mu.Unlock() }
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	d := a.BrowserDriver()
	done := make(chan error, 1)
	go func() { done <- d.Navigate(context.Background(), "http://localhost:5173") }()
	for end := time.Now().Add(5 * time.Second); ; {
		mu.Lock()
		q := queue
		queue = nil
		mu.Unlock()
		for _, f := range q {
			f()
		}
		tt.Frame()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if !a.rv.open || a.rv.tab != "preview" {
				t.Errorf("the agent's page is not in front: open=%v tab=%q", a.rv.open, a.rv.tab)
			}
			if got := page.urls(); len(got) != 1 || got[0] != "http://localhost:5173" {
				t.Errorf("the driver loaded %q", got)
			}
			return
		default:
		}
		if time.Now().After(end) {
			t.Fatal("Navigate did not return")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
