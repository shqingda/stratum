package app

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

// fakePage loads at once: each load counts as its tab's next one.
type fakePage struct {
	b       *browser
	mu      sync.Mutex
	loaded  []string
	backs   int
	reloads int
}

// LoadURL runs on the UI thread, as the panel calls it, so it may set
// what the page shows; the load's end comes from another goroutine, as a
// web view's does.
func (f *fakePage) LoadURL(u string) error {
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
func (f *fakePage) GoForward() {}
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

func TestBrowserTabsCloseAsSafarisDo(t *testing.T) {
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
	if tt.HasText("Calc") {
		t.Error("one tab still shows the tab bar")
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

func TestBrowserKeysAreSafaris(t *testing.T) {
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

// TestClearHistoryEmptiesTheBrowser: the menu's Clear History… asks
// first; then the browser's data goes, and each tab is made anew at its
// page, without its back and forward history.
func TestClearHistoryEmptiesTheBrowser(t *testing.T) {
	a, tabs := browserApp(t)
	cleared := 0
	a.clearWebData = func() error { cleared++; return nil }
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	a.newWebTab("http://localhost:5173/")
	a.newWebTab("https://example.com/")
	a.webTabs[0].back = true
	ids := []int{a.webTabs[0].id, a.webTabs[1].id}
	tt.Frame()

	_, _, _, clearHistory := a.MenuBar()
	if clearHistory.Label != "清除历史记录…" {
		t.Errorf("the menu item says %q", clearHistory.Label)
	}
	a.askClearWeb()
	tt.Frame()
	if !tt.HasText("清除浏览器的历史记录？") {
		t.Fatalf("no dialog: %q", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cleared != 0 || tt.HasText("清除浏览器的历史记录？") {
		t.Fatalf("cancel cleared %d times, dialog shown %v", cleared, tt.HasText("清除浏览器的历史记录？"))
	}

	a.askClearWeb()
	tt.Frame()
	if err := tt.Click("清除历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cleared != 1 {
		t.Fatalf("cleared %d times", cleared)
	}
	if len(a.webTabs) != 2 || a.webTabs[0].id != ids[0] || a.webTabs[1].id != ids[1] {
		t.Fatalf("the tabs after clearing: %d", len(a.webTabs))
	}
	if a.webTabs[0].back || tabs.destroyed != 2 || len(tabs.pages) != 4 {
		t.Errorf("tabs were not made anew: back %v, destroyed %d, made %d", a.webTabs[0].back, tabs.destroyed, len(tabs.pages))
	}
	if got := tabs.pages[2].urls(); len(got) != 1 || got[0] != "http://localhost:5173/" {
		t.Errorf("the first tab reloaded %q", got)
	}
	if !tt.HasText("已清除历史记录") {
		t.Errorf("no word that it was done: %q", tt.Texts())
	}

	a.clearWebData = func() error { return fmt.Errorf("WebKit said no") }
	a.askClearWeb()
	tt.Frame()
	if err := tt.Click("清除历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("未能清除历史记录：WebKit said no") || tabs.destroyed != 2 {
		t.Errorf("a failure: %q, destroyed %d", tt.Texts(), tabs.destroyed)
	}
}

// TestBrowserReopensItsTabs: the tabs' pages stay in the settings, and the
// browser opens them again the first time it shows, as Safari reopens
// its windows.
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

// Clear History… is in Settings, under Browser, as Safari's Settings
// manage its website data, and in ⌘K, found by "cookie": not only in the
// app menu, where it was easy to miss.
func TestClearHistoryFromSettingsAndSearch(t *testing.T) {
	a, _ := browserApp(t)
	cleared := 0
	a.clearWebData = func() error { cleared++; return nil }
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()

	a.openSettings()
	tt.Frame()
	for _, want := range []string{"浏览器", "历史记录与网站数据", "Cookie、登录状态、网站存储和缓存，以及前进后退记录。"} {
		if !tt.HasText(want) {
			t.Errorf("Settings lack %q: %q", want, tt.Texts())
		}
	}
	a.settingsQuery = "cookie"
	tt.Frame()
	// General shows twice: in the list, found, and as the page's title.
	if n := slices.Index(tt.Texts(), "通用"); n < 0 || !slices.Contains(tt.Texts()[n+1:], "通用") {
		t.Errorf("searching Settings for cookie does not find General: %q", tt.Texts())
	}
	a.settingsQuery = ""
	if err := tt.Click("清除历史记录…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("清除浏览器的历史记录？") {
		t.Fatalf("Settings' button opened no dialog: %q", tt.Texts())
	}
	if err := tt.Click("清除历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cleared != 1 {
		t.Fatalf("cleared %d times", cleared)
	}
	a.settingsOpen = false
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("cookie")
	tt.Frame()
	if !tt.HasText("清除浏览器历史记录和 Cookie…") {
		t.Fatalf("⌘K does not find it: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("清除浏览器的历史记录？") {
		t.Fatalf("⌘K opened no dialog: %q", tt.Texts())
	}

	// Without a browser to clear, neither shows.
	b, _ := browserApp(t)
	b.openSettings()
	tb := ui.NewTester(b.View, 1440, 880)
	tb.Frame()
	if tb.HasText("历史记录与网站数据") {
		t.Error("Settings offer to clear a browser there is none of")
	}
}
