package app

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
)

// webPage is the page of the panel's browser: a *mygo.Page, or a test's.
type webPage interface {
	LoadURL(rawURL string) error
	GoBack()
	GoForward()
	Reload()
	Stop()
	EvalContext(ctx context.Context, code string) (any, error)
}

// browser is the panel's browser: one page, which the user and the agent
// share, as Claude's browser pane does. Its fields change on the UI
// thread, but for loads, which the page's events count from the main
// thread.
type browser struct {
	page    webPage
	view    ui.NativeWebView // nil in tests
	capture func() ([]byte, error)

	addr, shown, title string
	loading            bool
	back, forward      bool
	failed             string
	input              ui.Handle
	editing            bool // the address field has the focus

	mu    sync.Mutex
	loads int    // finished or failed loads
	err   string // the last failed load's
}

// UseBrowser gives the panel's browser a way to make its web view: the
// window's, an untrusted page that only navigates and runs no Go.
func (a *App) UseBrowser(newView func() (*mygo.WebView, error)) {
	a.newBrowser = func() (*browser, error) {
		wv, err := newView()
		if err != nil {
			return nil, err
		}
		b := &browser{page: wv.Page(), view: wv, capture: wv.CapturePage}
		pg := wv.Page()
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
			edit(func() { b.title = t })
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
			edit(func() { b.loading = false })
			go history()
		})
		pg.OnDidFailLoad(func(le *mygo.LoadError) {
			b.mu.Lock()
			b.loads++
			b.err = le.Description
			b.mu.Unlock()
			// A load the user or the next one stopped is no failure to show.
			if le.Code == -999 {
				edit(func() { b.loading = false })
				return
			}
			edit(func() { b.loading, b.failed = false, le.Description })
		})
		// A link to a new window opens here: the panel has one page.
		pg.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
			if req.UserInitiated && webScheme(req.URL) {
				go pg.LoadURL(req.URL)
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
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(strings.Trim(host, "[]")) != nil && (strings.HasPrefix(host, "127.") || host == "[::1]" || host == "::1") {
			return "http://" + s
		}
		if strings.Contains(host, ".") {
			return "https://" + s
		}
	}
	return "https://www.google.com/search?q=" + url.QueryEscape(s)
}

// openBrowser makes the panel's browser the first time it is needed.
func (a *App) openBrowser() *browser {
	if a.web == nil && a.newBrowser != nil {
		b, err := a.newBrowser()
		if err != nil {
			a.webErr = err.Error()
			return nil
		}
		a.web, a.webErr = b, ""
	}
	return a.web
}

// browse loads the address field's text, or target, in the panel.
func (a *App) browse(target string) {
	b := a.openBrowser()
	if b == nil || target == "" {
		return
	}
	b.addr, b.loading, b.failed = target, true, ""
	if err := b.page.LoadURL(target); err != nil {
		b.loading, b.failed = false, err.Error()
	}
}

func (a *App) showBrowser() {
	a.rv.open, a.rv.tab = true, "preview"
}

// browserPane is the 浏览器 tab: Safari's toolbar over the page. Without
// a web view, as in tests and on systems without one, it is the headless
// preview.
func (a *App) browserPane(c *ui.Context) {
	if a.newBrowser == nil {
		a.previewPane(c)
		return
	}
	p := a.p
	b := a.web
	if b == nil && a.webAddr == "" && a.C.Preview != nil {
		a.webAddr = a.C.Preview.URL
	}
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
		inp := ui.TextInputBase(c.Key("web-url"), addr).Bind(h).
			Placeholder(locales.BrowserAddress()).PlaceholderColor(p.Mute).
			FontSize(12.5).TextColor(p.Fg).SingleLine().Label(locales.BrowserAddress()).
			Height(30).Padding(0, 10).Radius(8).Background(p.Chip).Grow(1).MinWidth(0).Margin(0, 4)
		if b != nil {
			b.editing = h.Focused(c)
		}
		if inp.Submitted() {
			a.browse(addressTarget(*addr))
			if a.web != nil {
				a.webAddr = ""
			}
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
		if a.webButton(c, "web-ext", "external-link", locales.OpenInBrowser(), b != nil && b.shown != "" && a.openURL != nil) {
			_ = a.openURL(b.shown)
		}
	})
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
			ui.WebView(c, b.view).Grow(1)
		} else {
			ui.Column(c.Key("web-page")).Grow(1).Label(b.shown)
		}
	}
}

// webButton is a toolbar button that dims while it can do nothing, as
// Safari's back button does on a first page.
func (a *App) webButton(c *ui.Context, key, symbol, tip string, enabled bool) bool {
	bt := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip).Disabled(!enabled)
	col := a.p.Faint
	if enabled {
		press(bt)
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
// (preview.Driver): what it opens shows in the panel.
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

// page shows the panel's browser and returns it, made if need be.
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

// Navigate loads url in the panel and waits for it.
func (d *BrowserDriver) Navigate(ctx context.Context, u string) error {
	b, err := d.page(ctx)
	if err != nil {
		return err
	}
	from := b.mark()
	if err := d.onUI(ctx, func() { d.a.browse(u) }); err != nil {
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
	d.onUI(ctx, func() {
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
	})
	if refused != nil {
		return refused
	}
	return b.wait(ctx, from)
}

// Eval runs JavaScript in the panel's page.
func (d *BrowserDriver) Eval(ctx context.Context, js string) (any, error) {
	b, err := d.current(ctx)
	if err != nil {
		return nil, err
	}
	return b.page.EvalContext(ctx, js)
}

// Capture is a PNG of the panel's page.
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

// current is the browser as it is, without showing it: reading a page
// does not take the panel from what the user looks at.
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
