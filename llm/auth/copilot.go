package auth

import (
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

// Copilot signs in with a GitHub Copilot plan: GitHub's device flow gives a
// GitHub token, which buys short-lived Copilot tokens for
// api.githubcopilot.com. The GitHub token is kept as the refresh token.
type Copilot struct {
	GitHub string // https://github.com when empty
	API    string // https://api.github.com when empty
	HTTP   *http.Client
}

// copilotClientID is the OAuth app editors sign in to Copilot with.
const copilotClientID = "Iv1.b507a08c87ecfe98"

func (g *Copilot) github() string {
	return strings.TrimSuffix(cmpOr(g.GitHub, "https://github.com"), "/")
}

func (g *Copilot) api() string {
	return strings.TrimSuffix(cmpOr(g.API, "https://api.github.com"), "/")
}

func (g *Copilot) Login(ctx context.Context, in Interaction) (Credential, error) {
	var dev struct {
		DeviceCode      string  `json:"device_code"`
		UserCode        string  `json:"user_code"`
		VerificationURI string  `json:"verification_uri"`
		Interval        float64 `json:"interval"`
		ExpiresIn       float64 `json:"expires_in"`
	}
	if _, err := g.post(ctx, "/login/device/code", url.Values{
		"client_id": {copilotClientID}, "scope": {"read:user"},
	}, &dev); err != nil {
		return Credential{}, err
	}
	if dev.DeviceCode == "" || dev.UserCode == "" || !strings.HasPrefix(dev.VerificationURI, "https://") {
		return Credential{}, errors.New("auth: bad GitHub device code response")
	}
	in.DeviceCode(dev.UserCode, dev.VerificationURI)

	interval := time.Duration(max(dev.Interval, 1) * float64(time.Second))
	wait := time.Duration(cmpOrFloat(dev.ExpiresIn, 900) * float64(time.Second))
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return Credential{}, fmt.Errorf("auth: GitHub device code: %w", ctx.Err())
		case <-time.After(interval):
		}
		// GitHub answers pending polls with 200 and an error field.
		var t struct {
			tokenResponse
			oauthError
		}
		if _, err := g.post(ctx, "/login/oauth/access_token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {copilotClientID},
			"device_code": {dev.DeviceCode},
		}, &t); err != nil {
			return Credential{}, err
		}
		switch t.Error {
		case "":
			if t.AccessToken == "" {
				return Credential{}, errors.New("auth: GitHub sent no access token")
			}
			c, err := g.exchange(ctx, t.AccessToken)
			if err != nil {
				return Credential{}, err
			}
			c.Email = g.login(ctx, t.AccessToken)
			return c, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return Credential{}, errors.New("auth: the GitHub sign-in was declined")
		case "expired_token":
			return Credential{}, errors.New("auth: the GitHub device code expired")
		default:
			return Credential{}, fmt.Errorf("auth: GitHub: %s", cmpOr(t.Description, t.Error))
		}
	}
}

func (g *Copilot) Refresh(ctx context.Context, c Credential) (Credential, error) {
	fresh, err := g.exchange(ctx, c.Refresh)
	if err != nil {
		return Credential{}, err
	}
	fresh.Email = c.Email
	return fresh, nil
}

// exchange trades a GitHub token for a Copilot token. The answer names the
// API host of the user's plan (Business and Enterprise have their own).
func (g *Copilot) exchange(ctx context.Context, github string) (Credential, error) {
	var t struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	status, err := g.get(ctx, "/copilot_internal/v2/token", github, &t)
	switch {
	case status == http.StatusUnauthorized:
		return Credential{}, errors.New("auth: the GitHub sign-in has expired; sign in again")
	case status == http.StatusForbidden || status == http.StatusNotFound:
		return Credential{}, errors.New("auth: this GitHub account has no Copilot plan")
	case err != nil:
		return Credential{}, err
	case t.Token == "":
		return Credential{}, errors.New("auth: GitHub sent no Copilot token")
	}
	c := Credential{
		Kind:    KindOAuth,
		Method:  "copilot",
		Access:  t.Token,
		Refresh: github,
		Expires: time.Unix(t.ExpiresAt, 0),
	}
	if strings.HasPrefix(t.Endpoints.API, "https://") {
		c.BaseURL = t.Endpoints.API
	}
	return c, nil
}

// login is the GitHub user name, to show the account; "" if it can't be had.
func (g *Copilot) login(ctx context.Context, github string) string {
	var u struct {
		Login string `json:"login"`
	}
	g.get(ctx, "/user", github, &u)
	return u.Login
}

// CopilotHeaders authenticates a Copilot request. The API refuses clients
// that don't name an editor integration.
func CopilotHeaders(c Credential) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Access)
	h.Set("Copilot-Integration-Id", "vscode-chat")
	h.Set("Editor-Version", "Talos/1.0")
	return h
}

func (g *Copilot) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (g *Copilot) post(ctx context.Context, path string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.github()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return g.do(req, out)
}

func (g *Copilot) get(ctx context.Context, path, github string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api()+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "token "+github)
	return g.do(req, out)
}

func (g *Copilot) do(req *http.Request, out any) (int, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := g.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("auth: GitHub %s: %d %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return resp.StatusCode, json.Unmarshal(data, out)
}
