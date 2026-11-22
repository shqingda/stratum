package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/perm"
)

const defaultSearchURL = "https://html.duckduckgo.com/html/"

var searchURL = defaultSearchURL

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
		req.Header.Set("User-Agent", "Stratum")
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

// WebSearch searches the web for pages matching a query. Like web_fetch,
// it leaves the machine, so it asks unless the session is in Full mode.
var WebSearch = &Tool{
	Name:        "web_search",
	Description: "Search the web and return matching titles, URLs and snippets. Use it for current information or pages whose URL you do not yet have.",
	Schema: schema(`{"type":"object","properties":{
		"query":{"type":"string","description":"The search query."},
		"allowed_domains":{"type":"array","items":{"type":"string"},"description":"Only include results from these domains."},
		"blocked_domains":{"type":"array","items":{"type":"string"},"description":"Exclude results from these domains."}},"required":["query"]}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct {
			Query string `json:"query"`
		}
		if err := decode("web_search", args, &a); err != nil {
			return perm.Request{}, err
		}
		if strings.TrimSpace(a.Query) == "" {
			return perm.Request{}, fmt.Errorf("web_search: query is required")
		}
		return perm.Request{Tool: "web_search", Kind: perm.Net}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Query          string   `json:"query"`
			AllowedDomains []string `json:"allowed_domains"`
			BlockedDomains []string `json:"blocked_domains"`
		}
		if err := decode("web_search", args, &a); err != nil {
			return "", err
		}
		if strings.TrimSpace(a.Query) == "" {
			return "", fmt.Errorf("web_search: query is required")
		}
		if os.Getenv("STRATUM_DEMO") != "" && searchURL == defaultSearchURL {
			return fmt.Sprintf("1. Demo result for %s\n   https://example.com/demo\n   Demo environment search result.", a.Query), nil
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		u := searchURL + "?q=" + url.QueryEscape(a.Query)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Stratum")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			return "", err
		}
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("web_search: %s", resp.Status)
		}
		results := parseSearchResults(string(b), a.AllowedDomains, a.BlockedDomains)
		if len(results) == 0 {
			return "No results found.", nil
		}
		var out strings.Builder
		for i, r := range results {
			if i > 0 {
				out.WriteString("\n\n")
			}
			fmt.Fprintf(&out, "%d. %s\n   %s", i+1, r.title, r.url)
			if r.snippet != "" {
				fmt.Fprintf(&out, "\n   %s", r.snippet)
			}
		}
		return clip(out.String(), 64<<10), nil
	},
}

type searchResult struct {
	title, url, snippet string
}

var (
	resultLinkRe    = regexp.MustCompile(`(?is)<a\b([^>]*\bclass="[^"]*\bresult__a\b[^"]*"[^>]*)>(.*?)</a>`)
	resultHrefRe    = regexp.MustCompile(`(?is)\bhref="([^"]+)"`)
	resultSnippetRe = regexp.MustCompile(`(?is)<[^>]*\bclass="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</(?:a|div|span|td)>`)
)

func parseSearchResults(body string, allowed, blocked []string) []searchResult {
	locs := resultLinkRe.FindAllStringSubmatchIndex(body, -1)
	var out []searchResult
	for idx, loc := range locs {
		attrs := body[loc[2]:loc[3]]
		rawTitle := body[loc[4]:loc[5]]
		hm := resultHrefRe.FindStringSubmatch(attrs)
		if hm == nil {
			continue
		}
		target := resolveResultURL(html.UnescapeString(hm[1]))
		if target == "" {
			continue
		}
		parsed, err := url.Parse(target)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		if !domainAllowed(parsed.Hostname(), allowed, blocked) {
			continue
		}
		end := len(body)
		if idx+1 < len(locs) {
			end = locs[idx+1][0]
		}
		snippet := ""
		if sm := resultSnippetRe.FindStringSubmatch(body[loc[1]:end]); sm != nil {
			snippet = cleanSearchText(sm[1])
		}
		title := cleanSearchText(rawTitle)
		if title == "" {
			title = target
		}
		out = append(out, searchResult{title: title, url: target, snippet: snippet})
		if len(out) == 10 {
			break
		}
	}
	return out
}

func resolveResultURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	return raw
}

func domainAllowed(host string, allowed, blocked []string) bool {
	host = strings.ToLower(host)
	for _, b := range blocked {
		if matchDomain(host, b) {
			return false
		}
	}
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if matchDomain(host, a) {
			return true
		}
	}
	return false
}

func matchDomain(host, dom string) bool {
	dom = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dom)), ".")
	if dom == "" {
		return false
	}
	return host == dom || strings.HasSuffix(host, "."+dom)
}

func cleanSearchText(s string) string {
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spaceRuns.ReplaceAllString(strings.ReplaceAll(s, "\n", " "), " "))
}
