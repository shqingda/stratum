package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/locales"
)

// WebData are the kinds of what the panel's browser keeps, which the user
// clears together or apart, as Chrome's and Codex's Clear browsing data
// offer them.
type WebData uint

const (
	// WebCookies is cookies and what sites store (local storage,
	// IndexedDB, service workers): sites' sign-ins go with them.
	WebCookies WebData = 1 << iota
	// WebCache is the images and files kept to load pages faster.
	WebCache
	// WebHistory is the pages visited and the tabs' back and forward.
	WebHistory
)

// webAll is every kind: Safari's Clear History, the app menu's.
const webAll = WebCookies | WebCache | WebHistory

// webVisit is a page the browser showed, for its history.
type webVisit struct {
	URL   string    `json:"url"`
	Title string    `json:"title,omitempty"`
	At    time.Time `json:"at"`
}

// historyMax is how many visits the history keeps: the oldest go first.
const historyMax = 5000

// webDownload is a file a page of the browser downloads, as Safari's
// Downloads list it.
type webDownload struct {
	id              int
	url, name, path string
	staged          string // where it downloads to while asking or before moving
	ask, retried    bool
	done, canceled  bool
	err             string
	at              time.Time
}

// downloadPrefs are Settings' download choices, as the page's download
// events, on the main thread, read them.
type downloadPrefs struct {
	mu  sync.Mutex
	dir string
	ask bool
}

func (d *downloadPrefs) set(dir string, ask bool) {
	d.mu.Lock()
	d.dir, d.ask = dir, ask
	d.mu.Unlock()
}

func (d *downloadPrefs) get() (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dir, d.ask
}

// KeepBrowserHistory reads the browser's history from file and keeps it
// there, as SaveBrowserHistory writes it.
func (a *App) KeepBrowserHistory(file string) {
	a.historyFile = file
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var visits []webVisit
	if json.Unmarshal(data, &visits) == nil {
		a.webHistory = visits
	}
}

// SaveBrowserHistory writes the history where KeepBrowserHistory read it,
// when it changed: at a quit, and after it was cleared.
func (a *App) SaveBrowserHistory() error {
	if a.historyFile == "" || !a.historyDirty {
		return nil
	}
	a.historyDirty = false
	if len(a.webHistory) == 0 {
		if err := os.Remove(a.historyFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(a.webHistory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.historyFile), 0o700); err != nil {
		return err
	}
	tmp := a.historyFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.historyFile)
}

// historyURL says whether a page belongs in the history: not a blank
// page, nor one that is the page's own data.
func historyURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// recordVisit adds a page that finished loading to the history. A reload
// of the page last visited only moves its time.
func (a *App) recordVisit(u, title string) {
	if !historyURL(u) {
		return
	}
	now := a.now()
	if n := len(a.webHistory); n > 0 && a.webHistory[n-1].URL == u {
		a.webHistory[n-1].At = now
		if title != "" {
			a.webHistory[n-1].Title = title
		}
	} else {
		a.webHistory = append(a.webHistory, webVisit{URL: u, Title: title, At: now})
		if over := len(a.webHistory) - historyMax; over > 0 {
			a.webHistory = slices.Delete(a.webHistory, 0, over)
		}
	}
	a.historyDirty = true
}

// titleVisit gives the page's last visit the title it got after loading.
func (a *App) titleVisit(u, title string) {
	if title == "" {
		return
	}
	for i := len(a.webHistory) - 1; i >= 0; i-- {
		if a.webHistory[i].URL == u {
			if a.webHistory[i].Title != title {
				a.webHistory[i].Title = title
				a.historyDirty = true
			}
			return
		}
	}
}

// historyMatches are the pages of the history whose title or address has
// q, newest first, each page once: at most n.
func (a *App) historyMatches(q string, n int) []webVisit {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []webVisit
	seen := map[string]bool{}
	for i := len(a.webHistory) - 1; i >= 0 && len(out) < n; i-- {
		v := a.webHistory[i]
		if seen[v.URL] {
			continue
		}
		seen[v.URL] = true
		if q == "" || strings.Contains(strings.ToLower(v.Title), q) || strings.Contains(strings.ToLower(v.URL), q) {
			out = append(out, v)
		}
	}
	return out
}

// removeVisits takes a page out of the history, every visit of it.
func (a *App) removeVisits(u string) {
	n := len(a.webHistory)
	a.webHistory = slices.DeleteFunc(a.webHistory, func(v webVisit) bool { return v.URL == u })
	if len(a.webHistory) != n {
		a.historyDirty = true
		_ = a.SaveBrowserHistory()
	}
}

// askClearWeb asks what of the browser's data to clear, all of it
// chosen, as Safari's Clear History… clears.
func (a *App) askClearWeb() {
	if a.clearWebData != nil {
		a.clearKinds, a.webClearing = webAll, true
	}
}

// clearWebDialog asks which of the browser's data to clear, as Codex's
// Clear browsing data does: the kinds, each with what it means.
func (a *App) clearWebDialog(c *ui.Context) {
	if !a.webClearing {
		return
	}
	open := true
	p := a.p
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		scrim := ui.RGBA(12, 12, 14, .18)
		if p.Dark {
			scrim = ui.RGBA(0, 0, 0, .42)
		}
		backdrop.Background(scrim)
		panel.Width(400).Padding(18, 18, 14, 18).Radius(14).Background(p.Card).Label(locales.ClearWebTitle())
		lift(panel, p).Children(func() {
			ui.Text(c, locales.ClearWebTitle()).FontSize(15).FontWeight(600).TextColor(p.Fg)
			ui.Column(c).Gap(10).Margin(14, 0, 12, 0).Children(func() {
				for _, k := range []struct {
					kind      WebData
					label, at string
				}{
					{WebHistory, locales.WebHistoryKind(), locales.WebHistoryKindNote()},
					{WebCookies, locales.WebCookiesKind(), locales.WebCookiesKindNote()},
					{WebCache, locales.WebCacheKind(), locales.WebCacheKindNote()},
				} {
					on := a.clearKinds&k.kind != 0
					// The row is the check box, as a Mac's label with its box
					// toggles it: the box drawn as macOS draws one.
					row := ui.CheckboxBase(c.Key(fmt.Sprint("clear-kind:", k.kind)), &on).Row().Gap(9).AlignItems(ui.Start).Label(k.label)
					focusRing(row, p)
					row.Children(func() {
						box := ui.Box(c).Size(16, 16).Shrink(0).Radius(4).Margin(1, 0, 0, 0).Center()
						if on {
							box.Background(p.Acc).Children(func() { sym(c, "check", 11, p.OnAcc) })
						} else {
							box.Background(p.Sheet).Border(.5, p.Edge2)
						}
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, k.label).FontSize(13.5).FontWeight(500).TextColor(p.Fg)
							ui.Text(c, k.at).FontSize(12).LineHeight(1.4).TextColor(p.Mute)
						})
					})
					if on {
						a.clearKinds |= k.kind
					} else {
						a.clearKinds &^= k.kind
					}
				}
			})
			ui.Text(c, locales.CantUndo()).FontSize(12).TextColor(p.Mute).Margin(0, 0, 14, 0)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, "clear-web-cancel", locales.Cancel(), "esc", false).Clicked() {
					open = false
				}
				ok := a.button(c, "clear-web-ok", locales.ClearWebOK(), "", true).Background(p.Minus).Disabled(a.clearKinds == 0)
				if a.clearKinds != 0 && ok.Clicked() {
					a.clearWeb(a.clearKinds)
					open = false
				}
			})
		})
	})
	if !open {
		a.webClearing = false
	}
}

// clearWeb deletes kinds of the browser's data. Clearing the history or
// the cookies makes each tab anew at its page: its back and forward go,
// and the page drops what it held in memory.
func (a *App) clearWeb(kinds WebData) {
	wipe, update := a.clearWebData, a.update
	if wipe == nil || kinds == 0 {
		return
	}
	done := func(err error) {
		if err != nil {
			a.flash("alert-circle", locales.ClearWebFailed(err.Error()))
			return
		}
		if kinds&WebHistory != 0 {
			a.webHistory = nil
			a.historyDirty = true
			_ = a.SaveBrowserHistory()
		}
		if kinds&(WebHistory|WebCookies) != 0 {
			a.remakeWebTabs()
		}
		switch kinds {
		case WebCookies:
			a.flash("check", locales.CookiesCleared())
		case WebCache:
			a.flash("check", locales.CacheCleared())
		case WebHistory:
			a.flash("check", locales.HistoryCleared())
		default:
			a.flash("check", locales.WebDataCleared())
		}
	}
	site := kinds &^ WebHistory
	if site == 0 {
		done(nil)
		return
	}
	if update == nil {
		done(wipe(site))
		return
	}
	// WebKit clears on the main thread: wait for it off the UI's.
	go func() {
		err := wipe(site)
		update(func() { done(err) })
	}()
}

// remakeWebTabs makes each tab's page anew, at the page it showed.
func (a *App) remakeWebTabs() {
	for i, b := range a.webTabs {
		nb, err := a.newBrowser()
		if err != nil {
			continue
		}
		nb.id = b.id
		a.webTabs[i] = nb
		if b.destroy != nil {
			b.destroy()
		}
		if b.shown != "" {
			a.load(nb, b.shown)
		}
	}
}

// webMoreMenu is the toolbar's ⋯: clearing the browser's cookies, cache
// and data, as Cursor's browser menu has them, and its settings.
func (a *App) webMoreMenu(c *ui.Context) {
	if a.clearWebData == nil {
		return
	}
	p := a.p
	bt := ui.ButtonBase(c.Key("web-more")).Size(28, 28).Radius(7).Center().Label(locales.MoreBrowser()).Tooltip(locales.MoreBrowser())
	focusRing(bt, p)
	if bt.Hovered() {
		bt.Background(p.Hover)
	}
	bt.Children(func() { sym(c, "dots", 16, p.Fg2) })
	bt.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(locales.ClearCookies()).ID("web-clear-cookies").Chosen() {
			a.clearWeb(WebCookies)
		}
		if m.Item(locales.ClearCache()).ID("web-clear-cache").Chosen() {
			a.clearWeb(WebCache)
		}
		if m.Item(locales.ClearWebData() + "…").ID("web-clear-all").Chosen() {
			a.askClearWeb()
		}
		m.Separator()
		if m.Item(locales.Downloads()).ID("web-downloads").Chosen() {
			a.downloadsOpen = true
		}
		if m.Item(locales.BrowserSettings() + "…").ID("web-settings").Chosen() {
			a.openSettings()
			a.settingsPage = "browser"
		}
	}, a.menuOptions(nil))
}

// syncDownloadPrefs passes Settings' download choices to the pages'
// download events.
func (a *App) syncDownloadPrefs() {
	if a.C.Settings != nil {
		a.dlPrefs.set(a.C.Settings.BrowserDownloads, a.C.Settings.BrowserAskDownload)
	}
}

// willDownload says where a page's download goes: the folder Settings
// name, or Downloads; while the user is to say where, a folder of its own
// until then. It runs on the main thread, as the page's events do.
func (a *App) willDownload(e *mygo.DownloadEvent) {
	dir, ask := a.dlPrefs.get()
	staged := ""
	switch {
	case ask || dir == "":
		// Stage in TMPDIR when asking or using default ~/Downloads so macOS
		// WebKit's networking XPC process does not fail with "Cannot create file"
		// under TCC/sandbox before our Go process moves it into ~/Downloads.
		tmp, err := os.MkdirTemp("", "stratum-download-")
		if err != nil {
			if ask {
				e.PreventDefault()
				return
			}
		} else {
			staged = filepath.Join(tmp, e.SuggestedName)
			e.Path = staged
		}
	case dir != "":
		if err := os.MkdirAll(dir, 0o755); err == nil {
			e.Path = uniquePath(filepath.Join(dir, e.SuggestedName))
		}
	}
	u, name, path := e.URL, e.SuggestedName, e.Path
	a.edit(func() {
		a.nextDownload++
		a.webDownloads = append(a.webDownloads, &webDownload{id: a.nextDownload, url: u, name: filepath.Base(name), path: path, staged: staged, ask: ask, at: a.now()})
		if path != "" && staged == "" {
			a.webDownloads[len(a.webDownloads)-1].name = filepath.Base(path)
		}
	})
}

// downloadDone marks a download done, or failed; one that waits for where
// to go asks now.
func (a *App) downloadDone(u, path string, err error) {
	a.edit(func() {
		var d *webDownload
		for _, x := range a.webDownloads {
			if !x.done && (path != "" && x.path == path || path == "" && x.url == u) {
				d = x
				break
			}
		}
		if d == nil {
			d = &webDownload{url: u, path: path, name: filepath.Base(path), at: a.now()}
			a.nextDownload++
			d.id = a.nextDownload
			a.webDownloads = append(a.webDownloads, d)
		}
		if err != nil {
			if !d.retried && (strings.HasPrefix(d.url, "http://") || strings.HasPrefix(d.url, "https://")) &&
				strings.Contains(err.Error(), "Cannot create file") {
				if d.staged != "" {
					_ = os.RemoveAll(filepath.Dir(d.staged))
					d.staged = ""
				}
				d.retried = true
				a.fallbackDownload(d)
				return
			}
			d.done = true
			d.err = err.Error()
			a.flash("alert-circle", locales.DownloadFailed(d.err))
			return
		}
		d.done = true
		if d.staged != "" {
			if d.ask {
				a.placeDownload(d)
				return
			}
			a.finishStagedDownload(d)
			return
		}
		a.flash("download", locales.Downloaded(d.name))
	})
}

func (a *App) defaultDownloadDir() string {
	dir, _ := a.dlPrefs.get()
	if dir == "" {
		if d, err := mygo.App.Path(mygo.PathDownloads); err == nil {
			dir = d
		}
	}
	return dir
}

// finishStagedDownload moves an auto-staged download to the configured folder
// or system Downloads directory without prompting.
func (a *App) finishStagedDownload(d *webDownload) {
	staged, name := d.staged, d.name
	dir := a.defaultDownloadDir()
	d.staged = ""
	if dir == "" {
		_ = os.RemoveAll(filepath.Dir(staged))
		d.err = "downloads directory unavailable"
		a.flash("alert-circle", locales.DownloadFailed(d.err))
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = os.RemoveAll(filepath.Dir(staged))
		d.err = err.Error()
		a.flash("alert-circle", locales.DownloadFailed(d.err))
		return
	}
	to := uniquePath(filepath.Join(dir, name))
	err := moveFile(staged, to)
	_ = os.RemoveAll(filepath.Dir(staged))
	if err != nil {
		d.err = err.Error()
		a.flash("alert-circle", locales.DownloadFailed(d.err))
		return
	}
	d.path, d.name = to, filepath.Base(to)
	a.flash("download", locales.Downloaded(d.name))
}

// fallbackDownload fetches u directly over HTTP when WebKit's WKDownload fails
// with "Cannot create file" (e.g. XPC sandbox restrictions on destination).
func (a *App) fallbackDownload(d *webDownload) {
	u, name, destPath, ask := d.url, d.name, d.path, d.ask
	dir := a.defaultDownloadDir()
	run := func() {
		var tmpStaged string
		var finalPath string
		var dlErr error
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Get(u)
		if err != nil {
			dlErr = err
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				dlErr = fmt.Errorf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
			} else if ask {
				if tmp, err := os.MkdirTemp("", "stratum-download-"); err != nil {
					dlErr = err
				} else {
					tmpStaged = filepath.Join(tmp, name)
					f, err := os.Create(tmpStaged)
					if err != nil {
						dlErr = err
						_ = os.RemoveAll(tmp)
						tmpStaged = ""
					} else {
						_, dlErr = io.Copy(f, resp.Body)
						if cerr := f.Close(); dlErr == nil {
							dlErr = cerr
						}
						if dlErr != nil {
							_ = os.RemoveAll(tmp)
							tmpStaged = ""
						}
					}
				}
			} else {
				target := destPath
				if target == "" || strings.Contains(target, string(filepath.Separator)+"stratum-download-") {
					if dir == "" {
						dlErr = errors.New("downloads directory unavailable")
					} else if err := os.MkdirAll(dir, 0o755); err != nil {
						dlErr = err
					} else {
						target = uniquePath(filepath.Join(dir, name))
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						dlErr = err
					} else {
						target = uniquePath(target)
					}
				}
				if dlErr == nil {
					f, err := os.Create(target)
					if err != nil {
						dlErr = err
					} else {
						_, dlErr = io.Copy(f, resp.Body)
						if cerr := f.Close(); dlErr == nil {
							dlErr = cerr
						}
						if dlErr == nil {
							finalPath = target
						} else {
							_ = os.Remove(target)
						}
					}
				}
			}
		}
		a.edit(func() {
			d.done = true
			if dlErr != nil {
				d.err = dlErr.Error()
				a.flash("alert-circle", locales.DownloadFailed(d.err))
				return
			}
			if tmpStaged != "" {
				d.staged = tmpStaged
				a.placeDownload(d)
				return
			}
			d.path, d.name = finalPath, filepath.Base(finalPath)
			a.flash("download", locales.Downloaded(d.name))
		})
	}
	if a.update == nil {
		run()
		return
	}
	go run()
}

// placeDownload asks where a download that waited goes, then moves it
// there; canceled, it is deleted.
func (a *App) placeDownload(d *webDownload) {
	ask := a.askSavePath
	if ask == nil {
		ask = func(name, dir string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{DefaultPath: filepath.Join(dir, name), CreateDirectories: true})
		}
	}
	dir, _ := a.dlPrefs.get()
	if dir == "" {
		if d, err := mygo.App.Path(mygo.PathDownloads); err == nil {
			dir = d
		}
	}
	staged, name := d.staged, d.name
	run := func() {
		to, err := ask(name, dir)
		if err == nil && to != "" {
			err = moveFile(staged, to)
		}
		_ = os.RemoveAll(filepath.Dir(staged))
		a.edit(func() {
			d.staged = ""
			switch {
			case err != nil:
				d.err = err.Error()
				a.flash("alert-circle", locales.DownloadFailed(d.err))
			case to == "":
				d.canceled = true
			default:
				d.path, d.name = to, filepath.Base(to)
				a.flash("download", locales.Downloaded(d.name))
			}
		})
	}
	if a.update == nil {
		run() // a test's: no panel to wait for
		return
	}
	// The save panel waits for the user: not on the UI thread.
	go run()
}

// moveFile moves a file, copying it where a rename cannot: to another
// volume.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// uniquePath numbers path, as "name (2).ext", when a file has its name,
// as the Downloads folder's are.
func uniquePath(path string) string {
	if _, err := os.Lstat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		p := base + " (" + strconv.Itoa(i) + ")" + ext
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}

// edit changes the UI's state from the page's events: on the UI thread,
// or at once where there is none (a test).
func (a *App) edit(f func()) {
	if a.update != nil {
		a.update(f)
		return
	}
	f()
}

// downloadsButton is Safari's Downloads in the toolbar: its list of the
// browser's downloads, each shown in Finder when clicked.
func (a *App) downloadsButton(c *ui.Context) {
	if a.newBrowser == nil {
		return
	}
	p := a.p
	busy := false
	for _, d := range a.webDownloads {
		busy = busy || !d.done || d.staged != ""
	}
	bt := ui.ButtonBase(c.Key("web-downloads")).Size(28, 28).Radius(7).Center().Label(locales.Downloads()).Tooltip(locales.Downloads())
	focusRing(bt, p)
	if bt.Hovered() || a.downloadsOpen {
		bt.Background(p.Hover)
	}
	col := p.Fg2
	if busy {
		col = p.Live
	}
	bt.Children(func() { sym(c, "download", 16, col) })
	if bt.Clicked() {
		a.downloadsOpen = !a.downloadsOpen
	}
	pop := ui.Popover(c, bt, &a.downloadsOpen, func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Row(c).Padding(4, 6, 6, 8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.Downloads()).FontSize(13).FontWeight(600).TextColor(p.Fg)
				ui.Spacer(c)
				if len(a.webDownloads) > 0 && a.pushButton(c, "downloads-clear", locales.ClearList()).Height(24).Clicked() {
					// Safari's Clear: the list, not the files.
					a.webDownloads = slices.DeleteFunc(a.webDownloads, func(d *webDownload) bool { return d.done && d.staged == "" })
				}
			})
			if len(a.webDownloads) == 0 {
				ui.Text(c, locales.NoDownloads()).FontSize(12.5).TextColor(p.Mute).Padding(10, 8, 14, 8)
				return
			}
			for i := len(a.webDownloads) - 1; i >= 0; i-- {
				a.downloadRow(c, a.webDownloads[i])
			}
		})
	})
	pop.AttachTo(bt, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(6, 0, 0, 0).
		Width(300).Background(p.Card).Border(.5, p.Edge2).Radius(12).Padding(6).NoShadow()
	lift(pop, p)
	rim(pop, p)
}

// downloadRow is a download in the list: its name, and how it went.
func (a *App) downloadRow(c *ui.Context, d *webDownload) {
	p := a.p
	status, col := locales.Downloading(), p.Live
	switch {
	case d.err != "":
		status, col = locales.DownloadFailed(d.err), p.Minus
	case d.canceled:
		status, col = locales.DownloadCanceled(), p.Mute
	case d.staged != "":
		status, col = locales.ChoosingWhere(), p.Mute
	case d.done:
		status, col = shortHost(d.url), p.Mute
		if fi, err := os.Stat(d.path); err == nil {
			status = formatBytes(fi.Size()) + " · " + status
		}
	}
	found := d.done && d.err == "" && !d.canceled && d.staged == ""
	row := ui.ButtonBase(c.Key(fmt.Sprint("download:", d.id))).Row().FillWidth().Padding(6, 8).Gap(9).Radius(7).AlignItems(ui.Center).
		Label(d.name).Disabled(!found)
	if found {
		row.Tooltip(locales.RevealInFinder())
		if row.Hovered() {
			row.Background(p.Hover)
		}
	}
	row.Children(func() {
		sym(c, "file", 18, p.Fg2)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, d.name).FontSize(12.5).FontWeight(500).TextColor(p.Fg).SingleLine().Ellipsis("…")
			ui.Text(c, status).FontSize(11.5).TextColor(col).SingleLine().Ellipsis("…")
		})
	})
	if found && row.Clicked() {
		_ = exec.Command("open", "-R", d.path).Start()
	}
}

// shortHost is a URL's host, or the URL.
func shortHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return raw
}

// formatBytes is a file's size as Finder says it, in units of 1000.
func formatBytes(n int64) string {
	switch {
	case n < 1000:
		return locales.SizeBytes(int(n))
	case n < 1000*1000:
		return fmt.Sprintf("%.0f KB", math.Round(float64(n)/1000))
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

// browserSettingsPage is Settings › Browser, as Codex's: the history, to
// search, reopen a page of or take a page out of; Clear Browsing Data;
// and where downloads go.
func (a *App) browserSettingsPage(c *ui.Context) {
	p := a.p
	set := a.settings()
	section(c, p, locales.BrowsingHistory(), true)
	ui.SearchField(c, &a.historyQuery).Label(locales.SearchHistory()).Placeholder(locales.SearchHistory()).Margin(0, 0, 8, 0)
	visits := a.historyMatches(a.historyQuery, 200)
	group(c, p, func() {
		if len(visits) == 0 {
			empty := locales.NoHistory()
			if strings.TrimSpace(a.historyQuery) != "" {
				empty = locales.NoHistoryFound()
			}
			ui.Text(c, empty).FontSize(13).TextColor(p.Mute).Padding(14, 12)
			return
		}
		r := rows{p: p}
		for _, v := range visits {
			a.historyRow(c, &r, v)
		}
	})
	if a.clearWebData != nil {
		group(c, p, func() {
			r := rows{p: p}
			r.row(c, "set-web-clear", locales.ClearWebData(), locales.ClearWebDataNote(), func() {
				if a.pushButton(c, "set-clear-web", locales.ClearWebData()+"…").Clicked() {
					a.askClearWeb()
				}
			})
		})
	}

	section(c, p, locales.Downloads(), false)
	group(c, p, func() {
		r := rows{p: p}
		where := set.BrowserDownloads
		if where == "" {
			if d, err := mygo.App.Path(mygo.PathDownloads); err == nil {
				where = d
			} else {
				where = locales.Downloads()
			}
		}
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(where, home+"/") {
			where = "~" + strings.TrimPrefix(where, home)
		}
		r.row(c, "set-dl-dir", locales.DownloadLocation(), where, func() {
			ui.Row(c).Gap(6).Shrink(0).Children(func() {
				if set.BrowserDownloads != "" && a.pushButton(c, "set-dl-default", locales.UseDownloadsFolder()).Clicked() {
					s := set
					s.BrowserDownloads = ""
					a.do(a.C.SaveSettings(s))
				}
				if a.pickDir != nil && a.pushButton(c, "set-dl-change", locales.ChangeFolder()).Clicked() {
					pick, update := a.pickDir, a.update
					save := func(dir string) {
						s := a.settings()
						s.BrowserDownloads = dir
						a.do(a.C.SaveSettings(s))
					}
					if update == nil {
						if dir := pick(); dir != "" {
							save(dir)
						}
					} else {
						go func() {
							if dir := pick(); dir != "" {
								update(func() { save(dir) })
							}
						}()
					}
				}
			})
		})
		r.toggle(c, "set-dl-ask", locales.AskWhereToSave(), set.BrowserAskDownload, false, func(on bool) {
			s := set
			s.BrowserAskDownload = on
			a.do(a.C.SaveSettings(s))
		})
	})
}

// historyRow is a page of the history: clicked, it opens in the browser;
// its × takes it out of the history.
func (a *App) historyRow(c *ui.Context, r *rows, v webVisit) {
	p := a.p
	r.sep(c)
	title := v.Title
	if title == "" {
		title = v.URL
	}
	row := ui.ButtonBase(c.Key("visit:"+v.URL)).Row().FillWidth().MinHeight(46).Padding(7, 8, 7, 12).Gap(10).AlignItems(ui.Center).
		Label(title).Tooltip(v.URL)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	removed := false
	row.Children(func() {
		sym(c, "world", 15, p.Mute)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.Fg).SingleLine().Ellipsis("…")
			ui.Text(c, shortHost(v.URL)+" · "+ago(a.now(), v.At)).FontSize(12).TextColor(p.Mute).SingleLine().Ellipsis("…")
		})
		x := ui.ButtonBase(c.Key("visit-remove:"+v.URL)).Size(24, 24).Radius(6).Center().
			Label(locales.RemoveFromHistory()).Tooltip(locales.RemoveFromHistory())
		if !row.Hovered() && !x.Hovered() {
			x.Opacity(0)
		}
		if x.Hovered() {
			x.Background(p.Chip)
		}
		x.Children(func() { sym(c, "x", 13, p.Mute) })
		if x.Clicked() {
			removed = true
		}
	})
	switch {
	case removed:
		a.removeVisits(v.URL)
	case row.Clicked():
		a.settingsOpen = false
		a.showBrowser()
		a.newWebTab(v.URL)
	}
}

// addressSuggestions are the pages of the history the address field's
// text finds while it is being typed in: none for the page's own address.
func (a *App) addressSuggestions(b *browser, text string, focused bool) []webVisit {
	q := strings.TrimSpace(text)
	if a.webSuggestOff != "" && text != a.webSuggestOff {
		a.webSuggestOff = "" // typed on: they come back
	}
	if !focused || q == "" || a.webSuggestOff != "" || b != nil && text == b.shown {
		return nil
	}
	return a.historyMatches(q, 6)
}

// suggestionList is the list under the address field: each page's title
// and address, the one ↓ picked marked. It returns the page clicked.
func (a *App) suggestionList(c *ui.Context, field ui.Element, text string, sugg []webVisit) string {
	open := len(sugg) > 0
	if !open {
		return ""
	}
	p := a.p
	picked := ""
	pop := ui.Popover(c, field, &open, func() {
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			for i, v := range sugg {
				title := v.Title
				if title == "" {
					title = v.URL
				}
				row := ui.ButtonBase(c.Key("suggest:"+v.URL)).Row().FillWidth().Height(36).Padding(0, 8).Gap(9).Radius(7).
					AlignItems(ui.Center).Label(title).KeepFocus()
				switch {
				case i == a.webSuggest:
					row.Background(p.Sel)
				case row.Hovered():
					row.Background(p.Hover)
				}
				row.Children(func() {
					sym(c, "history", 14, p.Mute)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(p.Fg).SingleLine().Ellipsis("…")
						ui.Text(c, v.URL).FontSize(11.5).TextColor(p.Mute).SingleLine().Ellipsis("…")
					})
				})
				if row.Clicked() {
					picked = v.URL
				}
			}
		})
	})
	pop.AttachTo(field, ui.AnchorBottomStart, ui.AnchorTopStart).Margin(4, 0, 0, 0).
		Width(max(field.Bounds().W, 260)).Background(p.Card).Border(.5, p.Edge2).Radius(10).Padding(4).NoShadow()
	lift(pop, p)
	if !open {
		a.webSuggestOff = text // Escape or a click away: until the next key
	}
	return picked
}
