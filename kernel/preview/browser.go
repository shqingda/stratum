package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shqingda/stratum/proto"
)

// Driver is a browser that a person sees and the agent shares with them:
// the app's own, in its panel, with Safari's tabs. Its methods act on the
// active tab.
type Driver interface {
	// Navigate loads url and returns once the page loaded or failed.
	Navigate(ctx context.Context, url string) error
	// Step goes "back", "forward" or "reload", and waits for the page.
	Step(ctx context.Context, step string) error
	// Eval runs JavaScript in the page as the body of an async function
	// and returns its value, decoded from JSON.
	Eval(ctx context.Context, js string) (any, error)
	// Capture is a PNG of what the page shows.
	Capture(ctx context.Context) ([]byte, error)
	// Tabs lists the tabs; OpenTab opens one in front, with url when it
	// is not empty; SelectTab and CloseTab act on one by its ID.
	Tabs(ctx context.Context) ([]proto.BrowserTab, error)
	OpenTab(ctx context.Context, url string) error
	SelectTab(ctx context.Context, id int) error
	CloseTab(ctx context.Context, id int) error
	// Acting marks the agent at work in the browser, as the panel shows.
	Acting()
}

// ErrNoDriver is returned for actions that need the app's browser, in a
// kernel that runs without one (stratumd).
var ErrNoDriver = errors.New("the interactive browser is in the Stratum app; this kernel has only the headless preview (open, screenshot, console)")

// SetDriver makes d the browser of open and of the page actions.
func (m *Manager) SetDriver(d Driver) {
	m.mu.Lock()
	m.driver = d
	m.mu.Unlock()
}

func (m *Manager) currentDriver() Driver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.driver
}

// Interactive reports whether the page actions have a browser.
func (m *Manager) Interactive() bool { return m.currentDriver() != nil }

// CurrentURL is the address of the page the driver's active tab shows,
// or of the last preview; "" before any.
func (m *Manager) CurrentURL(ctx context.Context) string {
	if d := m.currentDriver(); d != nil {
		tabs, err := d.Tabs(ctx)
		if err != nil {
			return ""
		}
		for _, t := range tabs {
			if t.Active && t.URL != "about:blank" {
				return t.URL
			}
		}
		return ""
	}
	return m.State().URL
}

// Action is one thing the agent does in the browser.
type Action struct {
	Do      string  `json:"action"`
	URL     string  `json:"url,omitempty"`
	Ref     int     `json:"ref,omitempty"`
	Text    string  `json:"text,omitempty"`
	Submit  bool    `json:"submit,omitempty"`
	Keys    string  `json:"keys,omitempty"`
	Dir     string  `json:"direction,omitempty"`
	Script  string  `json:"script,omitempty"`
	Tab     int     `json:"tab,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	// AcceptDialog answers yes to the dialogs the action opens (confirm,
	// prompt with DialogText); by default they are dismissed.
	AcceptDialog bool   `json:"accept_dialog,omitempty"`
	DialogText   string `json:"dialog_text,omitempty"`
}

// Act runs a page action in the driver's browser and returns what the
// model reads.
func (m *Manager) Act(ctx context.Context, a Action) (string, error) {
	d := m.currentDriver()
	if d == nil {
		return "", ErrNoDriver
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	d.Acting()
	// The dialogs the action opens are answered for the agent, not shown.
	arm := fmt.Sprintf("window.__stratumAgent = {accept: %v, text: %s, until: Date.now() + 4000};\n", a.AcceptDialog, quote(a.DialogText))
	do := func(call string) error {
		_, err := evalJS(ctx, d, pageJS+arm+"return __stratum."+call)
		return err
	}
	switch a.Do {
	case "back", "forward", "reload":
		if err := d.Step(ctx, a.Do); err != nil {
			return "", err
		}
		return m.read(ctx, d, false)
	case "read":
		return m.read(ctx, d, true)
	case "click", "hover":
		if err := do(fmt.Sprintf("%s(%d)", a.Do, a.Ref)); err != nil {
			return "", err
		}
		if a.Do == "click" {
			settle(ctx, d)
		}
		return m.read(ctx, d, false)
	case "type":
		if err := do(fmt.Sprintf("type(%d, %s, %v)", a.Ref, quote(a.Text), a.Submit)); err != nil {
			return "", err
		}
		settle(ctx, d)
		return m.read(ctx, d, false)
	case "key":
		k, err := parseKeys(a.Keys)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(k)
		if err := do(fmt.Sprintf("key(%s)", b)); err != nil {
			return "", err
		}
		settle(ctx, d)
		return m.read(ctx, d, false)
	case "scroll":
		if err := do(fmt.Sprintf("scroll(%s, %d)", quote(a.Dir), a.Ref)); err != nil {
			return "", err
		}
		return m.read(ctx, d, false)
	case "wait":
		if err := wait(ctx, d, a.Text, a.Seconds); err != nil {
			return "", err
		}
		return m.read(ctx, d, a.Text != "")
	case "console":
		v, err := evalJS(ctx, d, "return (window.__stratumLog || []).slice(-100)")
		if err != nil {
			return "", err
		}
		return logText(v, "No console messages since the page loaded."), nil
	case "network":
		v, err := evalJS(ctx, d, pageJS+"return __stratum.network()")
		if err != nil {
			return "", err
		}
		s, _ := v.(string)
		if s == "" {
			s = "No requests since the page loaded."
		}
		return s, nil
	case "tabs":
		return m.tabsText(ctx, d)
	case "new_tab":
		target := ""
		if strings.TrimSpace(a.URL) != "" {
			t, err := NormalizeURL(a.URL)
			if err != nil {
				return "", err
			}
			target = t
		}
		if err := d.OpenTab(ctx, target); err != nil {
			return "", err
		}
		if target == "" {
			return m.tabsText(ctx, d)
		}
		settle(ctx, d)
		return m.read(ctx, d, false)
	case "select_tab":
		if err := d.SelectTab(ctx, a.Tab); err != nil {
			return "", err
		}
		return m.read(ctx, d, false)
	case "close_tab":
		if err := d.CloseTab(ctx, a.Tab); err != nil {
			return "", err
		}
		return m.tabsText(ctx, d)
	case "eval":
		if strings.TrimSpace(a.Script) == "" {
			return "", errors.New("browser: eval needs a script")
		}
		v, err := d.Eval(ctx, arm+a.Script)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(v)
		return clip(string(b), 8000), nil
	}
	return "", fmt.Errorf("browser: unknown action %q", a.Do)
}

func (m *Manager) tabsText(ctx context.Context, d Driver) (string, error) {
	tabs, err := d.Tabs(ctx)
	if err != nil {
		return "", err
	}
	if len(tabs) == 0 {
		return "No tabs are open.", nil
	}
	var b strings.Builder
	b.WriteString("Tabs (use tab with select_tab and close_tab):")
	for _, t := range tabs {
		mark := " "
		if t.Active {
			mark = "*"
		}
		fmt.Fprintf(&b, "\n%s [%d] %s — %s", mark, t.ID, t.Title, t.URL)
	}
	return b.String(), nil
}

// openWithDriver loads target in the driver's browser and makes the
// snapshot of the page it shows.
func (m *Manager) openWithDriver(ctx context.Context, d Driver, target string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d.Acting()
	snap := Snapshot{URL: target}
	err := d.Navigate(ctx, target)
	if err == nil {
		settle(ctx, d)
	}
	if v, e := evalJS(ctx, d, pageJS+"return __stratum.state()"); e == nil {
		st := asMap(v)
		snap.URL, _ = st["url"].(string)
		snap.Title, _ = st["title"].(string)
		if n, ok := st["status"].(float64); ok {
			snap.Status = int(n)
		}
		snap.Console, snap.Network = logEntries(st["log"])
	}
	if png, e := d.Capture(ctx); e == nil {
		snap.Screenshot = png
	}
	if snap.URL == "" {
		snap.URL = target
	}
	return snap, err
}

// Summary is what the model reads after open: the page's state, its
// console errors and what it can act on.
func (m *Manager) Summary(ctx context.Context) (string, error) {
	d := m.currentDriver()
	if d == nil {
		return "", ErrNoDriver
	}
	return m.read(ctx, d, false)
}

// Capture is a PNG of the page the driver's browser shows.
func (m *Manager) Capture(ctx context.Context) ([]byte, error) {
	d := m.currentDriver()
	if d == nil {
		return nil, ErrNoDriver
	}
	d.Acting()
	return d.Capture(ctx)
}

// read is the page's address and title, its errors and dialogs, and the
// elements it can act on; with text, its visible text too.
func (m *Manager) read(ctx context.Context, d Driver, text bool) (string, error) {
	v, err := evalJS(ctx, d, pageJS+fmt.Sprintf("return __stratum.read(%v)", text))
	if err != nil {
		return "", err
	}
	st := asMap(v)
	var b strings.Builder
	title, _ := st["title"].(string)
	u, _ := st["url"].(string)
	fmt.Fprintf(&b, "Page: %s\nURL: %s", title, u)
	if n, ok := st["status"].(float64); ok && n > 0 {
		fmt.Fprintf(&b, " [HTTP %d]", int(n))
	}
	if tabs, err := d.Tabs(ctx); err == nil && len(tabs) > 1 {
		fmt.Fprintf(&b, "\n%d tabs are open (tabs lists them)", len(tabs))
	}
	cons, nets := logEntries(st["log"])
	for _, c := range cons {
		fmt.Fprintf(&b, "\n%s: %s", label(c.Level), c.Text)
		if c.Source != "" {
			fmt.Fprintf(&b, " (%s:%d)", c.Source, c.Line)
		}
	}
	for _, n := range nets {
		fmt.Fprintf(&b, "\nNetwork: %s", n.Error)
	}
	if s, _ := st["text"].(string); text && s != "" {
		b.WriteString("\n\nText:\n" + clip(s, 6000))
	}
	if s, _ := st["elements"].(string); s != "" {
		b.WriteString("\n\nElements (use ref with click, hover, type, scroll):\n" + s)
	} else {
		b.WriteString("\n\nElements: none visible")
	}
	return b.String(), nil
}

func label(level string) string {
	if level == "dialog" {
		return "Dialog"
	}
	return "Console " + level
}

// settle waits a moment for what an action starts: a navigation, or a
// page's script answering it.
func settle(ctx context.Context, d Driver) {
	end := time.Now().Add(10 * time.Second)
	time.Sleep(250 * time.Millisecond)
	for time.Now().Before(end) && ctx.Err() == nil {
		v, err := d.Eval(ctx, "return document.readyState")
		if s, _ := v.(string); err == nil && s == "complete" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// wait waits for text to show on the page, or for seconds without text.
func wait(ctx context.Context, d Driver, text string, seconds float64) error {
	if seconds <= 0 {
		seconds = 10
		if text == "" {
			seconds = 1
		}
	}
	seconds = min(seconds, 30)
	end := time.Now().Add(time.Duration(seconds * float64(time.Second)))
	for {
		if text != "" {
			v, err := d.Eval(ctx, "return document.body ? document.body.innerText : ''")
			if s, _ := v.(string); err == nil && strings.Contains(s, text) {
				return nil
			}
		}
		if time.Now().After(end) {
			if text != "" {
				return fmt.Errorf("browser: %q did not show in %gs", text, seconds)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// pageKey is a key press as the page script plays it.
type pageKey struct {
	Key   string `json:"key"`
	Code  string `json:"code"`
	Shift bool   `json:"shift"`
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Meta  bool   `json:"meta"`
}

var keyNames = map[string][2]string{
	"enter": {"Enter", "Enter"}, "return": {"Enter", "Enter"}, "tab": {"Tab", "Tab"}, "escape": {"Escape", "Escape"}, "esc": {"Escape", "Escape"},
	"backspace": {"Backspace", "Backspace"}, "delete": {"Delete", "Delete"}, "space": {" ", "Space"},
	"up": {"ArrowUp", "ArrowUp"}, "down": {"ArrowDown", "ArrowDown"}, "left": {"ArrowLeft", "ArrowLeft"}, "right": {"ArrowRight", "ArrowRight"},
	"arrowup": {"ArrowUp", "ArrowUp"}, "arrowdown": {"ArrowDown", "ArrowDown"}, "arrowleft": {"ArrowLeft", "ArrowLeft"}, "arrowright": {"ArrowRight", "ArrowRight"},
	"home": {"Home", "Home"}, "end": {"End", "End"}, "pageup": {"PageUp", "PageUp"}, "pagedown": {"PageDown", "PageDown"},
}

// parseKeys reads "Enter", "shift+tab" or "cmd+a" as a key press.
func parseKeys(s string) (pageKey, error) {
	var k pageKey
	parts := strings.Split(strings.TrimSpace(s), "+")
	for i, p := range parts {
		lp := strings.ToLower(strings.TrimSpace(p))
		if i < len(parts)-1 {
			switch lp {
			case "shift":
				k.Shift = true
			case "ctrl", "control":
				k.Ctrl = true
			case "alt", "option":
				k.Alt = true
			case "cmd", "command", "meta":
				k.Meta = true
			default:
				return k, fmt.Errorf("browser: unknown modifier %q in %q", p, s)
			}
			continue
		}
		if n, ok := keyNames[lp]; ok {
			k.Key, k.Code = n[0], n[1]
		} else if r := []rune(strings.TrimSpace(p)); len(r) == 1 {
			k.Key = string(r)
			if r[0] >= 'a' && r[0] <= 'z' || r[0] >= 'A' && r[0] <= 'Z' {
				k.Code = "Key" + strings.ToUpper(string(r))
			}
		} else if len(lp) >= 2 && lp[0] == 'f' {
			k.Key, k.Code = strings.ToUpper(lp), strings.ToUpper(lp)
		} else {
			return k, fmt.Errorf("browser: unknown key %q in %q", p, s)
		}
	}
	return k, nil
}

func evalJS(ctx context.Context, d Driver, js string) (any, error) {
	v, err := d.Eval(ctx, js)
	if err != nil {
		return nil, err
	}
	if m := asMap(v); m != nil {
		if e, _ := m["error"].(string); e != "" {
			return nil, errors.New("browser: " + e)
		}
	}
	return v, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// logEntries splits the page's log into console messages and dialogs, and
// failed requests; read shows errors, warnings and dialogs only.
func logEntries(v any) (cons []ConsoleEntry, nets []NetworkEntry) {
	list, _ := v.([]any)
	for _, it := range list {
		e := asMap(it)
		level, _ := e["level"].(string)
		text, _ := e["text"].(string)
		switch level {
		case "network":
			nets = append(nets, NetworkEntry{Error: text})
			continue
		case "error", "warn", "dialog":
		default:
			continue
		}
		src, _ := e["source"].(string)
		line, _ := e["line"].(float64)
		cons = append(cons, ConsoleEntry{Level: level, Text: text, Source: src, Line: int(line)})
	}
	return
}

// logText is the whole log, every level, for the console action.
func logText(v any, none string) string {
	list, _ := v.([]any)
	if len(list) == 0 {
		return none
	}
	var b strings.Builder
	for i, it := range list {
		e := asMap(it)
		level, _ := e["level"].(string)
		text, _ := e["text"].(string)
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[%s] %s", level, text)
		if src, _ := e["source"].(string); src != "" {
			line, _ := e["line"].(float64)
			fmt.Fprintf(&b, " (%s:%d)", src, int(line))
		}
	}
	return b.String()
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// IsLocalPage is IsLocalURL for an address the browser shows, where ""
// and about:blank are no site at all.
func IsLocalPage(raw string) bool {
	if raw == "" || strings.HasPrefix(raw, "about:") {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "file" || IsLocalURL(raw))
}

// LogScript is the preload script of the panel's pages: it keeps the
// console's messages, uncaught errors and failed loads and requests for
// read and console, and answers the dialogs that the agent's actions
// open, which would otherwise wait for the user.
const LogScript = `(() => {
if (window.__stratumLog) return;
const log = window.__stratumLog = [];
const push = (level, text, source, line) => { if (log.length >= 300) log.splice(0, 100); log.push({level, text: String(text).slice(0, 500), source: source || '', line: line || 0}); };
const show = x => { try { return typeof x === 'string' ? x : x instanceof Error ? x.message : JSON.stringify(x); } catch { return String(x); } };
for (const level of ['error', 'warn', 'info', 'log', 'debug']) {
  const f = console[level];
  console[level] = function (...a) { push(level, a.map(show).join(' ')); return f.apply(this, a); };
}
addEventListener('error', e => {
  const t = e.target;
  if (t && t !== window && (t.src || t.href)) push('network', 'failed to load ' + (t.src || t.href));
  else if (e.filename || e.message !== 'Script error.') push('error', e.message, e.filename, e.lineno);
}, true);
addEventListener('unhandledrejection', e => push('error', 'Unhandled rejection: ' + show(e.reason && e.reason.message || e.reason)));
const f = window.fetch;
if (f) window.fetch = function (...a) {
  const what = a[0] && a[0].url || String(a[0]);
  return f.apply(this, a).then(r => { if (!r.ok) push('network', r.status + ' ' + (r.url || what)); return r; }, e => { push('network', 'failed ' + what + ': ' + show(e)); throw e; });
};
const XO = XMLHttpRequest.prototype.open, XS = XMLHttpRequest.prototype.send;
XMLHttpRequest.prototype.open = function (m, u) { this.__stratum = m + ' ' + u; return XO.apply(this, arguments); };
XMLHttpRequest.prototype.send = function () {
  this.addEventListener('loadend', () => { if (this.status === 0 || this.status >= 400) push('network', (this.status || 'failed') + ' ' + this.__stratum); });
  return XS.apply(this, arguments);
};
const agent = () => { const a = window.__stratumAgent; return a && a.until > Date.now() ? a : null; };
const {alert, confirm, prompt} = window;
window.alert = function (m) { if (agent()) { push('dialog', 'alert("' + m + '") was closed'); return; } return alert.call(window, m); };
window.confirm = function (m) {
  const a = agent();
  if (!a) return confirm.call(window, m);
  push('dialog', 'confirm("' + m + '") was ' + (a.accept ? 'accepted' : 'dismissed (pass accept_dialog to accept)'));
  return !!a.accept;
};
window.prompt = function (m, d) {
  const a = agent();
  if (!a) return prompt.call(window, m, d);
  const v = a.accept ? (a.text || d || '') : null;
  push('dialog', 'prompt("' + m + '") was ' + (v === null ? 'dismissed (pass accept_dialog and dialog_text to answer)' : 'answered ' + JSON.stringify(v)));
  return v;
};
})();`

// pageJS defines __stratum in the page: numbered refs on the elements a
// person acts on, and what read, click, hover, type, key and scroll do
// with them. The element an action is on flashes, for the user to see.
const pageJS = `const __stratum = (() => {
const visible = el => {
  const r = el.getBoundingClientRect();
  if (r.width < 1 || r.height < 1) return false;
  const s = getComputedStyle(el);
  return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
};
const name = el => {
  const t = s => (s || '').replace(/\s+/g, ' ').trim();
  let n = t(el.getAttribute('aria-label'));
  const by = el.getAttribute('aria-labelledby');
  if (!n && by) n = t(by.split(/\s+/).map(id => (document.getElementById(id) || {}).innerText || '').join(' '));
  if (!n && el.labels && el.labels.length) n = t(el.labels[0].innerText);
  if (!n && el.tagName !== 'INPUT' && el.tagName !== 'TEXTAREA' && el.tagName !== 'SELECT') n = t(el.innerText);
  if (!n) n = t(el.title || el.alt || el.placeholder || el.name || (el.querySelector('img[alt]') || {}).alt);
  return n.length > 80 ? n.slice(0, 80) + '…' : n;
};
const role = el => {
  const r = el.getAttribute('role');
  if (r) return r;
  const tag = el.tagName.toLowerCase();
  if (tag === 'a') return 'link';
  if (tag === 'input') return 'input[' + (el.type || 'text') + ']';
  if (el.isContentEditable) return 'textbox';
  return tag;
};
const sel = 'a[href], button, input:not([type=hidden]), textarea, select, summary, [role=button], [role=link], [role=checkbox], [role=radio], [role=tab], [role=menuitem], [role=option], [role=switch], [role=textbox], [role=combobox], [role=slider], [contenteditable=""], [contenteditable=true], [onclick], [tabindex]:not([tabindex="-1"])';
const byRef = ref => {
  const el = document.querySelector('[data-stratum-ref="' + ref + '"]');
  if (!el) throw new Error('no element with ref ' + ref + ' on the page; read it again for fresh refs');
  return el;
};
const flash = el => {
  const r = el.getBoundingClientRect(), d = document.createElement('div');
  d.style.cssText = 'position:fixed;z-index:2147483647;pointer-events:none;border:2px solid #0A84FF;border-radius:5px;background:rgba(10,132,255,.12);transition:opacity .4s;left:' + (r.left - 3) + 'px;top:' + (r.top - 3) + 'px;width:' + (r.width + 2) + 'px;height:' + (r.height + 2) + 'px';
  document.documentElement.appendChild(d);
  setTimeout(() => { d.style.opacity = '0'; setTimeout(() => d.remove(), 450); }, 600);
};
const point = el => {
  el.scrollIntoView({block: 'center', inline: 'center'});
  const r = el.getBoundingClientRect();
  return {bubbles: true, cancelable: true, composed: true, view: window, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, button: 0};
};
const focusable = () => [...document.querySelectorAll('a[href], button, input:not([type=hidden]), textarea, select, summary, [contenteditable=""], [contenteditable=true], [tabindex]:not([tabindex="-1"])')].filter(el => !el.disabled && visible(el));
const log = () => (window.__stratumLog || []).filter(e => e.level !== 'log' && e.level !== 'info' && e.level !== 'debug').slice(-30);
const status = () => { const n = performance.getEntriesByType('navigation')[0]; return n && n.responseStatus || 0; };
return {
  state() { return {url: location.href, title: document.title, status: status(), log: log()}; },
  read(text) {
    let next = window.__stratumNext || 1;
    const lines = [];
    for (const el of document.querySelectorAll(sel)) {
      if (!visible(el) || el.closest('[aria-hidden=true]') || el.disabled) continue;
      let ref = el.getAttribute('data-stratum-ref');
      if (!ref) { ref = String(next++); el.setAttribute('data-stratum-ref', ref); }
      let line = '[' + ref + '] ' + role(el);
      const n = name(el);
      if (n) line += ' "' + n + '"';
      if ('value' in el && el.value && el.tagName !== 'BUTTON' && el.type !== 'checkbox' && el.type !== 'radio') line += ' value="' + String(el.value).slice(0, 60) + '"';
      if (el.type === 'checkbox' || el.type === 'radio') line += el.checked ? ' checked' : ' unchecked';
      if (el.getAttribute('aria-expanded')) line += el.getAttribute('aria-expanded') === 'true' ? ' expanded' : ' collapsed';
      if (el.getAttribute('aria-selected') === 'true') line += ' selected';
      if (el === document.activeElement) line += ' focused';
      if (el.tagName === 'A') line += ' → ' + (el.getAttribute('href') || '').slice(0, 100);
      if (lines.length < 250) lines.push(line);
    }
    window.__stratumNext = next;
    const s = this.state();
    s.elements = lines.join('\n');
    if (text) s.text = (document.body ? document.body.innerText : '').replace(/\n{3,}/g, '\n\n').trim();
    return s;
  },
  click(ref) {
    const el = byRef(ref), o = point(el);
    flash(el);
    el.dispatchEvent(new PointerEvent('pointerdown', o));
    el.dispatchEvent(new MouseEvent('mousedown', o));
    if (el.focus) el.focus();
    el.dispatchEvent(new PointerEvent('pointerup', o));
    el.dispatchEvent(new MouseEvent('mouseup', o));
    el.click();
    return true;
  },
  hover(ref) {
    const el = byRef(ref), o = point(el);
    flash(el);
    for (const t of ['pointerover', 'pointerenter', 'mouseover', 'mouseenter', 'pointermove', 'mousemove'])
      el.dispatchEvent(t.startsWith('pointer') ? new PointerEvent(t, o) : new MouseEvent(t, o));
    return true;
  },
  type(ref, text, submit) {
    const el = byRef(ref);
    el.scrollIntoView({block: 'center'});
    flash(el);
    el.focus();
    if (el.tagName === 'SELECT') {
      const o = [...el.options].find(o => o.value === text || o.text.trim() === text);
      if (!o) throw new Error('no option "' + text + '" in ref ' + ref + '; its options: ' + [...el.options].map(o => o.text.trim()).join(', '));
      el.value = o.value;
    } else if (el.isContentEditable) {
      document.execCommand('selectAll');
      document.execCommand('insertText', false, text);
    } else if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
      const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, text);
      el.dispatchEvent(new InputEvent('input', {bubbles: true, data: text, inputType: 'insertText'}));
    } else {
      throw new Error('ref ' + ref + ' is a ' + role(el) + ', not a field: click it, or type into a field');
    }
    el.dispatchEvent(new Event('change', {bubbles: true}));
    if (submit) this.key({key: 'Enter', code: 'Enter'});
    return true;
  },
  // key plays a key press on the focused element, with what a browser
  // does by default for the keys a page leaves alone: Enter submits a
  // form or presses a button, Tab moves the focus, the arrows and the
  // page keys scroll.
  key(k) {
    const el = document.activeElement || document.body;
    const o = {key: k.key, code: k.code || '', shiftKey: !!k.shift, ctrlKey: !!k.ctrl, altKey: !!k.alt, metaKey: !!k.meta, bubbles: true, cancelable: true, composed: true};
    if (k.key === 'Enter') { o.keyCode = o.which = 13; }
    if (k.key === 'Escape') { o.keyCode = o.which = 27; }
    if (k.key === 'Tab') { o.keyCode = o.which = 9; }
    const go = el.dispatchEvent(new KeyboardEvent('keydown', o));
    if (k.key.length === 1 && !k.ctrl && !k.meta) el.dispatchEvent(new KeyboardEvent('keypress', o));
    if (go) {
      const field = el.tagName === 'INPUT' || el.tagName === 'TEXTAREA';
      if (k.key === 'Enter' && el.tagName === 'TEXTAREA') {
        el.setRangeText('\n', el.selectionStart, el.selectionEnd, 'end');
        el.dispatchEvent(new InputEvent('input', {bubbles: true, inputType: 'insertLineBreak'}));
      } else if (k.key === 'Enter' && el.tagName === 'INPUT' && el.form) {
        el.form.requestSubmit ? el.form.requestSubmit() : el.form.submit();
      } else if ((k.key === 'Enter' || k.key === ' ') && (el.tagName === 'BUTTON' || el.tagName === 'A' || el.getAttribute('role') === 'button')) {
        el.click();
      } else if (k.key === 'Tab') {
        const all = focusable(), i = all.indexOf(el), n = all.length;
        const to = n ? all[((i < 0 ? (k.shift ? 0 : -1) : i) + (k.shift ? -1 : 1) + n) % n] : null;
        if (to) { to.focus(); flash(to); }
      } else if (k.key.length === 1 && field && !k.ctrl && !k.meta) {
        el.setRangeText(k.key, el.selectionStart, el.selectionEnd, 'end');
        el.dispatchEvent(new InputEvent('input', {bubbles: true, data: k.key, inputType: 'insertText'}));
      } else if (k.key === 'Backspace' && field && (el.selectionStart > 0 || el.selectionEnd > el.selectionStart)) {
        const s = el.selectionStart === el.selectionEnd ? el.selectionStart - 1 : el.selectionStart;
        el.setRangeText('', s, el.selectionEnd, 'end');
        el.dispatchEvent(new InputEvent('input', {bubbles: true, inputType: 'deleteContentBackward'}));
      } else if (!field) {
        const h = innerHeight * 0.8, dy = {ArrowDown: 40, ArrowUp: -40, PageDown: h, PageUp: -h, ' ': h}[k.key];
        if (dy) scrollBy(0, k.shift && k.key === ' ' ? -dy : dy);
        if (k.key === 'Home') scrollTo(0, 0);
        if (k.key === 'End') scrollTo(0, document.documentElement.scrollHeight);
      }
    }
    el.dispatchEvent(new KeyboardEvent('keyup', o));
    return true;
  },
  scroll(dir, ref) {
    if (ref) { const el = byRef(ref); el.scrollIntoView({block: 'center'}); flash(el); return true; }
    const h = innerHeight * 0.8;
    scrollBy(0, dir === 'up' ? -h : h);
    return true;
  },
  network() {
    const out = [];
    for (const e of performance.getEntriesByType('resource').slice(-80)) {
      const st = e.responseStatus ? ' ' + e.responseStatus : '';
      out.push((e.initiatorType || 'other') + st + ' ' + e.name.slice(0, 160) + ' (' + Math.round(e.duration) + ' ms)');
    }
    for (const e of (window.__stratumLog || []).filter(e => e.level === 'network').slice(-20)) out.push('FAILED ' + e.text);
    return out.join('\n');
  },
};
})();
`
