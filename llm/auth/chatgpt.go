package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"net/http"
)

// ChatGPT is Sign in with ChatGPT: OpenAI's sign-in for third-party agents.
// Each sign-in registers a client for this installation, and the token
// calls api.openai.com on the user's ChatGPT plan.
type ChatGPT struct {
	AuthBase string // https://auth.openai.com when empty
	HTTP     *http.Client
	Port     int // 1455 when zero; OpenAI only accepts that port
	// HostID names this installation, a UUID; DeviceID when nil.
	HostID func() (string, error)
}

const (
	chatgptScope       = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	chatgptDirectScope = "chatgpt.tokens.use.direct"
	chatgptResource    = "https://api.openai.com/v1"
)

func (c *ChatGPT) base() string {
	return strings.TrimSuffix(cmpOr(c.AuthBase, "https://auth.openai.com"), "/")
}

func (c *ChatGPT) redirect() string {
	return fmt.Sprintf("http://127.0.0.1:%d/auth/callback", cmpOrInt(c.Port, 1455))
}

func (c *ChatGPT) Login(ctx context.Context, in Interaction) (Credential, error) {
	hostID := c.HostID
	if hostID == nil {
		hostID = DeviceID
	}
	host, err := hostID()
	if err != nil {
		return Credential{}, err
	}
	verifier, challenge := pkce()
	state, nonce := randomURLValue(), randomURLValue()
	cb, err := listenCallback(cmpOrInt(c.Port, 1455), "/auth/callback", state)
	if err != nil {
		return Credential{}, fmt.Errorf("auth: port %d is taken, probably by another sign-in or the Codex CLI; finish or cancel it and try again: %w", cmpOrInt(c.Port, 1455), err)
	}
	defer cb.close()
	q := url.Values{
		"client_id":             {"dynamic_agent_client"},
		"agent_name_hint":       {"Talos"},
		"ext_agent_host_id":     {"urn:uuid:" + host},
		"response_type":         {"code"},
		"redirect_uri":          {c.redirect()},
		"resource":              {chatgptResource},
		"scope":                 {chatgptScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"nonce":                 {nonce},
	}
	if err := in.OpenURL(c.base() + "/api/accounts/authorize?" + q.Encode()); err != nil {
		return Credential{}, err
	}
	got, err := cb.wait(ctx)
	if err != nil {
		return Credential{}, err
	}
	// OpenAI registered a client for this sign-in and names it here.
	client := strings.TrimSpace(got.Get("client_id"))
	if client == "" {
		return Credential{}, errors.New("auth: ChatGPT issued no client id")
	}
	t, err := postForm(ctx, c.HTTP, c.base()+"/api/accounts/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {client},
		"code":          {got.Get("code")},
		"code_verifier": {verifier},
		"redirect_uri":  {c.redirect()},
		"resource":      {chatgptResource},
	})
	if err != nil {
		return Credential{}, err
	}
	return chatgptCredential(t, client)
}

func (c *ChatGPT) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	if cred.ClientID == "" {
		return Credential{}, errors.New("auth: the ChatGPT sign-in has no client id; sign in again")
	}
	t, err := postForm(ctx, c.HTTP, c.base()+"/api/accounts/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {cred.ClientID},
		"refresh_token": {cred.Refresh},
		"resource":      {chatgptResource},
	})
	if err != nil {
		return Credential{}, err
	}
	return chatgptCredential(t, cred.ClientID)
}

func chatgptCredential(t tokenResponse, client string) (Credential, error) {
	if !strings.Contains(" "+t.Scope+" ", " "+chatgptDirectScope+" ") {
		return Credential{}, errors.New("auth: ChatGPT did not grant API access (" + chatgptDirectScope + ")")
	}
	email, _ := jwtClaims(t.IDToken)["email"].(string)
	return Credential{
		Kind:     KindOAuth,
		Method:   "chatgpt",
		Access:   t.AccessToken,
		Refresh:  t.RefreshToken,
		Expires:  time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
		ClientID: client,
		Email:    email,
	}, nil
}

// Dir is Stratum's data directory (~/.stratum, falling back to existing ~/.talos).
var Dir = func() string {
	home, _ := os.UserHomeDir()
	stratum := filepath.Join(home, ".stratum")
	if _, err := os.Stat(stratum); err == nil {
		return stratum
	}
	legacy := filepath.Join(home, ".talos")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return stratum
}()

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// DeviceID returns this installation's id, a random UUID made on first use
// and kept in Dir.
func DeviceID() (string, error) {
	path := filepath.Join(Dir, "device-id")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); uuidPattern.MatchString(id) {
			return id, nil
		}
	}
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	if err := os.MkdirAll(Dir, 0o700); err != nil {
		return "", err
	}
	return id, os.WriteFile(path, []byte(id+"\n"), 0o600)
}

func randomURLValue() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cmpOrInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
