package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Kimi signs in with a Kimi Code subscription through Kimi's RFC 8628 device
// code flow against auth.kimi.com. The access token is sent as a bearer token
// to api.kimi.com/coding/v1.
type Kimi struct {
	AuthBase string // https://auth.kimi.com when empty
	HTTP     *http.Client
	backoff  time.Duration // base retry backoff in Refresh; 1s when zero
}

const (
	kimiClientID    = "17e5f671-d194-4dfb-9706-5516cb48c098"
	kimiDefaultHost = "https://auth.kimi.com"
	kimiMaxRetries  = 3
)

func (k *Kimi) base() string {
	host := strings.TrimSpace(k.AuthBase)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("TALOS_KIMI_OAUTH_HOST"))
	}
	if host == "" {
		host = strings.TrimSpace(os.Getenv("KIMI_CODE_OAUTH_HOST"))
	}
	if host == "" {
		host = kimiDefaultHost
	}
	return strings.TrimSuffix(host, "/")
}

func (k *Kimi) client() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (k *Kimi) Login(ctx context.Context, in Interaction) (Credential, error) {
	status, data, err := k.postForm(ctx, "/api/oauth/device_authorization", url.Values{
		"client_id": {kimiClientID},
	})
	if err != nil {
		return Credential{}, err
	}
	if status/100 != 2 {
		return Credential{}, fmt.Errorf("auth: Kimi Code device authorization: %d %s", status, strings.TrimSpace(string(data)))
	}
	var dev struct {
		DeviceCode      string  `json:"device_code"`
		UserCode        string  `json:"user_code"`
		VerificationURI string  `json:"verification_uri"`
		Complete        string  `json:"verification_uri_complete"`
		Interval        float64 `json:"interval"`
		ExpiresIn       float64 `json:"expires_in"`
	}
	if json.Unmarshal(data, &dev) != nil || dev.DeviceCode == "" || dev.UserCode == "" {
		return Credential{}, errors.New("auth: bad Kimi Code device code response")
	}
	uri := cmpOr(kimiTrustedURL(dev.Complete), kimiTrustedURL(dev.VerificationURI))
	if uri == "" {
		return Credential{}, errors.New("auth: bad Kimi Code verification URI")
	}
	in.DeviceCode(dev.UserCode, uri)

	interval := 5 * time.Second
	if dev.Interval > 0 {
		interval = time.Duration(dev.Interval * float64(time.Second))
	}
	wait := 15 * time.Minute
	if dev.ExpiresIn > 0 {
		wait = time.Duration(dev.ExpiresIn * float64(time.Second))
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return Credential{}, fmt.Errorf("auth: Kimi Code device code: %w", ctx.Err())
		case <-time.After(interval):
		}
		status, data, err := k.postForm(ctx, "/api/oauth/token", url.Values{
			"client_id":   {kimiClientID},
			"device_code": {dev.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		})
		if err != nil {
			return Credential{}, err
		}
		if status >= 500 {
			return Credential{}, fmt.Errorf("auth: Kimi Code device token: %d %s", status, strings.TrimSpace(string(data)))
		}
		if status/100 == 2 {
			return kimiCredential(data, "")
		}
		var errBody struct {
			Error       string  `json:"error"`
			Description string  `json:"error_description"`
			Interval    float64 `json:"interval"`
		}
		_ = json.Unmarshal(data, &errBody)
		switch errBody.Error {
		case "authorization_pending":
		case "slow_down":
			if errBody.Interval > 0 {
				interval = time.Duration(errBody.Interval * float64(time.Second))
			} else {
				interval += 5 * time.Second
			}
		case "expired_token":
			return Credential{}, errors.New("auth: the Kimi Code device code expired")
		case "access_denied":
			return Credential{}, errors.New("auth: the Kimi Code sign-in was declined")
		default:
			detail := cmpOr(errBody.Description, errBody.Error)
			if detail == "" {
				detail = strings.TrimSpace(string(data))
			}
			return Credential{}, fmt.Errorf("auth: Kimi Code: %d %s", status, detail)
		}
	}
}

func (k *Kimi) Refresh(ctx context.Context, c Credential) (Credential, error) {
	baseBackoff := k.backoff
	if baseBackoff <= 0 {
		baseBackoff = time.Second
	}
	var lastErr error
	for attempt := 0; attempt <= kimiMaxRetries; attempt++ {
		if attempt > 0 {
			delay := baseBackoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return Credential{}, fmt.Errorf("auth: Kimi Code refresh: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
		status, data, err := k.postForm(ctx, "/api/oauth/token", url.Values{
			"client_id":     {kimiClientID},
			"grant_type":    {"refresh_token"},
			"refresh_token": {c.Refresh},
		})
		if err != nil {
			if ctx.Err() != nil {
				return Credential{}, fmt.Errorf("auth: Kimi Code refresh: %w", ctx.Err())
			}
			lastErr = err
			continue
		}
		if status/100 == 2 {
			return kimiCredential(data, c.Refresh)
		}
		var errBody oauthError
		_ = json.Unmarshal(data, &errBody)
		if status == http.StatusUnauthorized || status == http.StatusForbidden || errBody.Error == "invalid_grant" {
			detail := cmpOr(errBody.Description, errBody.Error)
			if detail == "" {
				detail = strings.TrimSpace(string(data))
			}
			return Credential{}, fmt.Errorf("auth: Kimi Code refresh unauthorized (%d): %s", status, detail)
		}
		if (status == http.StatusTooManyRequests || status >= 500) && attempt < kimiMaxRetries {
			lastErr = fmt.Errorf("auth: Kimi Code refresh: %d %s", status, strings.TrimSpace(string(data)))
			continue
		}
		return Credential{}, fmt.Errorf("auth: Kimi Code refresh: %d %s", status, strings.TrimSpace(string(data)))
	}
	if lastErr != nil {
		return Credential{}, lastErr
	}
	return Credential{}, errors.New("auth: Kimi Code refresh failed")
}

func (k *Kimi) postForm(ctx context.Context, path string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.base()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := k.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, nil
}

func kimiCredential(data []byte, prevRefresh string) (Credential, error) {
	var t tokenResponse
	if err := json.Unmarshal(data, &t); err != nil {
		return Credential{}, fmt.Errorf("auth: Kimi Code token response: %w", err)
	}
	refresh := cmpOr(t.RefreshToken, prevRefresh)
	if t.AccessToken == "" || refresh == "" || t.ExpiresIn <= 0 {
		return Credential{}, errors.New("auth: Kimi Code token response missing fields")
	}
	return Credential{
		Kind:    KindOAuth,
		Method:  "kimi",
		Access:  t.AccessToken,
		Refresh: refresh,
		Expires: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
	}, nil
}

func kimiTrustedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "https" || u.Scheme == "http" {
		return u.String()
	}
	return ""
}
