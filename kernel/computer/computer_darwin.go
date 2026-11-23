package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func system() Desktop { return &mac{} }

// mac drives macOS with CoreGraphics events from JavaScript for
// Automation, and screencapture: Go without cgo.
type mac struct{}

var perms func() []string

// SetPermissions sets how the app checks its permissions, as
// permissionflow does: JavaScript sees only Accessibility.
func SetPermissions(check func() []string) {
	mu.Lock()
	perms = check
	mu.Unlock()
}

func (m *mac) jxa(ctx context.Context, args any, body string) (string, error) {
	script, err := jxaScript(args, body)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("computer: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// jxaScript is body after the helpers it calls, with its arguments as A.
func jxaScript(args any, body string) (string, error) {
	a, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return `ObjC.import("CoreGraphics"); ObjC.import("ApplicationServices"); ObjC.import("AppKit");
var A = ` + string(a) + `;
function post(e) { $.CGEventPost(0, e); }
function mouse(type, x, y, button, clicks) {
  var e = $.CGEventCreateMouseEvent(null, type, {x: x, y: y}, button);
  if (clicks) $.CGEventSetIntegerValueField(e, 1, clicks);
  post(e);
}
function app(r) {
  if (!r || r.isNil()) return null;
  var u = r.bundleURL, n = ObjC.unwrap(r.localizedName) || "";
  if (!u.isNil()) n = ObjC.unwrap($.NSFileManager.defaultManager.displayNameAtPath(u.path)).replace(/\.app$/, "");
  return {name: n, id: ObjC.unwrap(r.bundleIdentifier) || "", self: r.processIdentifier == A.self};
}
function key(code, flags, down) {
  var e = $.CGEventCreateKeyboardEvent(null, code, down);
  $.CGEventSetFlags(e, flags);
  post(e);
}
` + body, nil
}

func (m *mac) Displays(ctx context.Context) ([]Display, error) {
	out, err := m.jxa(ctx, nil, displaysJS)
	if err != nil {
		return nil, err
	}
	var ds []Display
	if err := json.Unmarshal([]byte(out), &ds); err != nil || len(ds) == 0 {
		return nil, fmt.Errorf("computer: displays %q", out)
	}
	slices.SortStableFunc(ds, func(a, b Display) int {
		switch {
		case a.Main == b.Main:
			return 0
		case a.Main:
			return -1
		}
		return 1
	})
	return ds, nil
}

func (m *mac) Missing(ctx context.Context) []string {
	mu.Lock()
	check := perms
	mu.Unlock()
	if check != nil {
		return check()
	}
	if out, err := m.jxa(ctx, nil, `"" + $.AXIsProcessTrusted()`); err != nil || out != "true" {
		return []string{"accessibility"}
	}
	return nil
}

func (m *mac) Screenshot(ctx context.Context, d Display, w, h int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "stratum-screen-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	raw, small := filepath.Join(dir, "screen.png"), filepath.Join(dir, "screen.jpg")
	rect := fmt.Sprintf("-R%g,%g,%g,%g", d.X, d.Y, d.W, d.H)
	if out, err := exec.CommandContext(ctx, "screencapture", "-x", rect, "-t", "png", raw).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("computer: screencapture: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "sips", "-z", strconv.Itoa(h), strconv.Itoa(w), "-s", "format", "jpeg", "-s", "formatOptions", "80", raw, "--out", small).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("computer: sips: %s", strings.TrimSpace(string(out)))
	}
	return os.ReadFile(small)
}

var buttons = map[string][3]int{"left": {1, 2, 0}, "right": {3, 4, 1}, "middle": {25, 26, 2}}

func (m *mac) Click(ctx context.Context, x, y float64, button string, count int) error {
	b, ok := buttons[button]
	if !ok {
		return fmt.Errorf("computer: unknown button %q", button)
	}
	_, err := m.jxa(ctx, map[string]any{"x": x, "y": y, "down": b[0], "up": b[1], "button": b[2], "count": max(1, count)}, clickJS)
	return err
}

func (m *mac) Move(ctx context.Context, x, y float64) error {
	_, err := m.jxa(ctx, map[string]any{"x": x, "y": y}, `mouse(5, A.x, A.y, 0);`)
	return err
}

func (m *mac) Drag(ctx context.Context, x, y, toX, toY float64) error {
	_, err := m.jxa(ctx, map[string]any{"x": x, "y": y, "tx": toX, "ty": toY}, dragJS)
	return err
}

// Type pastes text, keeping what the clipboard held: keystrokes would go
// through the input method, which turns Latin letters into pinyin.
func (m *mac) Type(ctx context.Context, text string) error {
	_, err := m.jxa(ctx, map[string]any{"text": text, "v": KeyCodes["v"]}, typeJS)
	return err
}

func (m *mac) Key(ctx context.Context, k Key) error {
	flags := 0
	for _, f := range []struct {
		on   bool
		mask int
	}{{k.Shift, 1 << 17}, {k.Control, 1 << 18}, {k.Option, 1 << 19}, {k.Cmd, 1 << 20}, {k.Fn, 1 << 23}} {
		if f.on {
			flags |= f.mask
		}
	}
	_, err := m.jxa(ctx, map[string]any{"code": KeyCodes[k.Name], "flags": flags}, `key(A.code, A.flags, true); key(A.code, A.flags, false);`)
	return err
}

func (m *mac) Scroll(ctx context.Context, x, y float64, dx, dy int) error {
	_, err := m.jxa(ctx, map[string]any{"x": x, "y": y, "dx": dx, "dy": dy}, scrollJS)
	return err
}

func (m *mac) Open(ctx context.Context, app App) error {
	args := []string{"-a", app.Name}
	if app.ID != "" {
		args = []string{"-b", app.ID}
	}
	if out, err := exec.CommandContext(ctx, "open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("computer: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *mac) FindApp(ctx context.Context, name string) (App, error) {
	var a App
	out, err := m.jxa(ctx, map[string]any{"name": name, "self": os.Getpid()}, findAppJS)
	if err == nil && out != "null" {
		err = json.Unmarshal([]byte(out), &a)
	}
	if err != nil {
		return a, err
	}
	if out == "null" {
		return a, fmt.Errorf("computer: there is no app named %q: use its name as Finder shows it, such as \"Google Chrome\"", name)
	}
	return a, nil
}

func (m *mac) FrontApp(ctx context.Context) (App, error) {
	var a App
	out, err := m.jxa(ctx, map[string]any{"self": os.Getpid()}, `JSON.stringify(app($.NSWorkspace.sharedWorkspace.frontmostApplication))`)
	if err == nil {
		err = json.Unmarshal([]byte(out), &a)
	}
	if err == nil && a.Name == "" {
		err = errors.New("computer: no app is in front")
	}
	return a, err
}

func (m *mac) AppAt(ctx context.Context, x, y float64) (Target, error) {
	var t Target
	out, err := m.jxa(ctx, map[string]any{"x": x, "y": y, "self": os.Getpid()}, appAtJS)
	if err == nil {
		err = json.Unmarshal([]byte(out), &t)
	}
	if err == nil && t.Name == "" {
		err = fmt.Errorf("computer: no app at %.0f, %.0f", x, y)
	}
	return t, err
}

// The scripts of the actions, after jxaScript's helpers.
const (
	clickJS = `
mouse(5, A.x, A.y, 0);
delay(0.05);
for (var i = 1; i <= A.count; i++) { mouse(A.down, A.x, A.y, A.button, i); mouse(A.up, A.x, A.y, A.button, i); }`
	dragJS = `
mouse(5, A.x, A.y, 0); delay(0.05);
mouse(1, A.x, A.y, 0, 1);
for (var i = 1; i <= 12; i++) { mouse(6, A.x + (A.tx - A.x) * i / 12, A.y + (A.ty - A.y) * i / 12, 0); delay(0.016); }
mouse(2, A.tx, A.ty, 0, 1);`
	typeJS = `
var pb = $.NSPasteboard.generalPasteboard;
var saved = [];
var items = pb.pasteboardItems;
for (var i = 0; i < items.count; i++) {
  var it = items.objectAtIndex(i), types = it.types, kept = [];
  for (var j = 0; j < types.count; j++) { var t = types.objectAtIndex(j), d = it.dataForType(t); if (d) kept.push([t, d]); }
  saved.push(kept);
}
pb.clearContents;
pb.setStringForType($(A.text), $.NSPasteboardTypeString);
key(A.v, 1 << 20, true); key(A.v, 1 << 20, false);
delay(0.3);
pb.clearContents;
if (saved.length) {
  var restore = $.NSMutableArray.array;
  saved.forEach(function (kept) {
    var it = $.NSPasteboardItem.alloc.init;
    kept.forEach(function (p) { it.setDataForType(p[1], p[0]); });
    restore.addObject(it);
  });
  pb.writeObjects(restore);
}`
	displaysJS = `
var out = [], screens = $.NSScreen.screens;
for (var i = 0; i < screens.count; i++) {
  var s = screens.objectAtIndex(i), id = ObjC.unwrap(s.deviceDescription.objectForKey($("NSScreenNumber")));
  var b = $.CGDisplayBounds(id);
  out.push({ID: id, Name: ObjC.unwrap(s.localizedName), X: b.origin.x, Y: b.origin.y, W: b.size.width, H: b.size.height, Main: $.CGDisplayIsMain(id) == 1});
}
JSON.stringify(out)`
	// findAppJS looks for an app by name: installed, else running.
	findAppJS = `
var ws = $.NSWorkspace.sharedWorkspace, p = ws.fullPathForApplication($(A.name)), found = null;
if (!p.isNil()) {
  var b = $.NSBundle.bundleWithPath(p), id = b.isNil() ? "" : (ObjC.unwrap(b.bundleIdentifier) || "");
  var running = id ? $.NSRunningApplication.runningApplicationsWithBundleIdentifier($(id)) : null;
  found = {name: ObjC.unwrap($.NSFileManager.defaultManager.displayNameAtPath(p)).replace(/\.app$/, ""), id: id,
    self: !!(running && running.count > 0 && running.objectAtIndex(0).processIdentifier == A.self)};
} else {
  var apps = ws.runningApplications, want = A.name.toLowerCase();
  for (var i = 0; i < apps.count && !found; i++) {
    var r = apps.objectAtIndex(i);
    if ((ObjC.unwrap(r.localizedName) || "").toLowerCase() == want && r.activationPolicy == 0) found = app(r);
  }
}
JSON.stringify(found)`
	// appAtJS finds the window at a point, front to back: on-screen
	// windows, without the desktop's.
	appAtJS = `
var ws = ObjC.deepUnwrap(ObjC.castRefToObject($.CGWindowListCopyWindowInfo(1 | 16, 0))) || [], hit = null;
for (var i = 0; i < ws.length && !hit; i++) {
  var w = ws[i], b = w.kCGWindowBounds;
  if (!b || w.kCGWindowAlpha === 0) continue;
  if (w.kCGWindowOwnerPID == A.self && w.kCGWindowLayer != 0) continue; // the indicator
  if (A.x >= b.X && A.y >= b.Y && A.x < b.X + b.Width && A.y < b.Y + b.Height) hit = w;
}
var front = app($.NSWorkspace.sharedWorkspace.frontmostApplication), out;
if (hit && (hit.kCGWindowLayer == 24 || hit.kCGWindowLayer == 25) && hit.kCGWindowOwnerName == "Window Server") {
  out = front; out.menuBar = true;
} else if (!hit || hit.kCGWindowOwnerName == "Dock") {
  out = app($.NSRunningApplication.runningApplicationsWithBundleIdentifier($("com.apple.finder")).firstObject) || {name: "Finder", id: "com.apple.finder"};
} else {
  out = app($.NSRunningApplication.runningApplicationWithProcessIdentifier(hit.kCGWindowOwnerPID)) || {name: hit.kCGWindowOwnerName, id: ""};
}
JSON.stringify(out)`
	scrollJS = `
mouse(5, A.x, A.y, 0); delay(0.05);
post($.CGEventCreateScrollWheelEvent2(null, 1, 2, -A.dy, -A.dx, 0));`
)
