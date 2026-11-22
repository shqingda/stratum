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
	"strconv"
	"strings"
	"time"
)

// Interaction is how a login talks to the user.
type Interaction struct {
	// OpenURL shows the sign-in page, usually in the browser.
	OpenURL func(u string) error
	// DeviceCode tells the user the code to enter at uri.
	DeviceCode func(code, uri string)
	// Paste asks for the address the browser landed on, for when the
	// callback port is taken. Nil gives up instead.
	Paste func(ctx context.Context) (string, error)
}

// Codex signs in with a ChatGPT account, whose plan pays for Codex models.
// The flow is the one OpenAI's Codex CLI uses; the app names itself in it
// as "talos", its name from before the rename to Stratum.
type Codex struct {
	AuthBase string // https://auth.openai.com when empty
	HTTP     *http.Client
	// Port is the loopback port of the browser flow's redirect; OpenAI only
	// accepts 1455, the default.
	Port int
}

const (
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexScope    = "openid profile email offline_access"
	codexClaim    = "https://api.openai.com/auth"
	// DeviceTimeout is how long a device code waits to be entered.
	DeviceTimeout = 15 * time.Minute
)

func (c *Codex) base() string {
	if c.AuthBase == "" {
		return "https://auth.openai.com"
	}
	return strings.TrimSuffix(c.AuthBase, "/")
}

func (c *Codex) port() int {
	if c.Port == 0 {
		return 1455
	}
	return c.Port
}

func (c *Codex) redirect() string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", c.port())
}

// AuthorizeURL builds the browser sign-in address.
func (c *Codex) AuthorizeURL(challenge, state string) string {
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {codexClientID},
		"redirect_uri":               {c.redirect()},
		"scope":                      {codexScope},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"talos"},
	}
	return c.base() + "/oauth/authorize?" + q.Encode()
}

// Login signs in through the browser. When the callback port is taken (the
// Codex CLI may hold it), the user pastes the address the browser landed on.
func (c *Codex) Login(ctx context.Context, in Interaction) (Credential, error) {
	verifier, challenge := pkce()
	state := randomState()
	cb, listenErr := listenCallback(c.port(), "/auth/callback", state)
	if listenErr != nil && in.Paste == nil {
		return Credential{}, fmt.Errorf("auth: port %d is taken (is the Codex CLI signing in?): %w", c.port(), listenErr)
	}
	if cb != nil {
		defer cb.close()
	}
	if err := in.OpenURL(c.AuthorizeURL(challenge, state)); err != nil {
		return Credential{}, err
	}
	var code string
	var err error
	if cb != nil {
		var q url.Values
		if q, err = cb.wait(ctx); err == nil {
			code = q.Get("code")
		}
	} else {
		var pasted string
		if pasted, err = in.Paste(ctx); err == nil {
			code, err = codeFromRedirect(pasted, state)
		}
	}
	if err != nil {
		return Credential{}, err
	}
	return c.exchange(ctx, code, verifier, c.redirect())
}

// codeFromRedirect takes the code from a pasted redirect address, or a bare
// code.
func codeFromRedirect(s, state string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "code=") {
		if s == "" {
			return "", errors.New("auth: no code")
		}
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("state") != state {
		return "", errors.New("auth: the address is from another sign-in")
	}
	return q.Get("code"), nil
}

// LoginDevice signs in with a code entered on another screen.
func (c *Codex) LoginDevice(ctx context.Context, in Interaction) (Credential, error) {
	var start struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     json.RawMessage `json:"interval"`
	}
	status, err := c.postJSON(ctx, "/api/accounts/deviceauth/usercode", map[string]string{"client_id": codexClientID}, &start)
	if status == http.StatusNotFound {
		return Credential{}, errors.New("auth: device code sign-in is not enabled; use the browser")
	}
	if err != nil {
		return Credential{}, err
	}
	if start.DeviceAuthID == "" || start.UserCode == "" {
		return Credential{}, errors.New("auth: bad device code response")
	}
	interval := 5 * time.Second
	if n, err := strconv.ParseFloat(strings.Trim(string(start.Interval), `" `), 64); err == nil && n > 0 {
		interval = time.Duration(n * float64(time.Second))
	}
	in.DeviceCode(start.UserCode, c.base()+"/codex/device")

	ctx, cancel := context.WithTimeout(ctx, DeviceTimeout)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return Credential{}, fmt.Errorf("auth: device code: %w", ctx.Err())
		case <-time.After(interval):
		}
		var got struct {
			Code     string `json:"authorization_code"`
			Verifier string `json:"code_verifier"`
		}
		status, err := c.postJSON(ctx, "/api/accounts/deviceauth/token",
			map[string]string{"device_auth_id": start.DeviceAuthID, "user_code": start.UserCode}, &got)
		switch {
		case err == nil:
			if got.Code == "" || got.Verifier == "" {
				return Credential{}, errors.New("auth: bad device token response")
			}
			return c.exchange(ctx, got.Code, got.Verifier, c.base()+"/deviceauth/callback")
		case status == http.StatusForbidden || status == http.StatusNotFound,
			strings.Contains(err.Error(), "deviceauth_authorization_pending"):
		case strings.Contains(err.Error(), "slow_down"):
			interval += 5 * time.Second
		default:
			return Credential{}, err
		}
	}
}

// Refresh trades the refresh token for new tokens. It is a Refresher.
func (c *Codex) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	t, err := postForm(ctx, c.HTTP, c.base()+"/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.Refresh},
		"client_id":     {codexClientID},
	})
	if err != nil {
		return Credential{}, err
	}
	return codexCredential(t)
}

func (c *Codex) exchange(ctx context.Context, code, verifier, redirect string) (Credential, error) {
	t, err := postForm(ctx, c.HTTP, c.base()+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirect},
	})
	if err != nil {
		return Credential{}, err
	}
	return codexCredential(t)
}

func codexCredential(t tokenResponse) (Credential, error) {
	claims, _ := jwtClaims(t.AccessToken)[codexClaim].(map[string]any)
	account, _ := claims["chatgpt_account_id"].(string)
	if account == "" {
		return Credential{}, errors.New("auth: the token names no ChatGPT account")
	}
	email, _ := jwtClaims(t.IDToken)["email"].(string)
	if email == "" {
		profile, _ := jwtClaims(t.AccessToken)["https://api.openai.com/profile"].(map[string]any)
		email, _ = profile["email"].(string)
	}
	return Credential{
		Kind:      KindOAuth,
		Method:    "codex",
		Access:    t.AccessToken,
		Refresh:   t.RefreshToken,
		Expires:   time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
		AccountID: account,
		Email:     email,
	}, nil
}

// postJSON posts a JSON body and decodes a 2xx answer into out. On another
// status it returns the status and an error carrying the body.
func (c *Codex) postJSON(ctx context.Context, path string, body, out any) (int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+path, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("auth: %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return resp.StatusCode, json.Unmarshal(data, out)
}

// CodexHeaders authenticates a request to the ChatGPT backend.
func CodexHeaders(c Credential) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Access)
	h.Set("chatgpt-account-id", c.AccountID)
	return h
}
