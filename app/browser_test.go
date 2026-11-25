package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// fakePage loads at once: each load counts as its tab's next one.
type fakePage struct {
	b        *browser
	mu       sync.Mutex
	loaded   []string
	backs    int
	forwards int
	reloads  int
	loadErr  error
}

// LoadURL runs on the UI thread, as the panel calls it, so it may set
// what the page shows; the load's end comes from another goroutine, as a
// web view's does.
func (f *fakePage) LoadURL(u string) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	f.mu.Lock()
	f.loaded = append(f.loaded, u)
	f.mu.Unlock()
	f.b.shown = u
	go func() {
		f.b.mu.Lock()
		f.b.loads++
		f.b.mu.Unlock()
	}()
	return nil
}
func (f *fakePage) GoBack()    { f.backs++ }
func (f *fakePage) GoForward() { f.forwards++ }
func (f *fakePage) Reload()    { f.reloads++ }
func (f *fakePage) Stop()      {}
func (f *fakePage) EvalContext(context.Context, string) (any, error) {
	return "http://localhost:5173/", nil
}

func (f *fakePage) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.loaded...)
}

// fakeTabs are the pages of the browser's tabs, in the order made, and
// how many were destroyed.
type fakeTabs struct {
	pages     []*fakePage
	destroyed int
}

func browserApp(t *testing.T) (*App, *fakeTabs) {
	a := screen1()
	tabs := &fakeTabs{}
	a.newBrowser = func() (*browser, error) {
		page := &fakePage{}
		b := &browser{page: page, capture: func() ([]byte, error) { return []byte("png"), nil }, destroy: func() { tabs.destroyed++ }}
		page.b = b
		tabs.pages = append(tabs.pages, page)
		return b, nil
	}
	return a, tabs
}

func TestBrowserLoadsWhatTheAddressSays(t *testing.T) {
	a, tabs := browserApp(t)
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	for _, want := range []string{"浏览器", "输入网址，或让代理打开你的本地应用。"} {
		if !tt.HasText(want) {
			t.Errorf("the empty browser lacks %q: %q", want, tt.Texts())
		}
	}
	if a.activeWeb() != nil {
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
	if len(tabs.pages) != 1 {
		t.Fatalf("the address made %d tabs", len(tabs.pages))
	}
	page := tabs.pages[0]
	if got := page.urls(); len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Fatalf("the address loaded %q", got)
	}
	if page.backs != 0 {
		t.Error("back on a first page went back")
	}
	b := a.activeWeb()
	b.shown, b.loading = "http://localhost:5173/", true
	tt.Frame()
	snapshot(t, tt, "pane-browser")
	b.back = true
	tt.Frame()
	if err := tt.Click("后退"); err != nil {
		t.Fatal(err)
	}
	if page.backs != 1 {
		t.Error("back did not go back")
	}
}

func TestBrowserAddressResolution(t *testing.T) {
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

func TestBrowserTabsBehavior(t *testing.T) {
	a, tabs := browserApp(t)
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	for i, title := range []string{"Calc", "Docs", "Shop"} {
		a.newWebTab(fmt.Sprintf("http://localhost:%d/", 5173+i)).title = title
	}
	tt.Frame()
	for _, want := range []string{"Calc", "Docs", "Shop"} {
		if !tt.HasText(want) {
			t.Errorf("the tab bar lacks %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pane-browser-tabs")
	if a.activeWeb().title != "Shop" {
		t.Fatalf("the last tab made is not in front: %q", a.activeWeb().title)
	}
	if err := tt.Click("Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.activeWeb().title != "Docs" {
		t.Fatalf("a click on Docs put %q in front", a.activeWeb().title)
	}
	// Closing the tab in front brings the one to its right.
	if err := tt.Click("关闭标签页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.activeWeb().title; got != "Shop" || len(a.webTabs) != 2 || tabs.destroyed != 1 {
		t.Fatalf("after closing Docs: %q in front of %d tabs, %d destroyed", got, len(a.webTabs), tabs.destroyed)
	}
	// The last tab's left neighbour comes to the front.
	a.closeWebTab(a.activeWeb().id)
	tt.Frame()
	if got := a.activeWeb().title; got != "Calc" {
		t.Fatalf("after closing the last tab %q is in front", got)
	}
	if !tt.HasText("Calc") {
		t.Error("Chrome/M3 tab bar should stay visible even with a single tab")
	}
	a.closeWebTab(a.activeWeb().id)
	tt.Frame()
	if a.activeWeb() != nil || !tt.HasText("输入网址，或让代理打开你的本地应用。") {
		t.Errorf("closing every tab left %v: %q", a.activeWeb(), tt.Texts())
	}
	if err := tt.Click("新建标签页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.webTabs) != 1 || a.activeWeb() == nil {
		t.Errorf("the new tab button made %d tabs", len(a.webTabs))
	}
}

// pump runs f off the UI thread, playing what it sends to the UI thread
// until it returns.
func pump(t *testing.T, a *App, tt *ui.Tester, f func() error) error {
	t.Helper()
	var mu sync.Mutex
	var queue []func()
	a.update = func(g func()) { mu.Lock(); queue = append(queue, g); mu.Unlock() }
	defer func() { a.update = func(g func()) { g() } }()
	done := make(chan error, 1)
	go func() { done <- f() }()
	for end := time.Now().Add(5 * time.Second); ; {
		mu.Lock()
		q := queue
		queue = nil
		mu.Unlock()
		for _, g := range q {
			g()
		}
		tt.Frame()
		select {
		case err := <-done:
			return err
		default:
		}
		if time.Now().After(end) {
			t.Fatal("the driver did not return")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBrowserDriverShowsWhatTheAgentOpens(t *testing.T) {
	a, tabs := browserApp(t)
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	d := a.BrowserDriver()
	ctx := context.Background()
	if err := pump(t, a, tt, func() error { return d.Navigate(ctx, "http://localhost:5173") }); err != nil {
		t.Fatal(err)
	}
	if !a.rv.open || a.rv.tab != "preview" {
		t.Errorf("the agent's page is not in front: open=%v tab=%q", a.rv.open, a.rv.tab)
	}
	if got := tabs.pages[0].urls(); len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Errorf("the driver loaded %q", got)
	}
	if err := pump(t, a, tt, func() error { return d.OpenTab(ctx, "http://localhost:3000") }); err != nil {
		t.Fatal(err)
	}
	list, err := d.Tabs(ctx)
	if err != nil || len(list) != 2 || list[0].Active || !list[1].Active || list[1].URL != "http://localhost:3000" {
		t.Fatalf("tabs after OpenTab: %+v, %v", list, err)
	}
	if err := pump(t, a, tt, func() error { return d.SelectTab(ctx, list[0].ID) }); err != nil || a.activeWeb().id != list[0].ID {
		t.Errorf("SelectTab: %v", err)
	}
	if err := pump(t, a, tt, func() error { return d.SelectTab(ctx, 99) }); err == nil {
		t.Error("SelectTab found tab 99")
	}
	if err := pump(t, a, tt, func() error { return d.CloseTab(ctx, list[0].ID) }); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.Tabs(ctx); len(list) != 1 || !list[0].Active || list[0].URL != "http://localhost:3000" {
		t.Errorf("tabs after CloseTab: %+v", list)
	}
	if err := pump(t, a, tt, func() error { return d.CloseTab(ctx, list[0].ID) }); err == nil {
		t.Error("closing a closed tab gave no error")
	}
	if err := pump(t, a, tt, func() error { return d.OpenTab(ctx, "") }); err != nil || len(a.webTabs) != 2 {
		t.Errorf("an empty OpenTab: %v, %d tabs", err, len(a.webTabs))
	}
}

func TestBrowserShowsTheAgentAtWork(t *testing.T) {
	a, _ := browserApp(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	a.update = func(f func()) { f() }
	a.showBrowser()
	a.newWebTab("http://localhost:5173")
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if tt.HasText("代理正在使用") {
		t.Fatal("the toolbar says the agent is at work before it acts")
	}
	a.BrowserDriver().Acting()
	tt.Frame()
	if !tt.HasText("代理正在使用") {
		t.Errorf("the toolbar does not show the agent at work: %q", tt.Texts())
	}
	snapshot(t, tt, "pane-browser-agent")
	now = now.Add(agentShows + time.Second)
	tt.Frame()
	if tt.HasText("代理正在使用") {
		t.Error("the toolbar still shows the agent after it stopped")
	}
}

func TestBrowserKeyboardShortcuts(t *testing.T) {
	a, tabs := browserApp(t)
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	tt.Frame()
	if len(a.webTabs) != 1 {
		t.Fatalf("⌘T made %d tabs", len(a.webTabs))
	}
	tt.Type("localhost:5173")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := tabs.pages[0].urls(); len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Fatalf("the new tab's address field did not take the typing: %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyR)
	tt.Frame()
	if tabs.pages[0].reloads != 1 {
		t.Errorf("⌘R reloaded %d times", tabs.pages[0].reloads)
	}
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if len(a.webTabs) != 2 || a.webActive != 1 {
		t.Errorf("a second ⌘T: %d tabs, %d in front", len(a.webTabs), a.webActive)
	}
}

// TestBrowserReopensItsTabs verifies saved tabs reopen when the browser opens.
func TestBrowserReopensItsTabs(t *testing.T) {
	a, tabs := browserApp(t)
	a.C.Settings = &proto.Settings{BrowserTabs: []string{"http://localhost:5173/", "https://example.com/"}, BrowserTab: 1}
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if len(a.webTabs) != 0 {
		t.Fatal("the tabs opened before the browser showed")
	}
	a.showBrowser()
	tt.Frame()
	if len(a.webTabs) != 2 || a.webActive != 1 {
		t.Fatalf("reopened %d tabs, %d in front", len(a.webTabs), a.webActive)
	}
	if got := tabs.pages[0].urls(); len(got) != 1 || got[0] != "http://localhost:5173/" {
		t.Errorf("the first tab loaded %q", got)
	}
	a.closeWebTab(a.webTabs[0].id)
	tt.Frame()
	if got := a.C.Settings.BrowserTabs; len(got) != 1 || got[0] != "https://example.com/" || a.C.Settings.BrowserTab != 0 {
		t.Errorf("after closing a tab the settings keep %q, %d", got, a.C.Settings.BrowserTab)
	}
}

func TestBrowserFocusShortcuts(t *testing.T) {
	a, tabs := browserApp(t)
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()

	// Open two tabs and focus the browser address bar.
	a.newWebTab("http://localhost:5173")
	a.newWebTab("http://localhost:3000")
	a.webTabs[1].back = true
	a.webTabs[1].forward = true
	a.webFocus = true
	tt.Frame()

	// ⌘[ and ⌘] navigate browser history when browser has focus.
	tt.Key(ui.Cmd, ui.KeyBracketLeft)
	tt.Frame()
	if tabs.pages[1].backs != 1 {
		t.Fatalf("⌘[ in browser: backs=%d, want 1", tabs.pages[1].backs)
	}
	tt.Key(ui.Cmd, ui.KeyBracketRight)
	tt.Frame()
	if tabs.pages[1].forwards != 1 {
		t.Fatalf("⌘] in browser: forwards=%d, want 1", tabs.pages[1].forwards)
	}

	// ⌘W closes active browser tab when browser has focus.
	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if len(a.webTabs) != 1 {
		t.Fatalf("⌘W in browser left %d tabs, want 1", len(a.webTabs))
	}

	// ⌘L while browser has focus targets address bar instead of composer.
	a.webFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyL)
	tt.Frame()
	if !tt.Focused("搜索或输入网站名称") {
		t.Fatal("⌘L in browser did not keep focus in address bar")
	}

	// Outside browser focus, ⌘L focuses composer and ⌘[ steps sessions instead of browser.
	if err := tt.Click(locales.HideChanges()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.showBrowser()
	tt.Frame()
	prevBack := tabs.pages[0].backs
	a.webTabs[0].back = true
	tt.Key(ui.Cmd, ui.KeyBracketLeft)
	tt.Frame()
	if tabs.pages[0].backs != prevBack {
		t.Fatalf("⌘[ outside browser triggered browser GoBack")
	}
}

func TestBrowserDriverLoadErrorDoesNotHang(t *testing.T) {
	a, tabs := browserApp(t)
	a.update = func(fn func()) { fn() }
	origNew := a.newBrowser
	a.newBrowser = func() (*browser, error) {
		b, err := origNew()
		if err == nil {
			tabs.pages[len(tabs.pages)-1].loadErr = errors.New("bad url")
		}
		return b, err
	}
	drv := &BrowserDriver{a: a}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := drv.OpenTab(ctx, "http://bad"); err == nil || err.Error() != "bad url" {
		t.Fatalf("OpenTab = %v, want bad url", err)
	}
	if err := drv.Navigate(ctx, "http://bad"); err == nil || err.Error() != "bad url" {
		t.Fatalf("Navigate = %v, want bad url", err)
	}
}
