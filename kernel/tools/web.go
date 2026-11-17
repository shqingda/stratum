package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shqingda/talos/kernel/perm"
)

// WebFetch reads a web page as text. It leaves the machine, so it asks
// unless the session is in Full mode.
var WebFetch = &Tool{
	Name:        "web_fetch",
	Description: "Fetch an http(s) URL and return its text: HTML is reduced to readable text, other text as is. Use it for documentation, issues and pages the user names.",
	Schema: schema(`{"type":"object","properties":{
		"url":{"type":"string","description":"An http or https URL."}},"required":["url"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct{ URL string }
		if err := decode("web_fetch", args, &a); err != nil {
			return perm.Request{}, err
		}
		if u, err := url.Parse(a.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return perm.Request{}, fmt.Errorf("web_fetch: %q is not an http(s) URL", a.URL)
		}
		return perm.Request{Tool: "web_fetch", Kind: perm.Net}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct{ URL string }
		if err := decode("web_fetch", args, &a); err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Talos")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			return "", err
		}
		ct := resp.Header.Get("Content-Type")
		text := string(b)
		switch {
		case strings.Contains(ct, "html"):
			text = htmlText(text)
		case !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "xml"):
			return fmt.Sprintf("[%s: %s, %d bytes, not text]", resp.Status, ct, len(b)), nil
		}
		out := fmt.Sprintf("[%s %s]\n%s", resp.Status, a.URL, text)
		return clip(out, 100<<10), nil
	},
}

var (
	dropTags  = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)\b.*?</(script|style|noscript|svg|head)>`)
	blockTags = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|pre|section|article|table|ul|ol)\b[^>]*>`)
	anyTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRuns = regexp.MustCompile(`\n[ \t]*(\n[ \t]*)+`)
	spaceRuns = regexp.MustCompile(`[ \t]+`)
)

// htmlText keeps a page's words and its paragraph breaks.
func htmlText(s string) string {
	s = dropTags.ReplaceAllString(s, "")
	s = blockTags.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spaceRuns.ReplaceAllString(s, " ")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
