package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

// webPage is the page of a tab of the panel's browser: a *mygo.Page, or
// a test's.
type webPage interface {
	LoadURL(rawURL string) error
	GoBack()
	GoForward()
	Reload()
	Stop()
	EvalContext(ctx context.Context, code string) (any, error)
}

// browser is a tab of the panel's browser, which the user and the agent
// share, as Claude's browser pane does. Its fields change on the UI
// thread, but for loads, which the page's events count from the main
// thread.
type browser struct {
	id      int
	page    webPage
	view    ui.NativeWebView // nil in tests
	capture func() ([]byte, error)
	destroy func()

	addr, shown, title string
	loading            bool
	back, forward      bool
	failed             string
	input              ui.Handle
	viewHandle         ui.Handle
	editing            bool // the address field has the focus

	mu    sync.Mutex
	loads int    // finished or failed loads
	err   string // the last failed load's
}

// agentShows is how long the toolbar says the agent is at work after its
// last action in the browser.
const agentShows = 3 * time.Second

// UseBrowser gives the panel's browser a way to make the web view of a
// tab: the window's, an untrusted page that only navigates and runs no Go;
// and wipe, which deletes what the browser's pages stored of the kinds
// asked (WebCookies, WebCache: the history is the app's own).
func (a *App) UseBrowser(newView func() (*mygo.WebView, error), wipe func(WebData) error) {
	a.clearWebData = wipe
	a.newBrowser = func() (*browser, error) {
		wv, err := newView()
		if err != nil {
			return nil, err
		}
		pg := wv.Page()
		b := &browser{page: pg, view: wv, capture: wv.CapturePage, destroy: wv.Destroy}
		edit := func(f func()) {
			if a.update != nil {
				a.update(f)
			}
		}
		history := func() {
			back, fwd := pg.CanGoBack(), pg.CanGoForward()
			edit(func() { b.back, b.forward = back, fwd })
		}
		pg.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
			e.PreventDefault() // the window keeps its own title
			t := e.Title
			edit(func() {
				b.title = t
				a.titleVisit(b.shown, t)
			})
		})
		pg.OnWillNavigate(func(e *mygo.NavigateEvent) {
			if !webScheme(e.URL) {
				// mailto:, tel: and apps' own schemes go to the system, as
				// Safari hands them on.
				e.PreventDefault()
				if a.openURL != nil {
					_ = a.openURL(e.URL)
				}
				return
			}
			edit(func() { b.loading, b.failed = true, "" })
		})
		pg.OnDidNavigate(func(u string) {
			edit(func() {
				if !b.editing {
					b.addr = u
				}
				b.shown = u
			})
			go history()
		})
		pg.OnDidFinishLoad(func() {
			b.mu.Lock()
			b.loads++
			b.mu.Unlock()
			edit(func() {
				b.loading = false
				a.recordVisit(b.shown, b.title)
			})
			go history()
		})
		pg.OnDidFailLoad(func(le *mygo.LoadError) {
			b.mu.Lock()
			b.loads++
			b.err = le.Description
			b.mu.Unlock()
			edit(func() { b.loading, b.failed = false, le.Description })
		})
		// Downloads go to the Downloads folder or the one Settings name,
		// and show in the toolbar's Downloads, as Safari's do; an untrusted
		// page's would be canceled without a listener.
		pg.OnWillDownload(func(e *mygo.DownloadEvent) { a.willDownload(e) })
		pg.OnDownloadDone(func(d *mygo.Download) { a.downloadDone(d.URL, d.Path, d.Err) })
		// A link to a new window opens in a new tab, as Safari opens it.
		pg.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
			if req.UserInitiated && webScheme(req.URL) {
				u := req.URL
				edit(func() { a.newWebTab(u) })
			}
			return nil
		})
		return b, nil
	}
}

func webScheme(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "about" || u.Scheme == "blob" || u.Scheme == "data")
}

// addressTarget is where the text of the address field goes, as Safari
// reads it: a URL, a host (http for this machine's), or else a search.
func addressTarget(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "://") || strings.HasPrefix(s, "about:"):
		return s
	case !strings.ContainsAny(s, " \t"):
		host := s
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "::1" || strings.HasPrefix(host, "127.") && net.ParseIP(host) != nil {
			return "http://" + s
		}
		if strings.Contains(host, ".") {
			return "https://" + s
		}
	}
	return "https://www.google.com/search?q=" + url.QueryEscape(s)
}

// activeWeb is the tab in front, or nil before any.
func (a *App) activeWeb() *browser {
	if a.webActive < 0 || a.webActive >= len(a.webTabs) {
		return nil
	}
	return a.webTabs[a.webActive]
}

// newWebTab opens a tab in front, loading target unless it is empty.
func (a *App) newWebTab(target string) *browser {
	a.restoreWebTabs()
	if a.newBrowser == nil {
		return nil
	}
	b, err := a.newBrowser()
	if err != nil {
		a.webErr = err.Error()
		return nil
	}
	a.webSeq++
	b.id, a.webErr = a.webSeq, ""
	a.webTabs = append(a.webTabs, b)
	a.webActive = len(a.webTabs) - 1
	if target != "" {
		a.load(b, target)
	}
	return b
}

// openBrowser is the tab in front, made the first time it is needed.
func (a *App) openBrowser() *browser {
	a.restoreWebTabs()
	if b := a.activeWeb(); b != nil {
		return b
	}
	return a.newWebTab("")
}

func (a *App) load(b *browser, target string) {
	b.addr, b.loading, b.failed = target, true, ""
	if err := b.page.LoadURL(target); err != nil {
		b.loading, b.failed = false, err.Error()
		b.mu.Lock()
		b.err = err.Error()
		b.loads++
		b.mu.Unlock()
	}
}

// browse loads target in the tab in front.
func (a *App) browse(target string) {
	if b := a.openBrowser(); b != nil && target != "" {
		a.load(b, target)
	}
}

func (a *App) webTab(id int) int {
	for i, b := range a.webTabs {
		if b.id == id {
			return i
		}
	}
	return -1
}

// closeWebTab closes a tab; the one to its right comes to the front, or
// the one to its left for the last, as in Safari.
func (a *App) closeWebTab(id int) bool {
	i := a.webTab(id)
	if i < 0 {
		return false
	}
	if d := a.webTabs[i].destroy; d != nil {
		d()
	}
	a.webTabs = append(a.webTabs[:i], a.webTabs[i+1:]...)
	if a.webActive > i || a.webActive >= len(a.webTabs) {
		a.webActive--
	}
	a.webActive = max(a.webActive, 0)
	return true
}

func (a *App) showBrowser() {
	a.rv.open, a.rv.tab = true, "preview"
}

func (a *App) browserShown() bool {
	return a.newBrowser != nil && a.rv.open && a.rv.tab == "preview"
}

func (a *App) browserFocused(c *ui.Context) bool {
	if !a.browserShown() {
		return false
	}
	if a.webInput.Focused(c) {
		return true
	}
	b := a.activeWeb()
	return b != nil && (b.input.Focused(c) || b.viewHandle.Focused(c))
}

// browserPane is the 浏览器 tab: Safari's toolbar, its tab bar from two
// tabs, and the page. Without a web view, as in tests and on systems
// without one, it is the headless preview.
func (a *App) browserPane(c *ui.Context) {
	if a.newBrowser == nil {
		a.previewPane(c)
		return
	}
	p := a.p
	a.restoreWebTabs()
	b := a.activeWeb()
	if b == nil && a.webAddr == "" && a.C.Preview != nil {
		a.webAddr = a.C.Preview.URL
	}
	agent := !a.webAgent.IsZero() && a.now().Sub(a.webAgent) < agentShows
	ui.Row(c).Height(44).Shrink(0).Padding(0, 10).Gap(2).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		if a.webButton(c, "web-back", "chevron-left", locales.Back(), b != nil && b.back) {
			b.page.GoBack()
		}
		if a.webButton(c, "web-forward", "chevron-right", locales.Forward(), b != nil && b.forward) {
			b.page.GoForward()
		}
		addr := &a.webAddr
		h := &a.webInput
		if b != nil {
			addr, h = &b.addr, &b.input
		}
		if a.webFocus {
			a.webFocus = false
			h.Focus(c)
		}
		field := ui.Row(c).Height(30).Radius(8).Background(p.Chip).Grow(1).MinWidth(0).Margin(0, 4).Padding(0, 4, 0, 10).Gap(6).AlignItems(ui.Center)
		if agent {
			// While the agent acts, the field says so, in the accent, as
			// Claude in Chrome marks the tab it drives.
			field.InsetShadow(0, 0, 0, 1, p.Live)
		}
		field.Children(func() {
			sugg := a.addressSuggestions(b, *addr, h.Focused(c))
			if a.webSuggest >= len(sugg) {
				a.webSuggest = -1
			}
			chosen := a.webPick
			a.webPick = ""
			inp := ui.TextInputBase(c.Key("web-url"), addr).Bind(h).
				Placeholder(locales.BrowserAddress()).PlaceholderColor(p.Mute).
				FontSize(12.5).TextColor(p.Fg).SingleLine().Label(locales.BrowserAddress()).Grow(1).MinWidth(0).
				OnTextKey(func(k ui.TextKey) bool {
					// The history's pages under the field, as Codex's and
					// Safari's address fields offer them: ↓ and ↑ pick one,
					// Return opens it.
					switch {
					case k.Key == ui.KeyDown && k.Mods == 0 && len(sugg) > 0:
						a.webSuggest = (a.webSuggest + 1) % len(sugg)
					case k.Key == ui.KeyUp && k.Mods == 0 && len(sugg) > 0:
						a.webSuggest = (a.webSuggest + len(sugg)) % (len(sugg) + 1)
						if a.webSuggest == len(sugg) {
							a.webSuggest = len(sugg) - 1
						}
					case k.Key == ui.KeyEnter && a.webSuggest >= 0 && a.webSuggest < len(sugg):
						// Keys come between frames: the next one opens it.
						a.webPick = sugg[a.webSuggest].URL
					case k.Key == ui.KeyEscape && len(sugg) > 0:
						a.webSuggestOff = k.Text()
					default:
						if k.Key != ui.KeyEnter {
							a.webSuggest = -1
						}
						return false
					}
					return true
				})
			if b != nil {
				b.editing = h.Focused(c)
			}
			if pick := a.suggestionList(c, field, *addr, sugg); pick != "" {
				chosen = pick
			}
			switch {
			case chosen != "":
				// Put away until the next key typed, as the page loads.
				*addr = chosen
				a.webSuggest, a.webSuggestOff = -1, chosen
				a.browse(chosen)
				if a.activeWeb() != nil {
					a.webAddr = ""
				}
			case inp.Submitted():
				a.webSuggest, a.webSuggestOff = -1, *addr
				a.browse(addressTarget(*addr))
				if a.activeWeb() != nil {
					a.webAddr = ""
				}
			}
			if agent {
				ui.Text(c, locales.AgentBrowsing()).FontSize(11.5).FontWeight(500).TextColor(p.Live).SingleLine().Shrink(0)
			}
			if b != nil && b.loading {
				if a.webButton(c, "web-stop", "x", locales.StopLoading(), true) {
					b.page.Stop()
					b.loading = false
				}
			} else if a.webButton(c, "web-reload", "refresh", locales.Reload(), b != nil && b.shown != "") {
				b.loading, b.failed = true, ""
				b.page.Reload()
			}
		})
		if a.webButton(c, "web-new-tab", "plus", locales.NewTab(), true) {
			a.newWebTab("")
			a.webFocus = true
		}
		if a.webButton(c, "web-ext", "external-link", locales.OpenInBrowser(), b != nil && b.shown != "" && a.openURL != nil) {
			_ = a.openURL(b.shown)
		}
		a.downloadsButton(c)
		a.webMoreMenu(c)
	})
	if len(a.webTabs) > 1 {
		a.webTabBar(c)
	}
	// The toolbar or the tab bar may have opened or closed a tab: the page
	// is the one in front now, never a destroyed one.
	b = a.activeWeb()
	switch {
	case a.webErr != "":
		emptyState(c, p, "world", locales.Browser(), a.webErr, nil)
	case b == nil:
		emptyState(c, p, "world", locales.Browser(), locales.BrowserEmpty(), nil)
	default:
		if b.failed != "" {
			ui.Text(c, b.failed).FontSize(12).TextColor(p.Minus).Selectable().Padding(8, 14).Shrink(0)
		}
		if b.view != nil {
			ui.WebView(c.Key(fmt.Sprint("web-page:", b.id)), b.view).Bind(&b.viewHandle).Grow(1)
		} else {
			ui.Column(c.Key("web-page")).Bind(&b.viewHandle).Grow(1).Label(b.shown)
		}
	}
}

// webTabBar is Safari's tab bar: tabs of equal width, each with its close
// button while it is in front or under the pointer.
func (a *App) webTabBar(c *ui.Context) {
	p := a.p
	// A tab closes after the bar is drawn, not while its loop runs.
	closing := 0
	ui.Row(c).Height(32).Shrink(0).Padding(0, 8).Gap(4).AlignItems(ui.Center).BorderWidth(0, 0, .5, 0).BorderColor(p.Edge).Children(func() {
		for i, b := range a.webTabs {
			active := i == a.webActive
			title := b.title
			if title == "" {
				if u, err := url.Parse(b.shown); err == nil && u.Host != "" {
					title = u.Host
				} else {
					title = locales.NewTab()
				}
			}
			tab := ui.ButtonBase(c.Key(fmt.Sprint("web-tab:", b.id))).Height(24).Grow(1).MinWidth(0).Radius(6).Padding(0, 4, 0, 8).
				Label(title).Tooltip(b.shown)
			if active {
				tab.Background(p.Chip)
			} else if tab.Hovered() {
				tab.Background(p.Hover)
			}
			tab.Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(4).AlignItems(ui.Center).Children(func() {
					col := p.Fg2
					if active {
						col = p.Fg
					}
					ui.Text(c, title).FontSize(12).TextColor(col).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
					if active || tab.Hovered() {
						x := ui.ButtonBase(c.Key(fmt.Sprint("web-tab-close:", b.id))).Size(18, 18).Radius(4).Center().
							Label(locales.CloseTab()).Tooltip(locales.CloseTab())
						if x.Hovered() {
							x.Background(p.Hover)
						}
						x.Children(func() { sym(c, "x", 11, p.Mute) })
						if x.Clicked() {
							closing = b.id
						}
					}
				})
			})
			if tab.Clicked() && closing != b.id {
				a.webActive = i
			}
		}
	})
	if closing != 0 {
		a.closeWebTab(closing)
	}
}

// webButton is a toolbar button that dims while it can do nothing, as
// Safari's back button does on a first page.
func (a *App) webButton(c *ui.Context, key, symbol, tip string, enabled bool) bool {
	bt := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip).Disabled(!enabled)
	col := a.p.Faint
	if enabled {
		focusRing(bt, a.p)
		if bt.Hovered() {
			bt.Background(a.p.Hover)
		}
		col = a.p.Fg2
	}
	bt.Children(func() { sym(c, symbol, 16, col) })
	return enabled && bt.Clicked()
}

// BrowserDriver is the panel's browser for the agent's browser tool
// (preview.Driver): what it does shows in the panel.
func (a *App) BrowserDriver() *BrowserDriver { return &BrowserDriver{a: a} }

// BrowserDriver drives the panel's browser from the kernel's goroutines.
type BrowserDriver struct{ a *App }

var errNoBrowser = errors.New("the browser panel is not available on this system")

// onUI runs f on the UI thread and waits for it.
func (d *BrowserDriver) onUI(ctx context.Context, f func()) error {
	if d.a.update == nil {
		f()
		return nil
	}
	done := make(chan struct{})
	d.a.update(func() { f(); close(done) })
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// page shows the panel's browser and returns its tab in front, made if
// need be.
func (d *BrowserDriver) page(ctx context.Context) (*browser, error) {
	var b *browser
	err := d.onUI(ctx, func() {
		b = d.a.openBrowser()
		if b != nil {
			d.a.showBrowser()
		}
	})
	if err == nil && b == nil {
		err = errNoBrowser
	}
	return b, err
}

// current is the tab in front as it is, without showing the panel:
// reading a page does not take it from what the user looks at.
func (d *BrowserDriver) current(ctx context.Context) (*browser, error) {
	var b *browser
	if err := d.onUI(ctx, func() { b = d.a.openBrowser() }); err != nil {
		return nil, err
	}
	if b == nil {
		return nil, errNoBrowser
	}
	return b, nil
}

// wait returns after the page's next load, or its failure.
func (b *browser) wait(ctx context.Context, from int) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for {
		b.mu.Lock()
		n, err := b.loads, b.err
		b.mu.Unlock()
		if n != from {
			if err != "" {
				return errors.New(err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("the page did not finish loading in 20s")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (b *browser) mark() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = ""
	return b.loads
}

// Navigate loads url in the tab in front and waits for it.
func (d *BrowserDriver) Navigate(ctx context.Context, u string) error {
	b, err := d.page(ctx)
	if err != nil {
		return err
	}
	from := b.mark()
	if err := d.onUI(ctx, func() { d.a.load(b, u) }); err != nil {
		return err
	}
	return b.wait(ctx, from)
}

// Step goes back, forward or reloads, and waits for the page.
func (d *BrowserDriver) Step(ctx context.Context, step string) error {
	b, err := d.page(ctx)
	if err != nil {
		return err
	}
	from := b.mark()
	var refused error
	if err := d.onUI(ctx, func() {
		switch {
		case step == "back" && b.back:
			b.page.GoBack()
		case step == "forward" && b.forward:
			b.page.GoForward()
		case step == "reload" && b.shown != "":
			b.loading = true
			b.page.Reload()
		default:
			refused = errors.New("browser: no page to go " + step + " to")
		}
	}); err != nil {
		return err
	}
	if refused != nil {
		return refused
	}
	return b.wait(ctx, from)
}

// Eval runs JavaScript in the page in front.
func (d *BrowserDriver) Eval(ctx context.Context, js string) (any, error) {
	b, err := d.current(ctx)
	if err != nil {
		return nil, err
	}
	return b.page.EvalContext(ctx, js)
}

// Capture is a PNG of the page in front.
func (d *BrowserDriver) Capture(ctx context.Context) ([]byte, error) {
	b, err := d.page(ctx)
	if err != nil {
		return nil, err
	}
	if b.capture == nil {
		return nil, errNoBrowser
	}
	return b.capture()
}

// Tabs lists the tabs, without making one.
func (d *BrowserDriver) Tabs(ctx context.Context) ([]proto.BrowserTab, error) {
	var tabs []proto.BrowserTab
	err := d.onUI(ctx, func() {
		d.a.restoreWebTabs()
		for i, b := range d.a.webTabs {
			tabs = append(tabs, proto.BrowserTab{ID: b.id, URL: b.shown, Title: b.title, Active: i == d.a.webActive})
		}
	})
	return tabs, err
}

// OpenTab opens a tab in front, and waits for url to load in it.
func (d *BrowserDriver) OpenTab(ctx context.Context, u string) error {
	var b *browser
	if err := d.onUI(ctx, func() {
		if b = d.a.newWebTab(u); b != nil {
			d.a.showBrowser()
		}
	}); err != nil {
		return err
	}
	if b == nil {
		return errNoBrowser
	}
	if u == "" {
		return nil
	}
	return b.wait(ctx, 0)
}

// SelectTab brings a tab to the front.
func (d *BrowserDriver) SelectTab(ctx context.Context, id int) error {
	found := false
	if err := d.onUI(ctx, func() {
		if i := d.a.webTab(id); i >= 0 {
			d.a.webActive, found = i, true
			d.a.showBrowser()
		}
	}); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("browser: no tab %d; tabs lists them", id)
	}
	return nil
}

// CloseTab closes a tab.
func (d *BrowserDriver) CloseTab(ctx context.Context, id int) error {
	found := false
	if err := d.onUI(ctx, func() { found = d.a.closeWebTab(id) }); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("browser: no tab %d; tabs lists them", id)
	}
	return nil
}

// Acting shows the agent at work in the toolbar, for agentShows after
// its last action.
func (d *BrowserDriver) Acting() {
	if d.a.update == nil {
		return
	}
	d.a.update(func() { d.a.webAgent = d.a.now() })
	time.AfterFunc(agentShows+50*time.Millisecond, func() { d.a.update(func() {}) })
}

// restoreWebTabs opens the tabs the settings keep, once, the first time
// the browser shows or the agent uses it.
func (a *App) restoreWebTabs() {
	if a.webRestored || a.newBrowser == nil || a.C.Settings == nil {
		return
	}
	a.webRestored = true
	if len(a.webTabs) > 0 {
		return
	}
	set := a.C.Settings
	for _, u := range set.BrowserTabs {
		if a.newWebTab(u) == nil {
			break
		}
	}
	if set.BrowserTab >= 0 && set.BrowserTab < len(a.webTabs) {
		a.webActive = set.BrowserTab
	}
}

// saveWebTabs keeps the tabs' pages in the settings when they changed,
// once those the settings kept were opened.
func (a *App) saveWebTabs() {
	if !a.webRestored || a.C.Settings == nil {
		return
	}
	var urls []string
	active := 0
	for i, b := range a.webTabs {
		if b.shown == "" || b.shown == "about:blank" {
			continue
		}
		if i == a.webActive {
			active = len(urls)
		}
		urls = append(urls, b.shown)
	}
	s := *a.C.Settings
	if slices.Equal(s.BrowserTabs, urls) && s.BrowserTab == active {
		return
	}
	s.BrowserTabs, s.BrowserTab = urls, active
	a.do(a.C.SaveSettings(s))
}
