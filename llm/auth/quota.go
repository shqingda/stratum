package auth

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shqingda/stratum/proto"
)

const quotaTTL = 2 * time.Minute

// QuotaStore caches subscription account quotas in memory and on disk so
// accounts.list returns immediately without blocking on network calls.
type QuotaStore struct {
	Path      string // defaults to filepath.Join(Dir, "quota-cache.json")
	HTTP      *http.Client
	ChatGPT   string // https://chatgpt.com/backend-api when empty
	GitHubAPI string // https://api.github.com when empty

	once     sync.Once
	mu       sync.RWMutex
	entries  map[string]proto.AccountQuota
	inFlight map[string]bool

	// Background work: refreshes and the one cache-file writer. idle is
	// closed when busy drops to zero; see Flush.
	bmu     sync.Mutex
	busy    int
	idle    chan struct{}
	dirty   bool // entries changed since the writer took its last snapshot
	writing bool
}

// Quotas is the process-wide quota cache.
var Quotas = &QuotaStore{}

func (q *QuotaStore) path() string {
	if q.Path != "" {
		return q.Path
	}
	return filepath.Join(Dir, "quota-cache.json")
}

func (q *QuotaStore) loadOnce() {
	q.once.Do(func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.entries == nil {
			q.entries = make(map[string]proto.AccountQuota)
		}
		if q.inFlight == nil {
			q.inFlight = make(map[string]bool)
		}
		b, err := os.ReadFile(q.path())
		if err != nil || len(b) == 0 {
			return
		}
		var m map[string]proto.AccountQuota
		if json.Unmarshal(b, &m) == nil {
			for k, v := range m {
				q.entries[k] = v
			}
		}
	})
}

// Get returns the cached quota for accountID in O(1) without I/O or network.
func (q *QuotaStore) Get(accountID string) (*proto.AccountQuota, bool) {
	q.loadOnce()
	q.mu.RLock()
	defer q.mu.RUnlock()
	v, ok := q.entries[accountID]
	if !ok {
		return nil, false
	}
	cp := v
	return &cp, true
}

// Set updates the cached quota in memory and persists asynchronously.
func (q *QuotaStore) Set(accountID string, val proto.AccountQuota) {
	q.loadOnce()
	if val.UpdatedAt.IsZero() {
		val.UpdatedAt = time.Now()
	}
	q.mu.Lock()
	q.entries[accountID] = val
	q.mu.Unlock()

	q.bmu.Lock()
	defer q.bmu.Unlock()
	q.dirty = true
	if !q.writing {
		q.writing = true
		q.beginLocked()
		go q.writeLoop()
	}
}

// Flush waits until no refresh is running and the cache file holds every
// quota set before the call.
func (q *QuotaStore) Flush() {
	q.bmu.Lock()
	idle := q.idle
	q.bmu.Unlock()
	if idle != nil {
		<-idle
	}
}

func (q *QuotaStore) beginLocked() {
	if q.busy == 0 {
		q.idle = make(chan struct{})
	}
	q.busy++
}

func (q *QuotaStore) endLocked() {
	q.busy--
	if q.busy == 0 {
		close(q.idle)
		q.idle = nil
	}
}

// writeLoop is the store's only cache-file writer, so writes never share the
// .tmp file and an older snapshot never lands after a newer one.
func (q *QuotaStore) writeLoop() {
	for {
		q.bmu.Lock()
		if !q.dirty {
			q.writing = false
			q.endLocked()
			q.bmu.Unlock()
			return
		}
		q.dirty = false
		q.bmu.Unlock()

		q.mu.RLock()
		snap := make(map[string]proto.AccountQuota, len(q.entries))
		for k, v := range q.entries {
			snap[k] = v
		}
		q.mu.RUnlock()
		q.persist(snap)
	}
}

func (q *QuotaStore) persist(snap map[string]proto.AccountQuota) {
	p := q.path()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// RefreshAsync refreshes stale subscription quotas in the background with a
// strict 2.5s timeout and invokes onUpdate if any quota changed.
func (q *QuotaStore) RefreshAsync(ctx context.Context, accounts []string, creds Store, onUpdate func()) {
	q.loadOnce()
	now := time.Now()
	var due []string
	q.mu.Lock()
	for _, id := range accounts {
		if q.inFlight[id] {
			continue
		}
		if cur, ok := q.entries[id]; ok && now.Sub(cur.UpdatedAt) < quotaTTL {
			continue
		}
		q.inFlight[id] = true
		due = append(due, id)
	}
	q.mu.Unlock()

	if len(due) == 0 || creds == nil {
		return
	}
	q.bmu.Lock()
	q.beginLocked()
	q.bmu.Unlock()
	go func() {
		var wg sync.WaitGroup
		var changed bool
		var cmu sync.Mutex
		for _, id := range due {
			wg.Add(1)
			go func(accID string) {
				defer wg.Done()
				defer func() {
					q.mu.Lock()
					delete(q.inFlight, accID)
					q.mu.Unlock()
				}()
				c, err := creds.Get(accID)
				if err != nil || !Subscription(c) {
					return
				}
				fctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
				defer cancel()
				quota, ok := q.fetch(fctx, accID, c)
				if !ok {
					return
				}
				q.Set(accID, quota)
				cmu.Lock()
				changed = true
				cmu.Unlock()
			}(id)
		}
		wg.Wait()
		// Done before onUpdate, so a callback that calls Flush does not wait
		// for itself.
		q.bmu.Lock()
		q.endLocked()
		q.bmu.Unlock()
		if changed && onUpdate != nil {
			onUpdate()
		}
	}()
}

func (q *QuotaStore) client() *http.Client {
	if q.HTTP != nil {
		return q.HTTP
	}
	return &http.Client{Timeout: 2500 * time.Millisecond}
}

func (q *QuotaStore) fetch(ctx context.Context, accountID string, c Credential) (proto.AccountQuota, bool) {
	switch c.Method {
	case "codex", "chatgpt":
		base := strings.TrimSuffix(cmpOr(q.ChatGPT, "https://chatgpt.com/backend-api"), "/")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/wham/usage", nil)
		if err != nil || c.Access == "" {
			return proto.AccountQuota{}, false
		}
		req.Header.Set("Authorization", "Bearer "+c.Access)
		if c.AccountID != "" {
			req.Header.Set("ChatGPT-Account-Id", c.AccountID)
		}
		resp, err := q.client().Do(req)
		if err != nil {
			return proto.AccountQuota{}, false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return proto.AccountQuota{}, false
		}
		var body struct {
			PlanType  string `json:"plan_type"`
			RateLimit struct {
				PrimaryWindow struct {
					UsedPercent       float64 `json:"used_percent"`
					ResetAfterSeconds int64   `json:"reset_after_seconds"`
					LimitWindowSec    int64   `json:"limit_window_seconds"`
				} `json:"primary_window"`
			} `json:"rate_limit"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return proto.AccountQuota{}, false
		}
		rem := int(math.Round(100 - body.RateLimit.PrimaryWindow.UsedPercent))
		rem = max(0, min(100, rem))
		var resetAt time.Time
		if body.RateLimit.PrimaryWindow.ResetAfterSeconds > 0 {
			resetAt = time.Now().Add(time.Duration(body.RateLimit.PrimaryWindow.ResetAfterSeconds) * time.Second)
		}
		win := "5h"
		if body.RateLimit.PrimaryWindow.LimitWindowSec >= 86400 {
			win = "7d"
		}
		return proto.AccountQuota{
			RemainingPct: rem,
			ResetAt:      resetAt,
			Window:       win,
			Plan:         body.PlanType,
			UpdatedAt:    time.Now(),
		}, true

	case "copilot":
		token := cmpOr(c.Refresh, c.Access)
		if token == "" {
			return proto.AccountQuota{}, false
		}
		base := strings.TrimSuffix(cmpOr(q.GitHubAPI, "https://api.github.com"), "/")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/copilot_internal/user", nil)
		if err != nil {
			return proto.AccountQuota{}, false
		}
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := q.client().Do(req)
		if err != nil {
			return proto.AccountQuota{}, false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return proto.AccountQuota{}, false
		}
		var body struct {
			CopilotPlan    string `json:"copilot_plan"`
			QuotaResetDate string `json:"quota_reset_date"`
			QuotaSnapshots struct {
				PremiumInteractions struct {
					PercentRemaining float64 `json:"percent_remaining"`
				} `json:"premium_interactions"`
			} `json:"quota_snapshots"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return proto.AccountQuota{}, false
		}
		rem := max(0, min(100, int(math.Round(body.QuotaSnapshots.PremiumInteractions.PercentRemaining))))
		var resetAt time.Time
		if body.QuotaResetDate != "" {
			if t, err := time.Parse(time.RFC3339, body.QuotaResetDate); err == nil {
				resetAt = t
			} else if t, err := time.Parse("2006-01-02", body.QuotaResetDate); err == nil {
				resetAt = t
			}
		}
		return proto.AccountQuota{
			RemainingPct: rem,
			ResetAt:      resetAt,
			Window:       "monthly",
			Plan:         body.CopilotPlan,
			UpdatedAt:    time.Now(),
		}, true
	}
	return proto.AccountQuota{}, false
}

// RecordResponseHeaders extracts rate-limit/quota headers from an LLM HTTP
// response and updates the quota cache with zero extra requests.
func (q *QuotaStore) RecordResponseHeaders(accountID string, hdr http.Header) {
	if accountID == "" || len(hdr) == 0 {
		return
	}
	if quota, ok := ParseQuotaHeaders(hdr, time.Now()); ok {
		q.Set(accountID, quota)
	}
}

// ParseQuotaHeaders parses Codex, Anthropic, or generic rate-limit headers.
func ParseQuotaHeaders(hdr http.Header, now time.Time) (proto.AccountQuota, bool) {
	if usedStr := hdr.Get("x-codex-primary-used-percent"); usedStr != "" {
		if used, err := strconv.ParseFloat(usedStr, 64); err == nil {
			rem := max(0, min(100, int(math.Round(100-used))))
			var resetAt time.Time
			if secStr := hdr.Get("x-codex-primary-reset-after-seconds"); secStr != "" {
				if sec, err := strconv.ParseInt(secStr, 10, 64); err == nil && sec > 0 {
					resetAt = now.Add(time.Duration(sec) * time.Second)
				}
			}
			return proto.AccountQuota{RemainingPct: rem, ResetAt: resetAt, Window: "5h", UpdatedAt: now}, true
		}
	}
	if utilStr := hdr.Get("anthropic-ratelimit-unified-5h-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			rem := max(0, min(100, int(math.Round((1-util)*100))))
			var resetAt time.Time
			if tsStr := hdr.Get("anthropic-ratelimit-unified-5h-reset"); tsStr != "" {
				if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil && ts > 0 {
					resetAt = time.Unix(ts, 0)
				}
			}
			return proto.AccountQuota{RemainingPct: rem, ResetAt: resetAt, Window: "5h", UpdatedAt: now}, true
		}
	}
	if remStr, limStr := hdr.Get("x-ratelimit-remaining-requests"), hdr.Get("x-ratelimit-limit-requests"); remStr != "" && limStr != "" {
		remVal, err1 := strconv.ParseFloat(remStr, 64)
		limVal, err2 := strconv.ParseFloat(limStr, 64)
		if err1 == nil && err2 == nil && limVal > 0 {
			pct := max(0, min(100, int(math.Round(remVal/limVal*100))))
			return proto.AccountQuota{RemainingPct: pct, UpdatedAt: now}, true
		}
	}
	return proto.AccountQuota{}, false
}
