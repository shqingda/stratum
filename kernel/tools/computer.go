package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/llm"
)

type computerArgs struct {
	Action    string   `json:"action"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	ToX       float64  `json:"to_x"`
	ToY       float64  `json:"to_y"`
	Text      string   `json:"text"`
	Keys      string   `json:"keys"`
	Direction string   `json:"direction"`
	Amount    int      `json:"amount"`
	App       string   `json:"app"`
	Apps      []string `json:"apps"`
	Reason    string   `json:"reason"`
	Display   string   `json:"display"`
	Seconds   float64  `json:"seconds"`
}

// Computer sees the user's screen and uses the mouse and the keyboard, as
// Claude's computer use does: the user grants each app first, browsers
// only show, terminals and editors take only clicks.
var Computer = &Tool{
	Name: "computer",
	Description: "See and use the user's Mac as a person at it would, for apps no other tool reaches " +
		"(use browser for web pages and shell for commands). First call request_access with the apps you need " +
		"(names as Finder shows them, such as \"Notes\" or \"Google Chrome\") and why: the user grants each app, at a tier. " +
		"Browsers get tier read: they show in screenshots, but clicks and typing are blocked. Terminals and code editors " +
		"get tier click: left clicks and scrolling only, no typing, keys, right clicks or drags. Other apps get tier full. " +
		"Clicking the desktop or the Dock needs Finder; the menu bar needs the app in front at tier full. Stratum itself is never granted. " +
		"screenshot shows a display, the main one at first; switch_display shows another (display: its number or name). " +
		"click, double_click, right_click and move act at x, y; drag goes from x, y to to_x, to_y; " +
		"coordinates are pixels of the latest screenshot. type types text where the focus is; " +
		"key presses keys such as \"return\", \"cmd+c\" or \"shift+tab\"; scroll scrolls at x, y " +
		"(direction up, down, left or right; amount in lines, 5 by default); open_app opens or brings forward a granted app; " +
		"wait waits seconds. Every action returns a new screenshot. The user can stop you with esc.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["request_access", "screenshot", "switch_display", "click", "double_click", "right_click", "move", "drag", "type", "key", "scroll", "open_app", "wait"]},
			"apps": {"type": "array", "items": {"type": "string"}, "description": "For request_access: app names"},
			"reason": {"type": "string", "description": "For request_access: what you will do, for the user"},
			"display": {"type": "string", "description": "For switch_display: a number from 1, or a name"},
			"x": {"type": "number"}, "y": {"type": "number"},
			"to_x": {"type": "number", "description": "For drag"}, "to_y": {"type": "number", "description": "For drag"},
			"text": {"type": "string", "description": "For type"},
			"keys": {"type": "string", "description": "For key, e.g. cmd+shift+t"},
			"direction": {"type": "string", "enum": ["up", "down", "left", "right"], "description": "For scroll"},
			"amount": {"type": "integer", "description": "For scroll: lines"},
			"app": {"type": "string", "description": "For open_app, e.g. Notes"},
			"seconds": {"type": "number", "description": "For wait, at most 10"}
		},
		"required": ["action"]
	}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a computerArgs
		if err := decode("computer", args, &a); err != nil {
			return perm.Request{}, err
		}
		if a.Action != "request_access" {
			// The apps granted bound it: run checks them.
			return perm.Request{Tool: "computer", Kind: perm.Computer, Granted: true}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		apps, err := findApps(ctx, a.Apps)
		if err != nil {
			return perm.Request{}, err
		}
		req := perm.Request{Tool: "computer", Kind: perm.Computer}
		for _, app := range apps {
			req.Apps = append(req.Apps, perm.App{Name: app.Name, Tier: string(computer.TierOf(app))})
		}
		return req, nil
	},
	RunImages: runComputer,
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		out, _, err := runComputer(ctx, env, args)
		return out, err
	},
}

// findApps finds the apps request_access names.
func findApps(ctx context.Context, names []string) ([]computer.App, error) {
	var out []computer.App
	seen := map[string]bool{}
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		d, err := computer.Current()
		if err != nil {
			return nil, err
		}
		app, err := d.FindApp(ctx, strings.TrimSpace(n))
		if err != nil {
			return nil, err
		}
		if app.Self {
			return nil, errors.New("computer: Stratum cannot be granted: the agent never uses Stratum itself")
		}
		if !seen[app.ID+"\x00"+app.Name] {
			seen[app.ID+"\x00"+app.Name] = true
			out = append(out, app)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("computer: request_access needs apps")
	}
	return out, nil
}

// tierWords say what a tier lets the agent do, for the model.
var tierWords = map[computer.Tier]string{
	computer.TierRead:  "it shows in screenshots, but clicks, typing and keys are blocked; use the browser tool for web pages",
	computer.TierClick: "left clicks and scrolling work, but typing, keys, right clicks and drags are blocked; use the shell tool for commands",
	computer.TierFull:  "everything works",
}

// allow checks that the session may do what needs tier need in app.
func allow(session string, app computer.App, need computer.Tier) error {
	if app.Self {
		return errors.New("computer: that is Stratum's own window, which the agent never uses: bring forward the app to use with open_app")
	}
	g, ok := computer.GrantFor(session, app)
	if !ok {
		return fmt.Errorf("computer: %s is not granted: call request_access with it first", app.Name)
	}
	if !g.Tier.Allows(need) {
		return fmt.Errorf("computer: %s is granted at tier %s: %s", app.Name, g.Tier, tierWords[g.Tier])
	}
	return nil
}

func runComputer(ctx context.Context, env Env, args json.RawMessage) (string, []llm.Image, error) {
	var a computerArgs
	if err := decode("computer", args, &a); err != nil {
		return "", nil, err
	}
	d, err := computer.Ready(ctx)
	if err != nil {
		return "", nil, err
	}
	if a.Action == "request_access" {
		apps, err := findApps(ctx, a.Apps)
		if err != nil {
			return "", nil, err
		}
		granted := computer.GrantApps(env.SessionID, apps)
		var b strings.Builder
		b.WriteString("The user granted, until Stratum quits:")
		for _, g := range granted {
			fmt.Fprintf(&b, "\n- %s: tier %s (%s)", g.Name, g.Tier, tierWords[g.Tier])
		}
		b.WriteString("\nTake a screenshot to see the screen.")
		return b.String(), nil, nil
	}
	if len(computer.Granted(env.SessionID)) == 0 {
		return "", nil, errors.New("computer: no app is granted yet: call request_access with the apps you need first")
	}
	computer.Using(ctx, env.SessionID, env.Stop)
	displays, err := d.Displays(ctx)
	if err != nil {
		return "", nil, err
	}
	shown := displays[0]
	for _, x := range displays {
		if x.ID == computer.ShownDisplay(env.SessionID) {
			shown = x
		}
	}
	if a.Action == "switch_display" {
		x, err := pickDisplay(displays, a.Display)
		if err != nil {
			return "", nil, err
		}
		computer.ShowDisplay(env.SessionID, x.ID)
		shown = x
	}
	s, iw, ih := computer.Scale(shown.W, shown.H)
	// The model's coordinates are the screenshot's pixels.
	pt := func(x, y float64) (float64, float64, error) {
		if x < 0 || y < 0 || x > float64(iw) || y > float64(ih) {
			return 0, 0, fmt.Errorf("computer: %.0f, %.0f is outside the %d×%d screenshot", x, y, iw, ih)
		}
		return shown.X + min(x/s, shown.W-1), shown.Y + min(y/s, shown.H-1), nil
	}
	// at is the point x, y once the app there may take what needs need.
	at := func(x, y float64, need computer.Tier) (float64, float64, error) {
		px, py, err := pt(x, y)
		if err != nil {
			return 0, 0, err
		}
		t, err := d.AppAt(ctx, px, py)
		if err != nil {
			return 0, 0, err
		}
		if t.MenuBar {
			need = computer.TierFull
		}
		return px, py, allow(env.SessionID, t.App, need)
	}
	// front checks the app in front, which takes the keyboard.
	front := func() error {
		app, err := d.FrontApp(ctx)
		if err != nil {
			return err
		}
		return allow(env.SessionID, app, computer.TierFull)
	}
	did := a.Action
	switch a.Action {
	case "screenshot", "switch_display":
	case "click", "double_click", "right_click":
		button, count, need := "left", 1, computer.TierClick
		if a.Action == "double_click" {
			count = 2
		} else if a.Action == "right_click" {
			button, need = "right", computer.TierFull
		}
		x, y, err := at(a.X, a.Y, need)
		if err != nil {
			return "", nil, err
		}
		if err := d.Click(ctx, x, y, button, count); err != nil {
			return "", nil, err
		}
		did = fmt.Sprintf("%s at %.0f, %.0f", a.Action, a.X, a.Y)
	case "move":
		x, y, err := at(a.X, a.Y, computer.TierRead)
		if err == nil {
			err = d.Move(ctx, x, y)
		}
		if err != nil {
			return "", nil, err
		}
	case "drag":
		x, y, err := at(a.X, a.Y, computer.TierFull)
		if err != nil {
			return "", nil, err
		}
		tx, ty, err := at(a.ToX, a.ToY, computer.TierFull)
		if err == nil {
			err = d.Drag(ctx, x, y, tx, ty)
		}
		if err != nil {
			return "", nil, err
		}
	case "type":
		if a.Text == "" {
			return "", nil, errors.New("computer: type needs text")
		}
		if err := front(); err != nil {
			return "", nil, err
		}
		if err := d.Type(ctx, a.Text); err != nil {
			return "", nil, err
		}
	case "key":
		k, err := computer.ParseKey(a.Keys)
		if err == nil {
			err = front()
		}
		if err != nil {
			return "", nil, err
		}
		release := func() {}
		if k.Name == "escape" {
			release = computer.HoldEsc() // the agent's esc does not stop it
		}
		err = d.Key(ctx, k)
		release()
		if err != nil {
			return "", nil, err
		}
		did = "pressed " + a.Keys
	case "scroll":
		x, y, err := at(a.X, a.Y, computer.TierClick)
		if err != nil {
			return "", nil, err
		}
		n := a.Amount
		if n <= 0 {
			n = 5
		}
		dx, dy := 0, 0
		switch a.Direction {
		case "up":
			dy = -n
		case "down", "":
			dy = n
		case "left":
			dx = -n
		case "right":
			dx = n
		default:
			return "", nil, fmt.Errorf("computer: unknown direction %q", a.Direction)
		}
		if err := d.Scroll(ctx, x, y, dx, dy); err != nil {
			return "", nil, err
		}
	case "open_app":
		if strings.TrimSpace(a.App) == "" {
			return "", nil, errors.New("computer: open_app needs an app")
		}
		app, err := d.FindApp(ctx, strings.TrimSpace(a.App))
		if err == nil {
			err = allow(env.SessionID, app, computer.TierRead)
		}
		if err == nil {
			err = d.Open(ctx, app)
		}
		if err != nil {
			return "", nil, err
		}
		did = "opened " + app.Name
	case "wait":
		sec := min(max(a.Seconds, 0.5), 10)
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(time.Duration(sec * float64(time.Second))):
		}
	default:
		return "", nil, fmt.Errorf("computer: unknown action %q", a.Action)
	}
	if a.Action != "screenshot" && a.Action != "wait" && a.Action != "switch_display" {
		// Let the screen show what the action did.
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(computerSettle):
		}
	}
	shot, err := d.Screenshot(ctx, shown, iw, ih)
	if err != nil {
		return "", nil, err
	}
	if wl, ok := d.(computer.WindowLister); ok {
		if wins, werr := wl.Windows(ctx); werr == nil && len(wins) > 0 {
			shot, err = computer.RedactScreenshot(shot, shown, s, wins, func(app computer.App) bool {
				_, ok := computer.GrantFor(env.SessionID, app)
				return ok
			})
			if err != nil {
				return "", nil, err
			}
		}
	}
	did = strings.ReplaceAll(did, "_", " ")
	out := fmt.Sprintf("%s. Screenshot of %s: %d×%d pixels.", strings.ToUpper(did[:1])+did[1:], shown.Name, iw, ih)
	if len(displays) > 1 {
		var names []string
		for i, x := range displays {
			n := fmt.Sprintf("%d %s", i+1, x.Name)
			if x.ID == shown.ID {
				n += " (shown)"
			}
			names = append(names, n)
		}
		out += " Displays: " + strings.Join(names, ", ") + "; switch_display shows another."
	}
	return out, []llm.Image{{MediaType: "image/jpeg", Data: shot}}, nil
}

// pickDisplay finds a display by its number from 1 or by its name.
func pickDisplay(displays []computer.Display, which string) (computer.Display, error) {
	which = strings.TrimSpace(which)
	if n, err := strconv.Atoi(which); err == nil && n >= 1 && n <= len(displays) {
		return displays[n-1], nil
	}
	for _, x := range displays {
		if which != "" && strings.Contains(strings.ToLower(x.Name), strings.ToLower(which)) {
			return x, nil
		}
	}
	var names []string
	for i, x := range displays {
		names = append(names, fmt.Sprintf("%d %s", i+1, x.Name))
	}
	return computer.Display{}, fmt.Errorf("computer: no display %q: there are %s", which, strings.Join(names, ", "))
}

var computerSettle = 600 * time.Millisecond
