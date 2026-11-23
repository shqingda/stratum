package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/proto"
)

// Clear Browsing Data asks which kinds, as Codex's does: what the user
// leaves unchecked stays. The history is the app's own; the rest WebKit
// clears.
func TestClearBrowsingDataByKind(t *testing.T) {
	a, tabs := browserApp(t)
	var wiped []WebData
	a.clearWebData = func(k WebData) error { wiped = append(wiped, k); return nil }
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	a.newWebTab("http://localhost:5173/")
	a.newWebTab("https://example.com/")
	a.webTabs[0].back = true
	a.recordVisit("https://example.com/", "Example")
	tt.Frame()

	_, _, _, clearHistory := a.MenuBar()
	if clearHistory.Label != "清除历史记录…" {
		t.Errorf("the menu item says %q", clearHistory.Label)
	}
	a.askClearWeb()
	tt.Frame()
	for _, want := range []string{"清除浏览数据", "浏览历史记录", "Cookie 和网站数据", "缓存的图片和文件", "此操作无法撤销。"} {
		if !tt.HasText(want) {
			t.Fatalf("the dialog lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(wiped) != 0 || a.webClearing {
		t.Fatalf("cancel wiped %v, dialog shown %v", wiped, a.webClearing)
	}

	// Cookies left: only the history and the cache go.
	a.askClearWeb()
	tt.Frame()
	a.clearKinds &^= WebCookies
	tt.Frame()
	if err := tt.Click("清除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Equal(wiped, []WebData{WebCache}) {
		t.Fatalf("WebKit was asked to clear %v", wiped)
	}
	if len(a.webHistory) != 0 {
		t.Errorf("the history kept %v", a.webHistory)
	}
	if a.webTabs[0].back || tabs.destroyed != 2 {
		t.Errorf("tabs were not made anew: back %v, destroyed %d", a.webTabs[0].back, tabs.destroyed)
	}
	if !tt.HasText("已清除浏览数据") {
		t.Errorf("no word that it was done: %q", tt.Texts())
	}

	// Nothing chosen: nothing to clear.
	a.askClearWeb()
	a.clearKinds = 0
	tt.Frame()
	_ = tt.Click("清除")
	tt.Frame()
	if len(wiped) != 1 || !a.webClearing {
		t.Fatalf("with nothing chosen: wiped %v, dialog shown %v", wiped, a.webClearing)
	}
	a.webClearing = false

	a.clearWebData = func(WebData) error { return errors.New("WebKit said no") }
	a.clearWeb(webAll)
	tt.Frame()
	if !tt.HasText("未能清除历史记录：WebKit said no") || tabs.destroyed != 2 {
		t.Errorf("a failure: %q, destroyed %d", tt.Texts(), tabs.destroyed)
	}
}

// The toolbar has Downloads and ⋯ beside New Tab: ⋯ clears the cookies
// or the cache at once, as Cursor's browser menu does, or asks what to
// clear, and opens the browser's settings.
func TestBrowserToolbarMenu(t *testing.T) {
	a, tabs := browserApp(t)
	var wiped []WebData
	a.clearWebData = func(k WebData) error { wiped = append(wiped, k); return nil }
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	a.newWebTab("https://example.com/")
	tt.Frame()
	nt, ok1 := tt.Find("新建标签页")
	dl, ok2 := tt.Find("下载")
	more, ok3 := tt.Find("更多")
	if !ok1 || !ok2 || !ok3 || dl.Y != nt.Y || more.Y != nt.Y || dl.X <= nt.X || more.X <= dl.X {
		t.Fatalf("toolbar: New Tab %v %v, Downloads %v %v, More %v %v", nt, ok1, dl, ok2, more, ok3)
	}

	for _, tc := range []struct {
		item  string
		kind  WebData
		toast string
	}{{"清除 Cookie", WebCookies, "已清除 Cookie"}, {"清除缓存", WebCache, "已清除缓存"}} {
		if err := tt.Click("更多"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.ChooseMenuItem(tc.item); err != nil {
			t.Fatalf("%s: %v in %q", tc.item, err, tt.Menu())
		}
		tt.Frame()
		if len(wiped) == 0 || wiped[len(wiped)-1] != tc.kind || !tt.HasText(tc.toast) {
			t.Fatalf("%s wiped %v, toast %v", tc.item, wiped, tt.HasText(tc.toast))
		}
	}
	if tabs.destroyed != 1 {
		t.Errorf("clearing cookies did not reload the tab: destroyed %d", tabs.destroyed)
	}

	_ = tt.Click("更多")
	tt.Frame()
	if err := tt.ChooseMenuItem("清除浏览数据…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.webClearing || a.clearKinds != webAll {
		t.Fatalf("Clear Browsing Data… asked %v for %v", a.webClearing, a.clearKinds)
	}
	a.webClearing = false
	tt.Frame()

	_ = tt.Click("更多")
	tt.Frame()
	if err := tt.ChooseMenuItem("浏览器设置…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen || a.settingsPage != "browser" || !tt.HasText("下载位置") {
		t.Fatalf("Browser Settings… opened %v at %q", a.settingsOpen, a.settingsPage)
	}
}

// Downloads go where Settings say, numbered beside a file of the name,
// and list in the toolbar's Downloads with their size, as Safari's.
func TestBrowserDownloads(t *testing.T) {
	a, _ := browserApp(t)
	a.clearWebData = func(WebData) error { return nil }
	dir := t.TempDir()
	a.C.Settings = &proto.Settings{BrowserDownloads: dir}
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()

	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("没有下载") {
		t.Fatalf("an empty list: %q", tt.Texts())
	}
	a.downloadsOpen = false

	if err := os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &mygo.DownloadEvent{URL: "https://example.com/report.pdf", SuggestedName: "report.pdf"}
	a.willDownload(e)
	if want := filepath.Join(dir, "report (2).pdf"); e.Path != want {
		t.Fatalf("saved to %q, not %q", e.Path, want)
	}
	tt.Frame()
	a.downloadsOpen = true
	tt.Frame()
	if !tt.HasText("report (2).pdf") || !tt.HasText("正在下载…") {
		t.Fatalf("a download under way: %q", tt.Texts())
	}
	if err := os.WriteFile(e.Path, make([]byte, 2500), 0o644); err != nil {
		t.Fatal(err)
	}
	a.downloadDone(e.URL, e.Path, nil)
	tt.Frame()
	if !tt.HasText("3 KB · example.com") || !tt.HasText("已下载到“下载”：report (2).pdf") {
		t.Fatalf("a finished download: %q", tt.Texts())
	}

	f := &mygo.DownloadEvent{URL: "https://example.com/x.zip", SuggestedName: "x.zip"}
	a.willDownload(f)
	a.downloadDone(f.URL, f.Path, errors.New("network lost"))
	tt.Frame()
	if !tt.HasText("下载失败：network lost") {
		t.Fatalf("a failed download: %q", tt.Texts())
	}

	if err := tt.Click("清除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.webDownloads) != 0 || !tt.HasText("没有下载") {
		t.Fatalf("Clear left %d", len(a.webDownloads))
	}
	if _, err := os.Stat(e.Path); err != nil {
		t.Errorf("Clear took the file too: %v", err)
	}
}

// Ask Where to Save: a download waits in a folder of its own, then goes
// where the user says, or away when they cancel.
func TestBrowserAsksWhereToSave(t *testing.T) {
	a, _ := browserApp(t)
	dir := t.TempDir()
	a.C.Settings = &proto.Settings{BrowserDownloads: dir, BrowserAskDownload: true}
	a.syncDownloadPrefs()
	to := filepath.Join(dir, "chosen.zip")
	var asked []string
	a.askSavePath = func(name, at string) (string, error) {
		asked = append(asked, name, at)
		if len(asked) > 2 {
			return "", nil // canceled
		}
		return to, nil
	}
	for i := range 2 {
		e := &mygo.DownloadEvent{URL: "https://example.com/a.zip", SuggestedName: "a.zip"}
		a.willDownload(e)
		if e.Path == "" || filepath.Dir(filepath.Dir(e.Path)) == dir {
			t.Fatalf("staged at %q", e.Path)
		}
		if err := os.WriteFile(e.Path, []byte("zip"), 0o644); err != nil {
			t.Fatal(err)
		}
		a.downloadDone(e.URL, e.Path, nil)
		if _, err := os.Stat(filepath.Dir(e.Path)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the staging folder stays: %v", err)
		}
		d := a.webDownloads[i]
		switch i {
		case 0:
			if b, err := os.ReadFile(to); err != nil || string(b) != "zip" || d.path != to || d.name != "chosen.zip" {
				t.Fatalf("moved to %q (%v), listed as %q", to, err, d.path)
			}
		case 1:
			if !d.canceled {
				t.Fatalf("a canceled one: %+v", d)
			}
		}
	}
	if !slices.Equal(asked[:2], []string{"a.zip", dir}) {
		t.Errorf("asked %q", asked)
	}
}

// The history keeps what the browser showed, newest first and each page
// once; Settings › Browser searches it, reopens a page and takes one out,
// and it outlasts a quit.
func TestBrowserHistory(t *testing.T) {
	a, tabs := browserApp(t)
	a.clearWebData = func(WebData) error { return nil }
	file := filepath.Join(t.TempDir(), "browser-history.json")
	a.KeepBrowserHistory(file)
	clock := now
	a.now = func() time.Time { return clock }
	a.recordVisit("about:blank", "")
	a.recordVisit("https://go.dev/", "")
	a.titleVisit("https://go.dev/", "The Go Programming Language")
	clock = clock.Add(time.Minute)
	a.recordVisit("https://example.com/", "Example Domain")
	clock = clock.Add(time.Minute)
	a.recordVisit("https://go.dev/", "The Go Programming Language")
	a.recordVisit("https://go.dev/", "The Go Programming Language") // a reload
	if len(a.webHistory) != 3 {
		t.Fatalf("visits %v", a.webHistory)
	}
	got := a.historyMatches("", 10)
	if len(got) != 2 || got[0].URL != "https://go.dev/" || got[1].URL != "https://example.com/" {
		t.Fatalf("newest first, once each: %v", got)
	}
	if got := a.historyMatches("EXAMPLE", 10); len(got) != 1 {
		t.Fatalf("search: %v", got)
	}
	if err := a.SaveBrowserHistory(); err != nil {
		t.Fatal(err)
	}
	b := &App{}
	b.KeepBrowserHistory(file)
	if len(b.webHistory) != 3 || b.webHistory[0].Title != "The Go Programming Language" {
		t.Fatalf("read back %v", b.webHistory)
	}

	a.openSettings()
	a.settingsPage = "browser"
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	for _, want := range []string{"历史记录", "The Go Programming Language", "Example Domain", "清除浏览数据…", "下载位置", "下载前询问存放位置"} {
		if !tt.HasText(want) {
			t.Fatalf("Settings › Browser lacks %q: %q", want, tt.Texts())
		}
	}
	a.historyQuery = "nothing like it"
	tt.Frame()
	if !tt.HasText("没有找到") {
		t.Fatalf("no match: %q", tt.Texts())
	}
	a.historyQuery = ""
	tt.Frame()

	a.removeVisits("https://go.dev/")
	tt.Frame()
	if tt.HasText("The Go Programming Language") || len(a.webHistory) != 1 {
		t.Fatalf("removed: %v", a.webHistory)
	}
	b = &App{}
	b.KeepBrowserHistory(file)
	if len(b.webHistory) != 1 {
		t.Fatalf("the removal was not kept: %v", b.webHistory)
	}

	if err := tt.Click("Example Domain"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsOpen || len(a.webTabs) != 1 {
		t.Fatalf("reopening: settings %v, tabs %d", a.settingsOpen, len(a.webTabs))
	}
	if urls := tabs.pages[0].urls(); len(urls) != 1 || urls[0] != "https://example.com/" {
		t.Fatalf("opened %q", urls)
	}
}

// Settings' search finds the Browser page by what it holds.
func TestSettingsFindBrowser(t *testing.T) {
	a, _ := browserApp(t)
	a.clearWebData = func(WebData) error { return nil }
	a.openSettings()
	a.settingsQuery = "cookie"
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if !tt.HasText("浏览器") {
		t.Fatalf("searching cookie: %q", tt.Texts())
	}
	// Without a browser there is no such page.
	b := screen1()
	b.openSettings()
	tb := ui.NewTester(b.View, 1440, 880)
	tb.Frame()
	if tb.HasText("浏览器") {
		t.Fatalf("a Browser page with no browser: %q", tb.Texts())
	}
}

// Typing in the address field lists the history's pages it finds, as
// Codex's does: ↓ picks one and Return opens it, a click opens one, and
// Escape puts the list away.
func TestAddressFieldSuggestsHistory(t *testing.T) {
	a, tabs := browserApp(t)
	a.recordVisit("https://go.dev/doc/", "Documentation - The Go Programming Language")
	a.recordVisit("https://example.com/", "Example Domain")
	a.recordVisit("https://go.dev/blog/", "The Go Blog")
	a.showBrowser()
	tt := ui.NewTester(a.View, 1440, 880)
	tt.Frame()
	if err := tt.Click("搜索或输入网站名称"); err != nil {
		t.Fatal(err)
	}
	tt.Type("go.dev")
	tt.Frame()
	for _, want := range []string{"The Go Blog", "Documentation - The Go Programming Language"} {
		if !tt.HasText(want) {
			t.Fatalf("suggestions lack %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Example Domain") {
		t.Fatal("a page that does not match is suggested")
	}
	blog, _ := tt.Find("The Go Blog")
	doc, _ := tt.Find("Documentation - The Go Programming Language")
	if blog.Y >= doc.Y {
		t.Errorf("not newest first: blog at %v, doc at %v", blog.Y, doc.Y)
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("The Go Blog") {
		t.Fatal("Escape left the list")
	}
	tt.Type("/")
	tt.Frame()
	if !tt.HasText("The Go Blog") {
		t.Fatal("typing again did not bring the list back")
	}

	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(tabs.pages) != 1 {
		t.Fatalf("tabs made: %d", len(tabs.pages))
	}
	if urls := tabs.pages[0].urls(); len(urls) != 1 || urls[0] != "https://go.dev/doc/" {
		t.Fatalf("↓↓ Return opened %q", urls)
	}
	if tt.HasText("The Go Blog") {
		t.Fatal("the list stays after opening a page")
	}

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("example")
	tt.Frame()
	if err := tt.Click("Example Domain"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if urls := tabs.pages[0].urls(); urls[len(urls)-1] != "https://example.com/" {
		t.Fatalf("a click opened %q", urls)
	}
}
