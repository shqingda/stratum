package session

import (
	"strings"
	"unicode"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/proto"
)

// ruleFor is the rule allowing a call like req: a command by its program
// and, for those that have them, its subcommand.
func ruleFor(req perm.Request) proto.Rule {
	r := proto.Rule{Tool: req.Tool, Unrestricted: req.Unrestricted}
	if req.Kind == perm.Exec {
		r.Prefix = prefix(req.Command)
	}
	return r
}

// prefix is a command line's program and, for programs made of
// subcommands, the subcommand: "go test", "npm run", "mkdir".
func prefix(cmd string) string {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return ""
	}
	if len(f) > 1 && subcommands[f[0]] && isWord(f[1]) {
		return f[0] + " " + f[1]
	}
	return f[0]
}

var subcommands = map[string]bool{"go": true, "git": true, "npm": true, "pnpm": true, "yarn": true, "bun": true, "cargo": true,
	"docker": true, "kubectl": true, "brew": true, "pip": true, "pip3": true, "uv": true, "poetry": true, "swift": true, "make": true, "gh": true}

func isWord(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// chained are what make a command line run more than its first command,
// which no rule covers.
const chained = ";&|`$<>\n()"

// allows reports whether the rule lets req run unasked.
func allows(r proto.Rule, req perm.Request) bool {
	if r.Tool != req.Tool || r.Unrestricted != req.Unrestricted {
		return false
	}
	if req.Kind != perm.Exec {
		return r.Prefix == ""
	}
	cmd := strings.TrimSpace(req.Command)
	if r.Prefix == "" || strings.ContainsAny(cmd, chained) {
		return false
	}
	return cmd == r.Prefix || strings.HasPrefix(cmd, r.Prefix+" ")
}
