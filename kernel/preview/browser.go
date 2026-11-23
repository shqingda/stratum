package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Driver is a browser that a person sees and the agent shares with them:
// the app's own, in its panel. Its methods take and give plain values, so
// the app implements it without this package.
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

// CurrentURL is the address of the page the driver shows, or of the last
// preview; "" before any.
func (m *Manager) CurrentURL(ctx context.Context) string {
	if d := m.currentDriver(); d != nil {
		if v, err := d.Eval(ctx, "return location.href"); err == nil {
			if s, ok := v.(string); ok && s != "about:blank" {
				return s
			}
		}
		return ""
	}
	return m.State().URL
}

// Action is one thing the agent does in the browser.
type Action struct {
	Do     string `json:"action"`
	URL    string `json:"url,omitempty"`
	Ref    int    `json:"ref,omitempty"`
	Text   string `json:"text,omitempty"`
	Submit bool   `json:"submit,omitempty"`
	Dir    string `json:"direction,omitempty"`
	Script string `json:"script,omitempty"`
}

// Act runs a page action in the driver's browser and returns what the
// model reads.
func (m *Manager) Act(ctx context.Context, a Action) (string, error) {
	d := m.currentDriver()
	if d == nil {
		return "", ErrNoDriver
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch a.Do {
	case "back", "forward", "reload":
		if err := d.Step(ctx, a.Do); err != nil {
			return "", err
		}
		return m.read(ctx, d, false)
	case "read":
		return m.read(ctx, d, true)
	case "click":
		if _, err := evalJS(ctx, d, pageJS+fmt.Sprintf("return __stratum.click(%d)", a.Ref)); err != nil {
			return "", err
		}
		settle(ctx, d)
		return m.read(ctx, d, false)
	case "type":
		js := fmt.Sprintf("return __stratum.type(%d, %s, %v)", a.Ref, quote(a.Text), a.Submit)
		if _, err := evalJS(ctx, d, pageJS+js); err != nil {
			return "", err
		}
		settle(ctx, d)
		return m.read(ctx, d, false)
	case "scroll":
		js := fmt.Sprintf("return __stratum.scroll(%s, %d)", quote(a.Dir), a.Ref)
		if _, err := evalJS(ctx, d, pageJS+js); err != nil {
			return "", err
		}
		return m.read(ctx, d, false)
	case "eval":
		if strings.TrimSpace(a.Script) == "" {
			return "", errors.New("browser: eval needs a script")
		}
		v, err := d.Eval(ctx, a.Script)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(v)
		return clip(string(b), 8000), nil
	}
	return "", fmt.Errorf("browser: unknown action %q", a.Do)
}

// openWithDriver loads target in the driver's browser and makes the
// snapshot of the page it shows.
func (m *Manager) openWithDriver(ctx context.Context, d Driver, target string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
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

// read is the page's address and title, its console errors and the
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
	cons, nets := logEntries(st["log"])
	for _, c := range cons {
		fmt.Fprintf(&b, "\nConsole %s: %s", c.Level, c.Text)
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
		b.WriteString("\n\nElements (use ref with click, type, scroll):\n" + s)
	} else {
		b.WriteString("\n\nElements: none visible")
	}
	return b.String(), nil
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

func logEntries(v any) (cons []ConsoleEntry, nets []NetworkEntry) {
	list, _ := v.([]any)
	for _, it := range list {
		e := asMap(it)
		level, _ := e["level"].(string)
		text, _ := e["text"].(string)
		if level == "network" {
			nets = append(nets, NetworkEntry{Error: text})
			continue
		}
		src, _ := e["source"].(string)
		line, _ := e["line"].(float64)
		cons = append(cons, ConsoleEntry{Level: level, Text: text, Source: src, Line: int(line)})
	}
	return
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

// LogScript collects a page's console errors and warnings, its uncaught
// errors and its failed loads and requests, for read and console: a
// preload script, before the page's own.
const LogScript = `(() => {
if (window.__stratumLog) return;
const log = window.__stratumLog = [];
const push = (level, text, source, line) => { if (log.length < 200) log.push({level, text: String(text).slice(0, 500), source: source || '', line: line || 0}); };
const show = x => { try { return typeof x === 'string' ? x : x instanceof Error ? x.message : JSON.stringify(x); } catch { return String(x); } };
for (const level of ['error', 'warn']) {
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
})();`

// pageJS defines __stratum in the page: numbered refs on the elements a
// person acts on, and what read, click, type and scroll do with them.
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
const sel = 'a[href], button, input:not([type=hidden]), textarea, select, summary, [role=button], [role=link], [role=checkbox], [role=radio], [role=tab], [role=menuitem], [role=option], [role=switch], [role=textbox], [role=combobox], [contenteditable=""], [contenteditable=true], [onclick], [tabindex]:not([tabindex="-1"])';
const byRef = ref => {
  const el = document.querySelector('[data-stratum-ref="' + ref + '"]');
  if (!el) throw new Error('no element with ref ' + ref + ' on the page; read it again for fresh refs');
  return el;
};
const log = () => (window.__stratumLog || []).slice(-30);
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
      if ('value' in el && el.value && el.tagName !== 'BUTTON') line += ' value="' + String(el.value).slice(0, 60) + '"';
      if (el.type === 'checkbox' || el.type === 'radio') line += el.checked ? ' checked' : ' unchecked';
      if (el.tagName === 'A') line += ' → ' + el.getAttribute('href').slice(0, 100);
      if (lines.length < 200) lines.push(line);
    }
    window.__stratumNext = next;
    const s = this.state();
    s.elements = lines.join('\n');
    if (text) s.text = (document.body ? document.body.innerText : '').replace(/\n{3,}/g, '\n\n').trim();
    return s;
  },
  click(ref) {
    const el = byRef(ref);
    el.scrollIntoView({block: 'center', inline: 'center'});
    const r = el.getBoundingClientRect(), x = r.left + r.width / 2, y = r.top + r.height / 2;
    const o = {bubbles: true, cancelable: true, view: window, clientX: x, clientY: y, button: 0};
    el.dispatchEvent(new PointerEvent('pointerdown', o));
    el.dispatchEvent(new MouseEvent('mousedown', o));
    if (el.focus) el.focus();
    el.dispatchEvent(new PointerEvent('pointerup', o));
    el.dispatchEvent(new MouseEvent('mouseup', o));
    el.click();
    return true;
  },
  type(ref, text, submit) {
    const el = byRef(ref);
    el.scrollIntoView({block: 'center'});
    el.focus();
    if (el.tagName === 'SELECT') {
      const o = [...el.options].find(o => o.value === text || o.text.trim() === text);
      if (!o) throw new Error('no option "' + text + '" in ref ' + ref);
      el.value = o.value;
    } else if (el.isContentEditable) {
      document.execCommand('selectAll');
      document.execCommand('insertText', false, text);
    } else {
      const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, text);
      el.dispatchEvent(new InputEvent('input', {bubbles: true, data: text, inputType: 'insertText'}));
    }
    el.dispatchEvent(new Event('change', {bubbles: true}));
    if (submit) {
      const k = {key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true};
      const go = el.dispatchEvent(new KeyboardEvent('keydown', k));
      el.dispatchEvent(new KeyboardEvent('keypress', k));
      el.dispatchEvent(new KeyboardEvent('keyup', k));
      if (go && el.form) el.form.requestSubmit ? el.form.requestSubmit() : el.form.submit();
    }
    return true;
  },
  scroll(dir, ref) {
    if (ref) { byRef(ref).scrollIntoView({block: 'center'}); return true; }
    const h = innerHeight * 0.8;
    scrollBy(0, dir === 'up' ? -h : h);
    return true;
  },
};
})();
`

// Capture is a PNG of the page the driver's browser shows.
func (m *Manager) Capture(ctx context.Context) ([]byte, error) {
	d := m.currentDriver()
	if d == nil {
		return nil, ErrNoDriver
	}
	return d.Capture(ctx)
}
