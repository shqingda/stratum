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
