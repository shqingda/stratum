package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const modelsDev = `{
 "anthropic": {"models": {
  "claude-new": {"id":"claude-new","name":"Claude New","tool_call":true,"reasoning":true,"release_date":"2026-10-01",
   "modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":1000,"output":100},
   "cost":{"input":2,"output":10,"cache_read":0.1,"cache_write":2.5}},
  "claude-old": {"id":"claude-old","name":"Claude Old","tool_call":true,"release_date":"2025-01-01","modalities":{"input":["text"],"output":["text"]}},
  "no-tools": {"id":"no-tools","tool_call":false,"modalities":{"output":["text"]}},
  "embed": {"id":"embed","type":"embedding","tool_call":true,"modalities":{"output":["text"]}}}},
 "moonshotai": {"models": {"kimi": {"id":"kimi","tool_call":true,"modalities":{"output":["text"]}}}},
 "elsewhere": {"models": {"x": {"id":"x","tool_call":true,"modalities":{"output":["text"]}}}}
}`

func TestLoadCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	os.WriteFile(path, []byte(modelsDev), 0o600)
	t.Cleanup(func() { catalog = builtin })
	if err := LoadCatalog(context.Background(), path, time.Hour, map[string]string{"anthropic": "anthropic", "moonshotai": "moonshot"}); err != nil {
		t.Fatal(err)
	}
	a := Models("anthropic")
	if len(a) != 2 || a[0].ID != "claude-new" || !a[0].Images || a[0].Price.CacheWrite != 2.5 {
		t.Fatalf("%+v", a)
	}
	if len(Models("moonshot")) != 1 || len(Models("elsewhere")) != 0 {
		t.Error(Models(""))
	}
	if _, ok := FindModel("codex", "gpt-6-luna"); !ok {
		t.Error("lost the ChatGPT backend's models")
	}
	if m, _ := FindModel("anthropic", "claude-new"); m.Cost(Usage{Input: 1_000_000, CacheRead: 500_000, Output: 100_000}) != 1+0.05+1 {
		t.Errorf("cost %v", m.Cost(Usage{Input: 1_000_000, CacheRead: 500_000, Output: 100_000}))
	}
}
