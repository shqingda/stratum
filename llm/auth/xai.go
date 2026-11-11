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

// XAI signs in with a SuperGrok or X Premium plan through xAI's device
// code flow; the token calls api.x.ai.
type XAI struct {
	AuthBase string // https://auth.x.ai when empty
	HTTP     *http.Client
}

const (
	xaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiScope    = "openid profile email offline_access grok-cli:access api:access"
)

func (x *XAI) base() string { return strings.TrimSuffix(cmpOr(x.AuthBase, "https://auth.x.ai"), "/") }

func (x *XAI) Login(ctx context.Context, in Interaction) (Credential, error) {
	var dev struct {
		DeviceCode      string  `json:"device_code"`
		UserCode        string  `json:"user_code"`
		VerificationURI string  `json:"verification_uri"`
		Complete        string  `json:"verification_uri_complete"`
		Interval        float64 `json:"interval"`
		ExpiresIn       float64 `json:"expires_in"`
	}
	if _, err := x.post(ctx, "/oauth2/device/code", url.Values{
		"client_id": {xaiClientID}, "scope": {xaiScope}, "referrer": {"talos"},
	}, &dev); err != nil {
		return Credential{}, err
	}
	uri := cmpOr(dev.Complete, dev.VerificationURI)
	if dev.DeviceCode == "" || dev.UserCode == "" || !strings.HasPrefix(uri, "https://") {
		return Credential{}, errors.New("auth: bad xAI device code response")
	}
	in.DeviceCode(dev.UserCode, uri)

	interval := time.Duration(max(dev.Interval, 1) * float64(time.Second))
	wait := time.Duration(cmpOrFloat(dev.ExpiresIn, 900) * float64(time.Second))
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return Credential{}, fmt.Errorf("auth: xAI device code: %w", ctx.Err())
		case <-time.After(interval):
		}
		var t tokenResponse
		body, err := x.post(ctx, "/oauth2/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {xaiClientID},
			"device_code": {dev.DeviceCode},
		}, &t)
		if err == nil {
			return xaiCredential(t, "")
		}
		switch body.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied", "authorization_denied":
			return Credential{}, errors.New("auth: the xAI sign-in was declined")
		case "expired_token":
			return Credential{}, errors.New("auth: the xAI device code expired")
		default:
			return Credential{}, err
		}
	}
}

func (x *XAI) Refresh(ctx context.Context, c Credential) (Credential, error) {
	var t tokenResponse
	if _, err := x.post(ctx, "/oauth2/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {xaiClientID}, "refresh_token": {c.Refresh},
	}, &t); err != nil {
		return Credential{}, err
	}
	return xaiCredential(t, c.Refresh)
}

func xaiCredential(t tokenResponse, refresh string) (Credential, error) {
	if t.AccessToken == "" {
		return Credential{}, errors.New("auth: xAI sent no access token")
	}
	email, _ := jwtClaims(t.IDToken)["email"].(string)
	return Credential{
		Kind:    KindOAuth,
		Method:  "xai",
		Access:  t.AccessToken,
		Refresh: cmpOr(t.RefreshToken, refresh),
		Expires: time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600)) * time.Second),
		Email:   email,
	}, nil
}

type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// post sends a form and decodes a 2xx answer into out; on another status it
// returns the OAuth error body too.
func (x *XAI) post(ctx context.Context, path string, form url.Values, out any) (oauthError, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, x.base()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthError{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	hc := x.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return oauthError{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e oauthError
		json.Unmarshal(data, &e)
		return e, fmt.Errorf("auth: xAI %s: %d %s", path, resp.StatusCode, strings.TrimSpace(cmpOr(e.Description, string(data))))
	}
	return oauthError{}, json.Unmarshal(data, out)
}

func cmpOrFloat(a, b float64) float64 {
	if a > 0 {
		return a
	}
	return b
}
