package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/preview"
)

type browserArgs struct {
	preview.Action
	SaveScreenshot string `json:"save_screenshot"`
}

// Browser uses the browser in the app's panel, which the user sees and
// can take over: open, read and act on pages, as a person would.
var Browser = &Tool{
	Name: "browser",
	Description: "Use the browser in Stratum's side panel, which the user sees and shares with you. " +
		"open loads a URL (a localhost dev server or a web page); read returns the page's visible text; " +
		"open, read, click, type, scroll, back, forward and reload return the page's console errors and its elements, each with a [ref]. " +
		"click and type act on an element by its ref (type sets the value of a field, select or editable element; submit presses Enter). " +
		"scroll moves by a screen (direction up or down) or to a ref. screenshot saves a PNG to save_screenshot. eval runs JavaScript in the page (use return for a value). " +
		"Refs come from the latest result: read again after the page changes.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["open", "read", "click", "type", "scroll", "back", "forward", "reload", "screenshot", "console", "eval"], "description": "What to do (defaults to open)"},
			"url": {"type": "string", "description": "For open: the URL, e.g. http://localhost:3000"},
			"ref": {"type": "integer", "description": "For click, type and scroll: the element's ref"},
			"text": {"type": "string", "description": "For type: the text, or the option of a select"},
			"submit": {"type": "boolean", "description": "For type: press Enter after typing"},
			"direction": {"type": "string", "enum": ["up", "down"], "description": "For scroll without a ref"},
			"script": {"type": "string", "description": "For eval: JavaScript run as the body of an async function"},
			"save_screenshot": {"type": "string", "description": "For screenshot: workspace-relative path of the PNG"}
		}
	}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a browserArgs
		if err := decode("browser", args, &a); err != nil {
			return perm.Request{}, err
		}
		if strings.TrimSpace(a.SaveScreenshot) != "" {
			if _, err := pathAccess("browser", perm.Write, env, a.SaveScreenshot); err != nil {
				return perm.Request{}, err
			}
		}
		if a.Do == "" || a.Do == "open" {
			target, err := preview.NormalizeURL(a.URL)
			if err != nil {
				return perm.Request{}, err
			}
			if !preview.IsLocalURL(target) {
				return perm.Request{Tool: "browser", Kind: perm.Net}, nil
			}
			return perm.Request{Tool: "browser", Kind: perm.Read}, nil
		}
		// Acting on a site of the web is reaching the network, as opening
		// it is: only on the user's own local pages does it run unasked.
		if a.Do != "screenshot" && a.Do != "console" && a.Do != "read" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if !preview.IsLocalPage(preview.Default.CurrentURL(ctx)) {
				return perm.Request{Tool: "browser", Kind: perm.Net}, nil
			}
		}
		return perm.Request{Tool: "browser", Kind: perm.Read}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a browserArgs
		if err := decode("browser", args, &a); err != nil {
			return "", err
		}
		m := preview.Default
		switch a.Do {
		case "", "open":
			snap, err := m.Open(ctx, a.URL)
			if err != nil && snap.URL == "" {
				return "", err
			}
			if m.Interactive() {
				s, rerr := m.Summary(ctx)
				if err != nil {
					s = "Error: " + err.Error() + "\n" + s
				}
				if rerr != nil {
					return snapshotText(snap), nil
				}
				return s, nil
			}
			return snapshotText(snap), nil
		case "screenshot", "console":
			var snap preview.Snapshot
			if m.Interactive() && a.Do == "console" {
				return m.Summary(ctx)
			}
			if m.Interactive() {
				png, err := m.Capture(ctx)
				if err != nil {
					return "", err
				}
				snap = preview.Snapshot{URL: m.CurrentURL(ctx), Screenshot: png}
			} else if snap = m.State(); snap.URL == "" {
				return "No page is open. Call browser with action=open and a url.", nil
			}
			out := snapshotText(snap)
			if rel := strings.TrimSpace(a.SaveScreenshot); rel != "" && len(snap.Screenshot) > 0 {
				clean := filepath.Clean(rel)
				if !filepath.IsAbs(clean) && !strings.HasPrefix(clean, "..") {
					if err := preview.SaveScreenshot(filepath.Join(env.Root, clean), snap.Screenshot); err != nil {
						return "", err
					}
					out += "\nSaved: " + clean
				}
			}
			return out, nil
		}
		return m.Act(ctx, a.Action)
	},
}

func snapshotText(snap preview.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Page: %s", snap.URL)
	if snap.Title != "" {
		fmt.Fprintf(&b, " (%s)", snap.Title)
	}
	if snap.Status > 0 {
		fmt.Fprintf(&b, " [HTTP %d]", snap.Status)
	}
	if len(snap.Screenshot) > 0 {
		fmt.Fprintf(&b, "\nScreenshot: %d bytes PNG captured", len(snap.Screenshot))
	}
	if snap.Error != "" {
		fmt.Fprintf(&b, "\nError: %s", snap.Error)
	}
	if len(snap.Console) > 0 {
		fmt.Fprintf(&b, "\nConsole (%d):", len(snap.Console))
		for _, c := range snap.Console {
			fmt.Fprintf(&b, "\n- [%s] %s", c.Level, c.Text)
			if c.Source != "" {
				fmt.Fprintf(&b, " (%s:%d)", c.Source, c.Line)
			}
		}
	} else {
		b.WriteString("\nConsole: 0 errors")
	}
	if len(snap.Network) > 0 {
		fmt.Fprintf(&b, "\nNetwork (%d):", len(snap.Network))
		for _, n := range snap.Network {
			fmt.Fprintf(&b, "\n- [%d] %s %s", n.Status, n.URL, n.Error)
		}
	}
	return b.String()
}
