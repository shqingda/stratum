// Package auth signs in to model providers and keeps their credentials:
// API keys and OAuth tokens, stored in the macOS keychain and refreshed
// before they expire.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"talos/llm"
)

// Kind is how a credential authenticates.
type Kind string

const (
	KindAPIKey Kind = "api_key"
	KindOAuth  Kind = "oauth"
)

// Credential is what a provider account signs in with.
type Credential struct {
	Kind    Kind      `json:"kind"`
	Key     string    `json:"key,omitempty"`     // KindAPIKey
	Access  string    `json:"access,omitempty"`  // KindOAuth
	Refresh string    `json:"refresh,omitempty"` // KindOAuth
	Expires time.Time `json:"expires,omitzero"`  // KindOAuth
	// AccountID is the provider's id of the account, where requests name it
	// (ChatGPT's chatgpt-account-id).
	AccountID string `json:"accountId,omitempty"`
	// Email names the account to the user, where the provider tells it.
	Email string `json:"email,omitempty"`
	// BaseURL overrides the provider's endpoint (a proxy, or a custom
	// OpenAI-compatible server).
	BaseURL string `json:"baseUrl,omitempty"`
}

// ErrNotFound means no credential is stored for an account.
var ErrNotFound = errors.New("auth: not signed in")

// Store keeps credentials by account: a provider id, or provider/label for
// a second account of one provider.
type Store interface {
	Get(account string) (Credential, error)
	Set(account string, c Credential) error
	Delete(account string) error
	List() ([]string, error)
}

// Keychain stores credentials in the macOS keychain under one service. The
// keychain cannot list items by service, so an index item names them.
type Keychain struct {
	Service string // "talos" when empty
	mu      sync.Mutex
}

const indexAccount = "_accounts"

func (k *Keychain) service() string {
	if k.Service == "" {
		return "talos"
	}
	return k.Service
}

func (k *Keychain) Get(account string) (Credential, error) {
	s, err := keyring.Get(k.service(), account)
	if errors.Is(err, keyring.ErrNotFound) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}
	var c Credential
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return Credential{}, fmt.Errorf("auth: credential of %s: %w", account, err)
	}
	return c, nil
}

func (k *Keychain) Set(account string, c Credential) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := keyring.Set(k.service(), account, string(b)); err != nil {
		return err
	}
	return k.index(func(m map[string]bool) { m[account] = true })
}

func (k *Keychain) Delete(account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := keyring.Delete(k.service(), account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return k.index(func(m map[string]bool) { delete(m, account) })
}

func (k *Keychain) List() ([]string, error) {
	m, err := k.readIndex()
	if err != nil {
		return nil, err
	}
	return sortedKeys(m), nil
}

func (k *Keychain) readIndex() (map[string]bool, error) {
	m := map[string]bool{}
	s, err := keyring.Get(k.service(), indexAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	_ = json.Unmarshal([]byte(s), &names)
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

func (k *Keychain) index(change func(map[string]bool)) error {
	m, err := k.readIndex()
	if err != nil {
		return err
	}
	change(m)
	b, _ := json.Marshal(sortedKeys(m))
	return keyring.Set(k.service(), indexAccount, string(b))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Memory is a Store in memory, for tests.
type Memory struct {
	mu sync.Mutex
	m  map[string]Credential
}

func (s *Memory) Get(account string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[account]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}

func (s *Memory) Set(account string, c Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]Credential{}
	}
	s.m[account] = c
	return nil
}

func (s *Memory) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, account)
	return nil
}

func (s *Memory) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]bool{}
	for k := range s.m {
		m[k] = true
	}
	return sortedKeys(m), nil
}

// Refresher trades a refresh token for new tokens.
type Refresher func(ctx context.Context, c Credential) (Credential, error)

// Source hands out an account's current credential, refreshing an OAuth
// token shortly before it expires and saving the new one. It is safe for
// concurrent use: one refresh runs at a time.
type Source struct {
	Store   Store
	Account string
	Refresh Refresher // nil for API keys
	// Now is the clock; time.Now when nil.
	Now func() time.Time

	mu sync.Mutex
}

// RefreshMargin is how long before expiry a token is refreshed.
const RefreshMargin = 5 * time.Minute

// Credential returns the account's credential, refreshed when it is about
// to expire.
func (s *Source) Credential(ctx context.Context) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.Store.Get(s.Account)
	if err != nil {
		return Credential{}, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	if c.Kind != KindOAuth || c.Expires.IsZero() || now().Add(RefreshMargin).Before(c.Expires) {
		return c, nil
	}
	if s.Refresh == nil || c.Refresh == "" {
		return Credential{}, fmt.Errorf("auth: %s: the session expired; sign in again", s.Account)
	}
	fresh, err := s.Refresh(ctx, c)
	if err != nil {
		return Credential{}, fmt.Errorf("auth: %s: refresh: %w", s.Account, err)
	}
	if fresh.Refresh == "" {
		fresh.Refresh = c.Refresh // some providers keep the refresh token
	}
	if fresh.Email == "" {
		fresh.Email = c.Email
	}
	fresh.BaseURL = c.BaseURL
	if err := s.Store.Set(s.Account, fresh); err != nil {
		return Credential{}, err
	}
	return fresh, nil
}

// Auth returns an llm.Auth that sets headers from the current credential.
func (s *Source) Auth(headers func(Credential) http.Header) llm.Auth {
	return func(ctx context.Context) (http.Header, error) {
		c, err := s.Credential(ctx)
		if err != nil {
			return nil, err
		}
		return headers(c), nil
	}
}
