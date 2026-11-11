package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenRouter signs in through the browser and receives an API key that the
// user can see and revoke in OpenRouter's settings.
type OpenRouter struct {
	Base string // https://openrouter.ai when empty
	HTTP *http.Client
}

func (o *OpenRouter) base() string {
	return strings.TrimSuffix(cmpOr(o.Base, "https://openrouter.ai"), "/")
}

func (o *OpenRouter) Login(ctx context.Context, in Interaction) (Credential, error) {
	verifier, challenge := pkce()
	// OpenRouter sends no state back, so a random path keeps stray requests
	// from completing the sign-in.
	path := "/oauth/callback/" + randomState()
	cb, err := listenCallback(0, path, "")
	if err != nil {
		return Credential{}, err
	}
	defer cb.close()
	q := url.Values{
		"callback_url":          {cb.redirectURI("localhost", path)},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if err := in.OpenURL(o.base() + "/auth?" + q.Encode()); err != nil {
		return Credential{}, err
	}
	got, err := cb.wait(ctx)
	if err != nil {
		return Credential{}, err
	}

	b, _ := json.Marshal(map[string]string{"code": got.Get("code"), "code_verifier": verifier, "code_challenge_method": "S256"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base()+"/api/v1/auth/keys", bytes.NewReader(b))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return Credential{}, fmt.Errorf("auth: OpenRouter key exchange: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(data, &out) != nil || out.Key == "" {
		return Credential{}, errors.New("auth: OpenRouter sent no key")
	}
	// The key does not expire, so it is stored like one the user pasted.
	return Credential{Kind: KindAPIKey, Key: out.Key, Method: "openrouter"}, nil
}

// Refresh is never needed: the key does not expire.
func (o *OpenRouter) Refresh(_ context.Context, c Credential) (Credential, error) { return c, nil }
