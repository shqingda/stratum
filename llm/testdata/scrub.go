//go:build ignore

// Run with: go run llm/testdata/scrub.go [-check] [directory]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var jsonString = regexp.MustCompile(`"(?:\\.|[^"\\\r\n])*"`)
var assignment = regexp.MustCompile(`(?i)\b(access[ _-]?token|refresh[ _-]?token|id[ _-]?token|api[ _-]?key|account[ _-]?id|organization[ _-]?id|org[ _-]?id|user[ _-]?id)([ \t]*[:=][ \t]*)([A-Za-z0-9._~@/%+\-]+)`)

var patterns = []struct {
	re          *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+){0,2}`), "REDACTED_TOKEN"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]+`), "REDACTED_KEY"},
	{regexp.MustCompile(`(?i)\b(?:acct|account)[_-][A-Za-z0-9][A-Za-z0-9_-]+`), "REDACTED_ACCOUNT"},
	{regexp.MustCompile(`(?i)\borg[_-][A-Za-z0-9][A-Za-z0-9_-]+`), "REDACTED_ORG"},
	{regexp.MustCompile(`(?i)\buser[_-][A-Za-z0-9][A-Za-z0-9_-]+`), "REDACTED_USER"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "REDACTED_ACCOUNT"},
	{regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), "REDACTED_EMAIL"},
	{regexp.MustCompile(`(?i)\bBearer[ \t]+[A-Za-z0-9._~\-]+`), "Bearer REDACTED_TOKEN"},
}

func placeholder(key string) string {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
	switch key {
	case "accesstoken", "refreshtoken", "idtoken", "token", "authorization", "auth", "credential", "credentials":
		return "REDACTED_TOKEN"
	case "apikey":
		return "REDACTED_KEY"
	case "account", "accountid", "chatgptaccountid":
		return "REDACTED_ACCOUNT"
	case "organization", "organizationid", "org", "orgid", "openaiorganization":
		return "REDACTED_ORG"
	case "user", "userid", "chatgptuserid", "sub":
		return "REDACTED_USER"
	case "email", "emailaddress":
		return "REDACTED_EMAIL"
	}
	return ""
}

func scrub(src []byte) []byte {
	var object map[string]json.RawMessage
	if json.Unmarshal(src, &object) == nil && (string(object["kind"]) == `"oauth"` || string(object["kind"]) == `"api_key"`) {
		return scrubIdentity(src, "REDACTED_TOKEN")
	}
	return scrubIdentity(src, "")
}

func scrubIdentity(src []byte, identity string) []byte {
	var out bytes.Buffer
	for len(src) > 0 {
		loc := jsonString.FindIndex(src)
		if loc == nil {
			out.WriteString(scrubText(string(src)))
			break
		}
		out.WriteString(scrubText(string(src[:loc[0]])))
		quoted := src[loc[0]:loc[1]]
		rest := src[loc[1]:]
		var value string
		if json.Unmarshal(quoted, &value) != nil {
			out.Write(quoted)
			src = rest
			continue
		}
		trim := bytes.TrimLeft(rest, " \t\r\n")
		isKey := len(trim) > 0 && trim[0] == ':'
		if !isKey {
			// Decode escaped strings too, including JSON nested inside an error message.
			clean := scrubText(value)
			if strings.Contains(value, `"`) {
				clean = string(scrub([]byte(value)))
			}
			if clean != value {
				quoted, _ = json.Marshal(clean)
			}
		}
		out.Write(quoted)
		replacement := placeholder(value)
		if identity == "REDACTED_TOKEN" {
			switch strings.ToLower(value) {
			case "access", "refresh":
				replacement = "REDACTED_TOKEN"
			case "key":
				replacement = "REDACTED_KEY"
			}
		}
		if identity != "" && strings.EqualFold(value, "id") {
			replacement = identity
		}
		if replacement != "" && isKey {
			v := bytes.TrimLeft(trim[1:], " \t\r\n")
			end := 0
			closingQuote := `"`
			if len(v) > 0 && (v[0] == '{' || v[0] == '[') {
				var nested json.RawMessage
				err := json.NewDecoder(bytes.NewReader(v)).Decode(&nested)
				if err == io.ErrUnexpectedEOF {
					nested = v
				}
				if err == nil || err == io.ErrUnexpectedEOF {
					out.Write(rest[:len(rest)-len(v)])
					out.Write(scrubIdentity(nested, replacement))
					src = v[len(nested):]
					continue
				}
			} else if len(v) > 0 && v[0] == '"' {
				if match := jsonString.FindIndex(v); match != nil && match[0] == 0 {
					end = match[1]
					var old string
					if json.Unmarshal(v[:end], &old) != nil || old == "" || old == replacement {
						end = 0
					}
				} else {
					// Cancellation can end inside a secret; keep the stream truncated.
					end = bytes.IndexAny(v, "\r\n")
					if end < 0 {
						end = len(v)
					}
					if end == 1 || string(v[1:end]) == replacement {
						end = 0
					}
					closingQuote = ""
				}
			} else if len(v) > 0 && (v[0] >= '0' && v[0] <= '9' || v[0] == '-') {
				end = bytes.IndexAny(v, ",}\r\n \t")
				if end < 0 {
					end = len(v)
				}
			}
			if end > 0 {
				out.Write(rest[:len(rest)-len(v)])
				out.WriteString(`"` + replacement + closingQuote)
				src = v[end:]
				continue
			}
		}
		src = rest
	}
	return out.Bytes()
}

func scrubText(s string) string {
	s = assignment.ReplaceAllStringFunc(s, func(match string) string {
		parts := assignment.FindStringSubmatch(match)
		return parts[1] + parts[2] + placeholder(parts[1])
	})
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	return s
}

func replaceFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".scrub-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode.Perm()); err == nil {
		_, err = f.Write(data)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func run(dir string, check bool) error {
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		clean := scrub(data)
		if bytes.Equal(data, clean) {
			return nil
		}
		if check {
			found = true
			fmt.Fprintln(os.Stderr, "secret pattern in", path)
			return nil
		}
		return replaceFile(path, clean, info.Mode())
	})
	if err == nil && found {
		return errors.New("fixtures contain secret patterns; run scrub.go without -check")
	}
	return err
}

func main() {
	check := flag.Bool("check", false, "check without changing files")
	flag.Parse()
	dir := "llm/testdata"
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: scrub.go [-check] [directory]")
		os.Exit(2)
	}
	if flag.NArg() == 1 {
		dir = flag.Arg(0)
	}
	if err := run(dir, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
