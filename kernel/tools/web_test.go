package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shqingda/talos/kernel/perm"
)

func TestWebFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>x</title><style>p{}</style></head><body><h1>Docs</h1><p>Use &amp; enjoy.</p><script>alert(1)</script></body></html>`))
	}))
	defer srv.Close()
	env := Env{Root: t.TempDir()}
	out, err := call(t, env, WebFetch, map[string]any{"url": srv.URL})
	if err != nil || !strings.Contains(out, "Docs\n\nUse & enjoy.") || strings.Contains(out, "alert") {
		t.Fatalf("%q %v", out, err)
	}
	req, _ := WebFetch.Access(env, []byte(`{"url":"https://example.com"}`))
	if req.Kind != perm.Net || perm.Decide(perm.Auto, req, true) != perm.AskUser || perm.Decide(perm.Full, req, true) != perm.Allow {
		t.Fatalf("%+v", req)
	}
	if _, err := WebFetch.Access(env, []byte(`{"url":"file:///etc/passwd"}`)); err == nil {
		t.Fatal("took a file URL")
	}
}

func TestWebSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "golang generics" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Ftutorial%2Fgenerics">Tutorial: <b>Generics</b></a>
  <a class="result__snippet">Learn how to use generics &amp; type parameters.</a>
</div>
<div class="result">
  <a href="https://spam.example.com/ad" class="result__a">Spam Result</a>
  <div class="result__snippet">Blocked snippet</div>
</div>
<div class="result">
  <a class="result__a" href="https://pkg.go.dev/golang.org/x/exp">x/exp package</a>
  <span class="result__snippet">Experimental Go packages.</span>
</div>
</body></html>`))
	}))
	defer srv.Close()
	old := searchURL
	searchURL = srv.URL
	defer func() { searchURL = old }()

	env := Env{Root: t.TempDir()}
	out, err := call(t, env, WebSearch, map[string]any{
		"query":           "golang generics",
		"allowed_domains": []string{"go.dev"},
		"blocked_domains": []string{"pkg.go.dev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1. Tutorial: Generics\n   https://go.dev/doc/tutorial/generics\n   Learn how to use generics & type parameters.") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if strings.Contains(out, "spam.example.com") || strings.Contains(out, "pkg.go.dev") {
		t.Fatalf("domain filters failed:\n%s", out)
	}

	req, err := WebSearch.Access(env, []byte(`{"query":"go"}`))
	if err != nil || req.Kind != perm.Net || perm.Decide(perm.Auto, req, true) != perm.AskUser || perm.Decide(perm.Full, req, true) != perm.Allow {
		t.Fatalf("%+v %v", req, err)
	}
	if _, err := WebSearch.Access(env, []byte(`{"query":"   "}`)); err == nil {
		t.Fatal("accepted blank query")
	}
}
