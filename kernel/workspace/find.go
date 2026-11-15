package workspace

import (
	"context"
	"io/fs"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// maxFiles bounds the files Files lists.
const maxFiles = 20000

// Files lists the files of the workspace at root, relative to it: those
// git tracks or would track when it is a repository, else those under it
// outside folders of dependencies and builds.
func Files(ctx context.Context, root string) []string {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-co", "--exclude-standard")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		files := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(files) == 1 && files[0] == "" {
			return nil
		}
		return files[:min(len(files), maxFiles)]
	}
	var files []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxFiles {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || skipped[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(root, path); err == nil {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

var skipped = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, "__pycache__": true}

// Match picks the files query names, best first, at most limit: the
// query's letters must come in order, and they score where they start a
// word of the path, run on together, and fall in the file's name.
func Match(files []string, query string, limit int) []string {
	type hit struct {
		path  string
		score int
	}
	q := []rune(strings.ToLower(query))
	var hits []hit
	for _, f := range files {
		if s, ok := score(f, q); ok {
			hits = append(hits, hit{f, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return len(a.path) - len(b.path)
	})
	var out []string
	for _, h := range hits[:min(len(hits), limit)] {
		out = append(out, h.path)
	}
	return out
}

func score(path string, q []rune) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	p := []rune(strings.ToLower(path))
	orig := []rune(path)
	base := len(p) - len([]rune(filepath.Base(path)))
	s, qi, prev := 0, 0, -2
	for i, r := range p {
		if qi == len(q) {
			break
		}
		if r != q[qi] {
			continue
		}
		pts := 1
		if i == prev+1 {
			pts += 3 // runs on
		}
		if i == 0 || strings.ContainsRune("/._- ", p[i-1]) || unicode.IsUpper(orig[i]) && !unicode.IsUpper(orig[i-1]) {
			pts += 4 // starts a word
		}
		if i >= base {
			pts += 2 // in the file's name
		}
		s += pts
		prev = i
		qi++
	}
	return s, qi == len(q)
}
