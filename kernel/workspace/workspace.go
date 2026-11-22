// Package workspace resolves paths the model names against the directory a
// session works in, following symbolic links so that a link cannot lead a
// path outside without the resolver seeing it.
package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Path is a resolved path.
type Path struct {
	Abs    string // absolute, with every existing link resolved
	Inside bool   // within the workspace root
	Rel    string // relative to the root, when Inside
}

// Resolve resolves input, absolute or relative to root. Links in the part
// of the path that exists are followed; the rest is joined as written.
func Resolve(root, input string) (Path, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Path{}, err
	}
	realRoot, _ = filepath.Abs(realRoot)
	p := input
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(realRoot, p)
	}
	p = filepath.Clean(p)

	// Walk up to the deepest existing ancestor, resolve it, then add back
	// the parts that do not exist yet.
	var tail []string
	cur := p
	links := 0
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return located(realRoot, real), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return Path{}, err
		}
		// A dangling link still names a target; do not mistake it for a missing file.
		info, statErr := os.Lstat(cur)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return Path{}, statErr
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 255 {
				return Path{}, errors.New("workspace: too many symbolic links")
			}
			target, err := os.Readlink(cur)
			if err != nil {
				return Path{}, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			cur = target
			continue
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return Path{}, err
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func located(root, abs string) Path {
	rel, err := filepath.Rel(root, abs)
	inside := err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
	if !inside {
		rel = ""
	}
	return Path{Abs: abs, Inside: inside, Rel: rel}
}

var sensitive = regexp.MustCompile(`(?i)(?:^|/)(?:\.git|\.ssh|\.aws|\.gnupg|\.stratum|\.talos|\.env(?:\.[^/]*)?|\.npmrc|\.netrc|[^/]*(?:secret|credential|token|password)[^/]*|[^/]*\.(?:pem|key|p12))(?:/|$)`)

// Sensitive tells whether a path likely holds secrets or version control
// state, which no mode short of full access touches without asking.
func Sensitive(path string) bool {
	return sensitive.MatchString(filepath.ToSlash(path))
}
