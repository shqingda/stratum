package llm

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixturesHaveNoSecrets(t *testing.T) {
	cmd := exec.Command("go", "run", "testdata/scrub.go", "-check", "testdata")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture secret check: %v\n%s", err, out)
	}
}

func TestScrubFixtures(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "scrub")
	if out, err := exec.Command("go", "build", "-o", bin, "testdata/scrub.go").CombinedOutput(); err != nil {
		t.Fatalf("build scrub: %v\n%s", err, out)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.sse")
	original := []byte("event: response.created\n" + `data: {"access_token":"opaque-access","refresh_token":"opaque-refresh","id_token":"opaque-id","api_key":"opaque-key","account_id":"opaque-account","organization_id":"opaque-org","user_id":123456,"user":null,"account":{"id":"opaque-account-object"},"organization":[{"id":"opaque-org-object"}],"email":"sample@example.invalid","id":"resp_keep","key":"keep-key","auth":{"access":"opaque-auth-access","refresh":"opaque-auth-refresh","key":"opaque-auth-key"},"delta":"Keep this text.","nested":"{\"access_token\":\"nested-secret\",\"user_id\":\"nested-user\"}","escaped":"\u0073k-examplekey","access\u005ftoken":"unicode-secret"}` + "\n\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(credential, []byte(`{"kind":"oauth","access":"opaque-root-access","refresh":"opaque-root-refresh","key":"opaque-root-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Check every extension, including raw error messages and partial streams.
	raw := filepath.Join(dir, "partial.unknown")
	if err := os.WriteFile(raw, []byte("eyJhbGciOiJub25lIn0.eyJzdWIiOiJmYWtlIn0.signature sk-examplekey user-exampleuser org-exampleorg acct-exampleaccount 12345678-1234-1234-1234-123456789abc sample@example.invalid Bearer opaque-bearer access_token=opaque-query refresh token: opaque-refresh user_id=opaque-user"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-check", dir).CombinedOutput(); err == nil {
		t.Fatal("check accepted unsanitized fixtures")
	} else if bytes.Contains(out, []byte("opaque-access")) || bytes.Contains(out, []byte("examplekey")) {
		t.Fatal("check printed secret contents")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("check mode changed a fixture")
	}
	if out, err := exec.Command(bin, dir).CombinedOutput(); err != nil {
		t.Fatalf("scrub: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "-check", dir).CombinedOutput(); err != nil {
		t.Fatalf("sanitized fixtures fail check: %v\n%s", err, out)
	}
	cleanCredential, err := os.ReadFile(credential)
	if err != nil || bytes.Contains(cleanCredential, []byte("opaque-")) || !bytes.Contains(cleanCredential, []byte("REDACTED_KEY")) || !bytes.Contains(cleanCredential, []byte("REDACTED_TOKEN")) {
		t.Fatal("credential aliases were not scrubbed")
	}
	cleanRaw, err := os.ReadFile(raw)
	if err != nil || bytes.Contains(cleanRaw, []byte("opaque-")) || bytes.Contains(cleanRaw, []byte("example")) || bytes.Contains(cleanRaw, []byte("12345678")) || bytes.Contains(cleanRaw, []byte("eyJ")) {
		t.Fatal("raw secrets survived")
	}
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"opaque-", "nested-secret", "nested-user", "unicode-secret", "123456", "example.invalid", "examplekey"} {
		if bytes.Contains(clean, []byte(secret)) {
			t.Errorf("secret category %q survived", secret)
		}
	}
	prefix := []byte("event: response.created\ndata: ")
	if !bytes.HasPrefix(clean, prefix) || !bytes.HasSuffix(clean, []byte("\n\n")) {
		t.Fatal("SSE framing changed")
	}
	var body map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(clean[len(prefix):]), &body); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"access_token": "REDACTED_TOKEN", "refresh_token": "REDACTED_TOKEN", "id_token": "REDACTED_TOKEN",
		"api_key": "REDACTED_KEY", "account_id": "REDACTED_ACCOUNT", "organization_id": "REDACTED_ORG",
		"user_id": "REDACTED_USER", "user": nil, "email": "REDACTED_EMAIL",
		"id": "resp_keep", "key": "keep-key", "delta": "Keep this text.", "escaped": "REDACTED_KEY",
	} {
		if body[key] != want {
			t.Errorf("field %s = %v, want %v", key, body[key], want)
		}
	}
	if auth, ok := body["auth"].(map[string]any); !ok || auth["access"] != "REDACTED_TOKEN" || auth["refresh"] != "REDACTED_TOKEN" || auth["key"] != "REDACTED_KEY" {
		t.Fatal("auth aliases were not scrubbed")
	}
	if account, ok := body["account"].(map[string]any); !ok || account["id"] != "REDACTED_ACCOUNT" {
		t.Fatal("account object ID was not scrubbed")
	}
	if orgs, ok := body["organization"].([]any); !ok || len(orgs) != 1 || orgs[0].(map[string]any)["id"] != "REDACTED_ORG" {
		t.Fatal("organization list ID was not scrubbed")
	}
	if nested, ok := body["nested"].(string); !ok || !json.Valid([]byte(nested)) || !strings.Contains(nested, "REDACTED_TOKEN") || !strings.Contains(nested, "REDACTED_USER") {
		t.Fatal("nested JSON was not safely scrubbed")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("scrubbing changed private file permissions")
	}
	if out, err := exec.Command(bin, dir).CombinedOutput(); err != nil {
		t.Fatalf("repeat scrub: %v\n%s", err, out)
	}
	again, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(clean, again) {
		t.Fatal("scrub is not idempotent")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, dir).CombinedOutput(); err == nil {
		t.Fatalf("scrub followed symlink: %s", out)
	}
	unchanged, err = os.ReadFile(outside)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("scrub changed a file outside its directory")
	}
}

func TestScrubTruncatedSecrets(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "scrub")
	if out, err := exec.Command("go", "build", "-o", bin, "testdata/scrub.go").CombinedOutput(); err != nil {
		t.Fatalf("build scrub: %v\n%s", err, out)
	}
	prefix := "event: response.created\r\ndata: {\"delta\":\"Keep this text.\"}\r\n\r\ndata: "
	for _, tt := range []struct{ name, input, want string }{
		{"token", `{"access_token":"opaque-secret`, `{"access_token":"REDACTED_TOKEN`},
		{"refresh", `{"refresh_token":"opaque-secret\`, `{"refresh_token":"REDACTED_TOKEN`},
		{"account", `{"account_id": "opaque-secret`, `{"account_id": "REDACTED_ACCOUNT`},
		{"user", `{"user_id":"opaque-secret`, `{"user_id":"REDACTED_USER`},
		{"escaped-key", `{"organization\u005fid":"opaque-secret`, `{"organization\u005fid":"REDACTED_ORG`},
		{"nested-account", `{"account":{"id":"opaque-secret`, `{"account":{"id":"REDACTED_ACCOUNT`},
		{"nested-auth", `{"auth":{"access":"opaque-secret`, `{"auth":{"access":"REDACTED_TOKEN`},
		{"line-ending", "{\"access_token\":\"opaque-secret\r\n\r\n", "{\"access_token\":\"REDACTED_TOKEN\r\n\r\n"},
		{"next-event", "{\"access_token\":\"opaque-secret\n\ndata: {\"delta\":\"Keep this too.\"}\n\n", "{\"access_token\":\"REDACTED_TOKEN\n\ndata: {\"delta\":\"Keep this too.\"}\n\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cancel.sse")
			if err := os.WriteFile(path, []byte(prefix+tt.input), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(bin, "-check", dir).CombinedOutput(); err == nil {
				t.Fatal("check accepted a truncated secret")
			} else if bytes.Contains(out, []byte("opaque-secret")) {
				t.Fatal("check printed a secret")
			}
			if out, err := exec.Command(bin, dir).CombinedOutput(); err != nil {
				t.Fatalf("scrub: %v\n%s", err, out)
			}
			clean, err := os.ReadFile(path)
			if err != nil || string(clean) != prefix+tt.want {
				t.Fatal("scrub did not replace only the truncated secret")
			}
			if out, err := exec.Command(bin, "-check", dir).CombinedOutput(); err != nil {
				t.Fatalf("sanitized partial stream fails check: %v\n%s", err, out)
			}
		})
	}
}
