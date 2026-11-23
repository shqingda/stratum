// Package computer lets the agent see the user's screen and use the
// mouse and the keyboard, as Claude's computer use does: a screenshot
// scaled down for the model, and clicks, typing, keys and scrolling at
// the coordinates of that screenshot.
package computer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
)

// Desktop is the screen and the input of this machine.
type Desktop interface {
	// Size is the main display's size in points.
	Size(ctx context.Context) (w, h float64, err error)
	// Missing names the system permissions the app lacks: "accessibility"
	// for input, "screen-recording" for screenshots.
	Missing(ctx context.Context) []string
	// Screenshot is a JPEG of the main display, w by h pixels.
	Screenshot(ctx context.Context, w, h int) ([]byte, error)
	// Click presses a mouse button count times at x, y in points.
	Click(ctx context.Context, x, y float64, button string, count int) error
	Move(ctx context.Context, x, y float64) error
	Drag(ctx context.Context, x, y, toX, toY float64) error
	// Type types text as the keyboard would.
	Type(ctx context.Context, text string) error
	// Key presses a key with modifiers, as Key parses it.
	Key(ctx context.Context, k Key) error
	// Scroll scrolls at x, y by lines: dy > 0 down, dx > 0 right.
	Scroll(ctx context.Context, x, y float64, dx, dy int) error
	// Open opens an app by its name, or brings it to the front.
	Open(ctx context.Context, app string) error
}

// Guide asks the user for missing permissions, and returns once they gave
// them or turned away: the app's System Settings guide. Nil leaves the
// tool to say what is missing.
type Guide func(ctx context.Context, missing []string) error

var (
	mu      sync.Mutex
	current Desktop = system()
	guide   Guide
)

// Use sets the desktop the tool drives, as tests do with a fake.
func Use(d Desktop) (undo func()) {
	mu.Lock()
	defer mu.Unlock()
	old := current
	current = d
	return func() { mu.Lock(); current = old; mu.Unlock() }
}

// SetGuide sets how missing permissions are asked for.
func SetGuide(g Guide) {
	mu.Lock()
	guide = g
	mu.Unlock()
}

// ErrUnsupported is returned on systems computer use does not run on.
var ErrUnsupported = errors.New("computer use runs on macOS only")

func desktop() (Desktop, Guide) {
	mu.Lock()
	defer mu.Unlock()
	return current, guide
}

// Ready returns the desktop once the app may use it, after asking the
// user for permissions it lacks.
func Ready(ctx context.Context) (Desktop, error) {
	d, g := desktop()
	if d == nil {
		return nil, ErrUnsupported
	}
	missing := d.Missing(ctx)
	if len(missing) > 0 && g != nil {
		if err := g(ctx, missing); err != nil {
			return nil, err
		}
		missing = d.Missing(ctx)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Stratum lacks the %s permission: the user must allow it in System Settings › Privacy & Security, then try again", strings.Join(missing, " and "))
	}
	return d, nil
}

// MaxWidth and MaxHeight bound a screenshot: what the model sees well and
// what costs it little.
const MaxWidth, MaxHeight = 1280, 800

// Scale is how screenshot pixels map to points: the screenshot is the
// display scaled by Scale to fit MaxWidth by MaxHeight.
func Scale(w, h float64) (s float64, iw, ih int) {
	s = math.Min(1, math.Min(MaxWidth/w, MaxHeight/h))
	return s, int(math.Round(w * s)), int(math.Round(h * s))
}

// Key is a key and its modifiers.
type Key struct {
	Name                        string // a key: "return", "a", "f5", "left"…
	Cmd, Shift, Option, Control bool
	Fn                          bool
}

// ParseKey reads keys as "cmd+shift+t", "return", "ctrl+c" or "esc".
func ParseKey(s string) (Key, error) {
	var k Key
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			switch p {
			case "cmd", "command", "super", "meta":
				k.Cmd = true
			case "shift":
				k.Shift = true
			case "alt", "option", "opt":
				k.Option = true
			case "ctrl", "control":
				k.Control = true
			case "fn":
				k.Fn = true
			default:
				return k, fmt.Errorf("computer: unknown modifier %q in %q", p, s)
			}
			continue
		}
		if alias, ok := keyAliases[p]; ok {
			p = alias
		}
		if _, ok := KeyCodes[p]; !ok {
			return k, fmt.Errorf("computer: unknown key %q in %q", p, s)
		}
		k.Name = p
	}
	return k, nil
}

var keyAliases = map[string]string{
	"enter": "return", "esc": "escape", "backspace": "delete", "del": "forwarddelete",
	"arrowleft": "left", "arrowright": "right", "arrowup": "up", "arrowdown": "down",
	"pgup": "pageup", "pgdn": "pagedown", "spacebar": "space", " ": "space",
}

// KeyCodes are macOS's virtual key codes (Events.h), for a US layout.
var KeyCodes = map[string]int{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9, "b": 11, "q": 12, "w": 13, "e": 14, "r": 15,
	"y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28, "0": 29,
	"]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42, ",": 43,
	"/": 44, "n": 45, "m": 46, ".": 47, "`": 50,
	"return": 36, "tab": 48, "space": 49, "delete": 51, "escape": 53, "forwarddelete": 117,
	"home": 115, "end": 119, "pageup": 116, "pagedown": 121, "left": 123, "right": 124, "down": 125, "up": 126,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
}
