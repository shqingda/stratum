package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/preview"
	"github.com/shqingda/stratum/kernel/workspace"
	"github.com/shqingda/stratum/llm"
)

type browserArgs struct {
	preview.Action
	SaveScreenshot string `json:"save_screenshot"`
}

// Browser uses the browser in the app's panel, which the user sees and
// can take over: open, read and act on pages, as a person would.
var Browser = &Tool{
	Name: "browser",
	Description: "Use the browser in Stratum's side panel, which the user sees and shares with you; it has tabs, as Safari does. " +
		"open loads a URL in the active tab (a localhost dev server or a web page); new_tab opens one, with url or blank. " +
		"read returns the page's visible text. open, read, click, hover, type, key, scroll, wait, back, forward and reload return the page's console errors, " +
		"dialogs and failed requests, and its elements, each with a [ref]. click, hover and type act on an element by its ref: type sets the value of a field, " +
		"a select (by option text or value) or an editable element, and submit presses Enter after it. upload attaches files (paths) to a file input ref. " +
		"drag drags an element from ref onto to_ref. key presses keys on the focused element " +
		"(\"Enter\", \"Escape\", \"Tab\", \"shift+Tab\", \"ArrowDown\", \"cmd+a\"). scroll moves by a screen (direction up or down) or to a ref. " +
		"wait waits for text to show (or for seconds). Dialogs the action opens are dismissed unless accept_dialog is set (dialog_text answers a prompt). " +
		"screenshot shows you the page (and saves a PNG to save_screenshot); console lists every console message; network lists the page's requests; " +
		"tabs lists the tabs, select_tab and close_tab take a tab's id; eval runs JavaScript in the page (use return for a value). " +
		"Refs come from the latest result: read again after the page changes.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["open", "new_tab", "read", "click", "hover", "type", "upload", "drag", "key", "scroll", "wait", "back", "forward", "reload", "screenshot", "console", "network", "tabs", "select_tab", "close_tab", "eval"], "description": "What to do (defaults to open)"},
			"url": {"type": "string", "description": "For open and new_tab: the URL, e.g. http://localhost:3000"},
			"ref": {"type": "integer", "description": "For click, hover, type, upload, drag and scroll: the element's ref"},
			"to_ref": {"type": "integer", "description": "For drag: the target element's ref"},
			"paths": {"type": "array", "items": {"type": "string"}, "description": "For upload: workspace-relative file paths to attach"},
			"text": {"type": "string", "description": "For type: the text, or the option of a select. For wait: the text to wait for"},
			"submit": {"type": "boolean", "description": "For type: press Enter after typing"},
			"keys": {"type": "string", "description": "For key, e.g. Enter or shift+Tab"},
			"direction": {"type": "string", "enum": ["up", "down"], "description": "For scroll without a ref"},
			"seconds": {"type": "number", "description": "For wait: at most this long (default 10 with text, 1 without; at most 30)"},
			"tab": {"type": "integer", "description": "For select_tab and close_tab: the tab's id from tabs"},
			"accept_dialog": {"type": "boolean", "description": "Accept the confirm or prompt dialogs this action opens"},
			"dialog_text": {"type": "string", "description": "With accept_dialog: the answer to a prompt dialog"},
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
		if a.Do == "" || a.Do == "open" || a.Do == "new_tab" && strings.TrimSpace(a.URL) != "" {
			target, err := preview.NormalizeURL(a.URL)
			if err != nil {
				return perm.Request{}, err
			}
			if !preview.IsLocalURL(target) {
				return perm.Request{Tool: "browser", Kind: perm.Net}, nil
			}
			return perm.Request{Tool: "browser", Kind: perm.Read}, nil
		}
		var uploadPaths []workspace.Path
		if a.Do == "upload" {
			if len(a.Paths) == 0 {
				return perm.Request{}, errors.New("browser: upload requires paths")
			}
			for _, p := range a.Paths {
				req, err := pathAccess("browser", perm.Read, env, p)
				if err != nil {
					return perm.Request{}, err
				}
				uploadPaths = append(uploadPaths, req.Paths...)
			}
		}
		// Acting on a site of the web is reaching the network, as opening
		// it is: only on the user's own local pages does it run unasked.
		switch a.Do {
		case "click", "hover", "type", "upload", "drag", "key", "eval":
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if !preview.IsLocalPage(preview.Default.CurrentURL(ctx)) {
				return perm.Request{Tool: "browser", Kind: perm.Net, Paths: uploadPaths}, nil
			}
		}
		return perm.Request{Tool: "browser", Kind: perm.Read, Paths: uploadPaths}, nil
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
				return m.Act(ctx, a.Action)
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
		case "upload":
			files, err := loadUploadFiles(env.Root, a.Paths)
			if err != nil {
				return "", err
			}
			a.Action.Files = files
		}
		return m.Act(ctx, a.Action)
	},
}

const (
	maxUploadFileBytes  = 10 << 20 // 10 MiB per file
	maxUploadTotalBytes = 20 << 20 // 20 MiB total
)

func loadUploadFiles(root string, paths []string) ([]preview.UploadFile, error) {
	if len(paths) == 0 {
		return nil, errors.New("browser: upload requires paths")
	}
	var out []preview.UploadFile
	var total int64
	for _, rel := range paths {
		resolved, err := workspace.Resolve(root, rel)
		if err != nil {
			return nil, err
		}
		abs := resolved.Abs
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("browser: %s is a directory", rel)
		}
		if info.Size() > maxUploadFileBytes {
			return nil, fmt.Errorf("browser: %s exceeds 10 MiB upload limit", rel)
		}
		total += info.Size()
		if total > maxUploadTotalBytes {
			return nil, errors.New("browser: total upload size exceeds 20 MiB")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		mimeType := mime.TypeByExtension(filepath.Ext(abs))
		if mimeType == "" {
			mimeType = http.DetectContentType(data)
		}
		out = append(out, preview.UploadFile{
			Name:   filepath.Base(abs),
			MIME:   mimeType,
			Base64: base64.StdEncoding.EncodeToString(data),
		})
	}
	return out, nil
}

// The browser's screenshot goes to the model too, when the model sees
// images: what the page looks like, not only its bytes.
func init() {
	Browser.RunImages = func(ctx context.Context, env Env, args json.RawMessage) (string, []llm.Image, error) {
		out, err := Browser.Run(ctx, env, args)
		if err != nil {
			return "", nil, err
		}
		var a browserArgs
		if decode("browser", args, &a) == nil && a.Do == "screenshot" && preview.Default.Interactive() {
			if png, err := preview.Default.Capture(ctx); err == nil && len(png) > 0 {
				return out, []llm.Image{{MediaType: "image/png", Data: png}}, nil
			}
		}
		return out, nil, nil
	}
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
