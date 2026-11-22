package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pkce returns a code verifier and its S256 challenge.
func pkce() (verifier, challenge string) {
	b := make([]byte, 32)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	return verifier, pkceChallenge(verifier)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// callback is a loopback server that waits for an OAuth redirect.
type callback struct {
	srv  *http.Server
	ln   net.Listener
	got  chan url.Values
	errs chan error
}

// listenCallback serves path on 127.0.0.1:port and delivers the query of the
// first redirect that carries a code and, unless state is empty, the
// matching state. Port 0 picks a free port.
func listenCallback(port int, path, state string) (*callback, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	cb := &callback{ln: ln, got: make(chan url.Values, 1), errs: make(chan error, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Browsers keep spare connections open; closing them stops a later
		// sign-in's redirect from landing on this finished one.
		w.Header().Set("Connection", "close")
		if e := q.Get("error"); e != "" {
			msg := e
			if d := q.Get("error_description"); d != "" {
				msg += ": " + d
			}
			page(w, "Sign-in failed", msg)
			cb.fail(errors.New(msg))
			return
		}
		if state != "" && q.Get("state") != state {
			w.WriteHeader(http.StatusBadRequest)
			page(w, "Sign-in failed", "The sign-in did not come from Stratum. Start it again from the app.")
			return
		}
		if q.Get("code") == "" {
			w.WriteHeader(http.StatusBadRequest)
			page(w, "Sign-in failed", "No authorization code came back.")
			return
		}
		page(w, "Signed in", "You can close this tab and return to Stratum.")
		select {
		case cb.got <- q:
		default:
		}
	})
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go cb.srv.Serve(ln)
	return cb, nil
}

func (cb *callback) fail(err error) {
	select {
	case cb.errs <- err:
	default:
	}
}

func (cb *callback) port() int { return cb.ln.Addr().(*net.TCPAddr).Port }

func (cb *callback) wait(ctx context.Context) (url.Values, error) {
	select {
	case q := <-cb.got:
		return q, nil
	case err := <-cb.errs:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// redirectURI is the callback's address for path.
func (cb *callback) redirectURI(host, path string) string {
	return fmt.Sprintf("http://%s:%d%s", host, cb.port(), path)
}

func (cb *callback) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cb.srv.Shutdown(ctx)
	cb.ln.Close() // Shutdown misses it when Serve has not started yet
}

func page(w io.Writer, title, msg string) {
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%[1]s</title>
<style>body{font:15px/1.5 -apple-system,system-ui,sans-serif;display:grid;place-items:center;height:100vh;margin:0}
@media(prefers-color-scheme:dark){body{background:#151515;color:#eee}}</style>
<div><h1 style="font-size:20px;font-weight:600;margin:0 0 6px">%[1]s</h1><p style="margin:0;opacity:.7">%[2]s</p></div>`,
		html.EscapeString(title), html.EscapeString(msg))
}

// tokenResponse is an OAuth token endpoint's answer.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// postForm posts a form to a token endpoint.
func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return tokenResponse{}, fmt.Errorf("token endpoint: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var t tokenResponse
	if err := json.Unmarshal(b, &t); err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: %w", err)
	}
	if t.AccessToken == "" {
		return tokenResponse{}, errors.New("token endpoint: no access token")
	}
	return t, nil
}

// jwtClaims decodes a JWT's payload without checking it: the token came
// straight from the provider over TLS, and only names the account.
func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}
