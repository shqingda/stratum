package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaHeadersAndFastCache(t *testing.T) {
	now := time.Unix(1700000000, 0)
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "26")
	h.Set("x-codex-primary-reset-after-seconds", "14400")
	q, ok := ParseQuotaHeaders(h, now)
	if !ok || q.RemainingPct != 74 || !q.ResetAt.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("unexpected codex header quota: %+v %v", q, ok)
	}

	ha := http.Header{}
	ha.Set("anthropic-ratelimit-unified-5h-utilization", "0.35")
	ha.Set("anthropic-ratelimit-unified-5h-reset", "1700003600")
	qa, ok := ParseQuotaHeaders(ha, now)
	if !ok || qa.RemainingPct != 65 || qa.ResetAt.Unix() != 1700003600 {
		t.Fatalf("unexpected anthropic header quota: %+v %v", qa, ok)
	}

	qs := &QuotaStore{Path: filepath.Join(t.TempDir(), "quota.json")}
	t.Cleanup(qs.Flush) // before TempDir's RemoveAll: cleanups run last-in first-out
	qs.Set("codex", q)
	got, ok := qs.Get("codex")
	if !ok || got.RemainingPct != 74 {
		t.Fatalf("cache miss: %+v %v", got, ok)
	}

	qs.Flush()
	reloaded := &QuotaStore{Path: qs.Path}
	if got, ok := reloaded.Get("codex"); !ok || got.RemainingPct != 74 {
		t.Fatalf("cache file after Flush: %+v %v", got, ok)
	}
}

func TestQuotaAsyncRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_type": "pro",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         18,
					"reset_after_seconds":  7200,
					"limit_window_seconds": 18000,
				},
			},
		})
	}))
	defer srv.Close()

	qs := &QuotaStore{
		Path:    filepath.Join(t.TempDir(), "quota.json"),
		ChatGPT: srv.URL,
	}
	t.Cleanup(qs.Flush)
	mem := &MemoryStore{items: map[string]Credential{
		"codex": {Kind: KindOAuth, Method: "codex", Access: "tok", AccountID: "acc-1"},
	}}
	var updated atomic.Bool
	done := make(chan struct{})
	qs.RefreshAsync(context.Background(), []string{"codex"}, mem, func() {
		updated.Store(true)
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for async quota refresh")
	}
	if !updated.Load() {
		t.Fatal("expected onUpdate callback")
	}
	got, ok := qs.Get("codex")
	if !ok || got.RemainingPct != 82 || got.Plan != "pro" {
		t.Fatalf("unexpected refreshed quota: %+v %v", got, ok)
	}
}

type MemoryStore struct {
	items map[string]Credential
}

func (m *MemoryStore) Get(account string) (Credential, error) {
	c, ok := m.items[account]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}
func (m *MemoryStore) Set(account string, c Credential) error { m.items[account] = c; return nil }
func (m *MemoryStore) Delete(account string) error            { delete(m.items, account); return nil }
func (m *MemoryStore) List() ([]string, error) {
	var out []string
	for k := range m.items {
		out = append(out, k)
	}
	return out, nil
}
