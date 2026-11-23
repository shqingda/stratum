package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/llm"
)

type computerArgs struct {
	Action    string  `json:"action"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	ToX       float64 `json:"to_x"`
	ToY       float64 `json:"to_y"`
	Text      string  `json:"text"`
	Keys      string  `json:"keys"`
	Direction string  `json:"direction"`
	Amount    int     `json:"amount"`
	App       string  `json:"app"`
	Seconds   float64 `json:"seconds"`
}

// Computer sees the user's screen and uses the mouse and the keyboard.
// Every call asks the user unless they gave Stratum full access.
var Computer = &Tool{
	Name: "computer",
	Description: "See and use the user's Mac as a person at it would, for apps no other tool reaches " +
		"(use browser for web pages and shell for commands). screenshot shows the main display. " +
		"click, double_click, right_click and move act at x, y; drag goes from x, y to to_x, to_y; " +
		"coordinates are pixels of the latest screenshot. type types text where the focus is; " +
		"key presses keys such as \"return\", \"cmd+c\" or \"shift+tab\"; scroll scrolls at x, y " +
		"(direction up, down, left or right; amount in lines, 5 by default); open_app opens or brings forward an app by name; " +
		"wait waits seconds. Every action returns a new screenshot.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["screenshot", "click", "double_click", "right_click", "move", "drag", "type", "key", "scroll", "open_app", "wait"]},
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
		return perm.Request{Tool: "computer", Kind: perm.Computer}, nil
	},
	RunImages: runComputer,
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		out, _, err := runComputer(ctx, env, args)
		return out, err
	},
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
	w, h, err := d.Size(ctx)
	if err != nil {
		return "", nil, err
	}
	s, iw, ih := computer.Scale(w, h)
	// The model's coordinates are the screenshot's pixels.
	pt := func(x, y float64) (float64, float64, error) {
		if x < 0 || y < 0 || x > float64(iw) || y > float64(ih) {
			return 0, 0, fmt.Errorf("computer: %.0f, %.0f is outside the %d×%d screenshot", x, y, iw, ih)
		}
		return x / s, y / s, nil
	}
	did := a.Action
	switch a.Action {
	case "screenshot":
	case "click", "double_click", "right_click":
		x, y, err := pt(a.X, a.Y)
		if err != nil {
			return "", nil, err
		}
		button, count := "left", 1
		if a.Action == "double_click" {
			count = 2
		} else if a.Action == "right_click" {
			button = "right"
		}
		err = d.Click(ctx, x, y, button, count)
		if err != nil {
			return "", nil, err
		}
		did = fmt.Sprintf("%s at %.0f, %.0f", a.Action, a.X, a.Y)
	case "move":
		x, y, err := pt(a.X, a.Y)
		if err == nil {
			err = d.Move(ctx, x, y)
		}
		if err != nil {
			return "", nil, err
		}
	case "drag":
		x, y, err := pt(a.X, a.Y)
		if err != nil {
			return "", nil, err
		}
		tx, ty, err := pt(a.ToX, a.ToY)
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
		if err := d.Type(ctx, a.Text); err != nil {
			return "", nil, err
		}
	case "key":
		k, err := computer.ParseKey(a.Keys)
		if err == nil {
			err = d.Key(ctx, k)
		}
		if err != nil {
			return "", nil, err
		}
		did = "pressed " + a.Keys
	case "scroll":
		x, y, err := pt(a.X, a.Y)
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
		if err := d.Open(ctx, a.App); err != nil {
			return "", nil, err
		}
		did = "opened " + a.App
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
	if a.Action != "screenshot" && a.Action != "wait" {
		// Let the screen show what the action did.
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(computerSettle):
		}
	}
	shot, err := d.Screenshot(ctx, iw, ih)
	if err != nil {
		return "", nil, err
	}
	out := fmt.Sprintf("%s. Screenshot of the main display: %d×%d pixels.", strings.ToUpper(did[:1])+did[1:], iw, ih)
	return out, []llm.Image{{MediaType: "image/jpeg", Data: shot}}, nil
}

var computerSettle = 600 * time.Millisecond
