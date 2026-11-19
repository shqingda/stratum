package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKimiDeviceLogin(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/api/oauth/device_authorization":
			if r.PostForm.Get("client_id") != kimiClientID {
				t.Errorf("client_id %q", r.PostForm.Get("client_id"))
			}
			io.WriteString(w, `{"device_code":"dc-1","user_code":"KM-99","verification_uri":"https://auth.kimi.com/verify","verification_uri_complete":"https://auth.kimi.com/verify?code=KM-99","interval":0.01,"expires_in":60}`)
		case "/api/oauth/token":
			n := polls.Add(1)
			switch n {
			case 1:
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"authorization_pending"}`)
			case 2:
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"slow_down","interval":0.01}`)
			default:
				io.WriteString(w, `{"access_token":"kimi-at","refresh_token":"kimi-rt","expires_in":3600}`)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	k := &Kimi{AuthBase: srv.URL}
	var shown string
	cred, err := k.Login(context.Background(), Interaction{
		DeviceCode: func(code, uri string) { shown = code + " " + uri },
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown != "KM-99 https://auth.kimi.com/verify?code=KM-99" {
		t.Errorf("shown %q", shown)
	}
	if cred.Kind != KindOAuth || cred.Method != "kimi" || cred.Access != "kimi-at" || cred.Refresh != "kimi-rt" || !Subscription(cred) {
		t.Errorf("cred %+v", cred)
	}
	if polls.Load() != 3 {
		t.Errorf("polls %d, want 3", polls.Load())
	}
}

func TestKimiDeviceLoginErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  string
		want string
	}{
		{"denied", "access_denied", "declined"},
		{"expired", "expired_token", "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/oauth/device_authorization":
					io.WriteString(w, `{"device_code":"dc","user_code":"KM-1","verification_uri":"https://auth.kimi.com/v","interval":0.01,"expires_in":60}`)
				case "/api/oauth/token":
					w.WriteHeader(400)
					io.WriteString(w, `{"error":"`+tc.err+`"}`)
				}
			}))
			defer srv.Close()
			_, err := (&Kimi{AuthBase: srv.URL}).Login(context.Background(), Interaction{DeviceCode: func(string, string) {}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got err=%v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestKimiRefresh(t *testing.T) {
	var attempts atomic.Int32
	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("client_id") != kimiClientID {
			t.Errorf("unexpected form %v", r.PostForm)
		}
		n := attempts.Add(1)
		switch mode {
		case "retry":
			if n <= 2 {
				w.WriteHeader(500)
				io.WriteString(w, "temporary")
				return
			}
			io.WriteString(w, `{"access_token":"at2","expires_in":3600}`)
		case "unauth":
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"invalid_grant","error_description":"revoked"}`)
		}
	}))
	defer srv.Close()

	k := &Kimi{AuthBase: srv.URL, backoff: time.Millisecond}

	mode = "retry"
	attempts.Store(0)
	fresh, err := k.Refresh(context.Background(), Credential{Kind: KindOAuth, Method: "kimi", Refresh: "rt1"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Access != "at2" || fresh.Refresh != "rt1" || attempts.Load() != 3 {
		t.Errorf("fresh=%+v attempts=%d", fresh, attempts.Load())
	}

	mode = "unauth"
	attempts.Store(0)
	if _, err := k.Refresh(context.Background(), Credential{Kind: KindOAuth, Method: "kimi", Refresh: "rt1"}); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("401 should not retry, attempts=%d", attempts.Load())
	}
}
