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

func TestEmbeddedCatalogAndDefaults(t *testing.T) {
	if got := len(Models("")); got <= 1200 {
		t.Fatalf("expected >1200 embedded models, got %d", got)
	}
	expectedProviders := []string{
		"codex", "copilot", "qwen",
		"ant-ling", "anthropic", "baseten", "cerebras", "deepseek", "fireworks",
		"google", "groq", "huggingface", "kimi-coding", "kimi-coding-global",
		"minimax", "minimax-cn", "moonshotai", "moonshotai-cn", "nvidia",
		"openai", "opencode", "opencode-go", "openrouter",
		"qwen-token-plan", "qwen-token-plan-cn", "qwen-token-plan-individual", "qwen-token-plan-individual-cn",
		"together", "vercel-ai-gateway", "xai",
		"xiaomi", "xiaomi-token-plan-ams", "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp",
		"zai", "zai-api", "zai-coding-cn", "zhipuai",
	}
	for _, p := range expectedProviders {
		if len(Models(p)) == 0 {
			t.Errorf("provider %q has no models in embedded catalog", p)
		}
	}
	for p, id := range defaults {
		if _, ok := FindModel(p, id); !ok {
			t.Errorf("default model %q/%q missing from catalog", p, id)
		}
	}
	if len(Models("moonshot")) == 0 {
		t.Error("moonshot alias returned 0 models")
	}
	if _, ok := FindModel("moonshot", "kimi-k3.1-pro"); !ok {
		t.Error("moonshot alias failed to find kimi-k3.1-pro")
	}
	if DefaultModel("moonshot") != DefaultModel("moonshotai") {
		t.Errorf("DefaultModel(moonshot) = %q, want %q", DefaultModel("moonshot"), DefaultModel("moonshotai"))
	}
}

func TestResolveEffort(t *testing.T) {
	str := func(s string) *string { return &s }
	if got := ResolveEffort("", map[string]*string{"medium": nil}); got != "" {
		t.Errorf("empty effort got %q", got)
	}
	glmMap := map[string]*string{
		"off": nil, "minimal": nil, "low": str("low"), "medium": nil, "high": str("high"), "xhigh": nil, "max": str("max"),
	}
	if got := ResolveEffort("medium", glmMap); got != "high" {
		t.Errorf("medium clamped to %q, want high", got)
	}
	if got := ResolveEffort("xhigh", glmMap); got != "max" {
		t.Errorf("xhigh clamped to %q, want max", got)
	}
	gemmaMap := map[string]*string{
		"off": nil, "minimal": str("MINIMAL"), "low": nil, "medium": nil, "high": str("HIGH"),
	}
	if got := ResolveEffort("xhigh", gemmaMap); got != "HIGH" {
		t.Errorf("xhigh on gemma got %q, want HIGH", got)
	}
	if got := ResolveEffort("minimal", gemmaMap); got != "MINIMAL" {
		t.Errorf("minimal on gemma got %q, want MINIMAL", got)
	}
}

func TestLoadCatalogPreservesMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	data := `{
		"anthropic": {"models": {
			"claude-opus-5-5": {"id":"claude-opus-5-5","name":"Claude Opus 5.5","tool_call":true,"reasoning":true,"release_date":"2026-10-01","modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":1000000,"output":128000}}
		}}
	}`
	os.WriteFile(path, []byte(data), 0o600)
	t.Cleanup(func() { catalog = builtin })
	if err := LoadCatalog(context.Background(), path, time.Hour, map[string]string{"anthropic": "anthropic"}); err != nil {
		t.Fatal(err)
	}
	m, ok := FindModel("anthropic", "claude-opus-5-5")
	if !ok {
		t.Fatal("claude-opus-5-5 missing after LoadCatalog")
	}
	if m.API != "anthropic" || m.Compat == nil || m.Compat.ForceAdaptiveThinking == nil || !*m.Compat.ForceAdaptiveThinking {
		t.Errorf("metadata lost: API=%q Compat=%+v", m.API, m.Compat)
	}
	if len(m.ThinkingLevelMap) == 0 {
		t.Error("ThinkingLevelMap lost after LoadCatalog")
	}
}
