package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/preview"
)

// Preview opens a localhost or web URL in the built-in browser preview,
// captures a screenshot, and reports console & network errors.
var Preview = &Tool{
	Name:        "preview",
	Description: "Open a localhost or web URL in the built-in browser preview, capture a screenshot, and inspect console/network errors.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["open", "console", "screenshot"],
				"description": "Preview action (defaults to open)"
			},
			"url": {
				"type": "string",
				"description": "Target URL (e.g. http://localhost:3000)"
			},
			"save_screenshot": {
				"type": "string",
				"description": "Optional workspace-relative path to save the PNG screenshot"
			}
		}
	}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct {
			Action         string `json:"action"`
			URL            string `json:"url"`
			SaveScreenshot string `json:"save_screenshot"`
		}
		if err := decode("preview", args, &a); err != nil {
			return perm.Request{}, err
		}
		if strings.TrimSpace(a.SaveScreenshot) != "" {
			if _, err := pathAccess("preview", perm.Write, env, a.SaveScreenshot); err != nil {
				return perm.Request{}, err
			}
		}
		if a.Action == "" || a.Action == "open" {
			target, err := preview.NormalizeURL(a.URL)
			if err != nil {
				return perm.Request{}, err
			}
			if !preview.IsLocalURL(target) {
				return perm.Request{Tool: "preview", Kind: perm.Net}, nil
			}
		}
		return perm.Request{Tool: "preview", Kind: perm.Read}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Action         string `json:"action"`
			URL            string `json:"url"`
			SaveScreenshot string `json:"save_screenshot"`
		}
		if err := decode("preview", args, &a); err != nil {
			return "", err
		}
		act := a.Action
		if act == "" {
			act = "open"
		}
		var snap preview.Snapshot
		var err error
		if act == "open" || strings.TrimSpace(a.URL) != "" {
			snap, err = preview.Default.Open(ctx, a.URL)
			if err != nil && snap.URL == "" {
				return "", err
			}
		} else {
			snap = preview.Default.State()
			if snap.URL == "" {
				return "No active preview. Call preview with action=open and a url.", nil
			}
		}

		if rel := strings.TrimSpace(a.SaveScreenshot); rel != "" && len(snap.Screenshot) > 0 {
			clean := filepath.Clean(rel)
			if !filepath.IsAbs(clean) && !strings.HasPrefix(clean, "..") {
				_ = preview.SaveScreenshot(filepath.Join(env.Root, clean), snap.Screenshot)
			}
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Preview: %s", snap.URL)
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
		return b.String(), nil
	},
}
