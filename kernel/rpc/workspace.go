package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	ws "github.com/shqingda/stratum/kernel/workspace"
	"github.com/shqingda/stratum/proto"
)

var ghRun = defaultGH

func defaultGH(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	if os.Getenv("STRATUM_DEMO") != "" {
		return nil, errors.New("gh is disabled in demo mode")
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = cwd
	return cmd.CombinedOutput()
}

func git(ctx context.Context, cwd string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitOut is git's output as it is, for records whose leading spaces count.
func gitOut(ctx context.Context, cwd string, args ...string) string {
	out, _ := exec.CommandContext(ctx, "git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...).Output()
	return string(out)
}

// workspace says which branch a folder is on and what is not committed;
// a folder outside git, or git missing, is a workspace without a branch.
func workspace(ctx context.Context, cwd string) (proto.Workspace, error) {
	if !filepath.IsAbs(cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var w proto.Workspace
	w.Recent = recentFiles(ctx, cwd, overviewLen)
	if b, err := git(ctx, cwd, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		w.Branch = b
	} else if b, err := git(ctx, cwd, "rev-parse", "--short", "HEAD"); err == nil {
		w.Branch = b
	} else {
		return w, nil
	}
	top, err := repoTop(ctx, cwd)
	if err != nil {
		return w, nil
	}
	counts := numstat(ctx, cwd, "HEAD")
	var untracked []string
	status := gitOut(ctx, cwd, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	for _, rec := range strings.Split(status, "\x00") {
		if len(rec) < 4 {
			continue
		}
		rel := rec[3:]
		f := proto.ChangeFile{Path: filepath.Join(top, rel)}
		if rec[:2] == "??" {
			f.Added = lineCount(f.Path)
			untracked = append(untracked, rel)
		} else {
			f.Added, f.Removed = counts[rel][0], counts[rel][1]
		}
		w.Files = append(w.Files, f)
		w.Added += f.Added
		w.Removed += f.Removed
	}
	w.Dirty = len(w.Files) > 0
	sort.Slice(w.Files, func(i, j int) bool { return w.Files[i].Path < w.Files[j].Path })
	if commit, name := branchBase(ctx, cwd); commit != "" {
		w.Base = name
		for rel, n := range numstat(ctx, cwd, commit) {
			w.BaseFiles = append(w.BaseFiles, proto.ChangeFile{Path: filepath.Join(top, rel), Added: n[0], Removed: n[1]})
		}
		for _, rel := range untracked {
			p := filepath.Join(top, rel)
			w.BaseFiles = append(w.BaseFiles, proto.ChangeFile{Path: p, Added: lineCount(p)})
		}
		sort.Slice(w.BaseFiles, func(i, j int) bool { return w.BaseFiles[i].Path < w.BaseFiles[j].Path })
	}
	w.Branches = localBranches(ctx, cwd)
	w.Ahead = aheadCount(ctx, cwd, w.Branch, w.Base)
	w.Commits = recentCommits(ctx, cwd, overviewLen)
	w.PR = readPR(ctx, cwd, w.Branch, w.Base)
	return w, nil
}

// overviewLen is how many commits and files an empty session lists.
const overviewLen = 2

// recentCommits are HEAD's latest n commits, newest first; none on a
// branch not yet born.
func recentCommits(ctx context.Context, cwd string, n int) []proto.CommitSummary {
	out := gitOut(ctx, cwd, "log", "-n", strconv.Itoa(n), "--format=%h%x00%ct%x00%s%x00", "HEAD")
	f := strings.Split(out, "\x00")
	var commits []proto.CommitSummary
	for i := 0; i+2 < len(f); i += 3 {
		hash := strings.TrimSpace(f[i])
		if hash == "" {
			continue
		}
		t, _ := strconv.ParseInt(f[i+1], 10, 64)
		commits = append(commits, proto.CommitSummary{Hash: hash, Time: t, Subject: f[i+2]})
	}
	return commits
}

// recentFiles are the n files of the folder last changed on disk, newest
// first: of those git tracks or would track in a repository, else of
// those outside folders of dependencies and builds. Secrets are left out.
func recentFiles(ctx context.Context, cwd string, n int) []proto.RecentFile {
	var recent []proto.RecentFile
	for _, rel := range ws.Files(ctx, cwd) {
		if ws.Sensitive(rel) {
			continue
		}
		abs := filepath.Join(cwd, rel)
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		recent = append(recent, proto.RecentFile{Path: abs, Time: info.ModTime().Unix()})
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].Time > recent[j].Time })
	return recent[:min(len(recent), n)]
}

func localBranches(ctx context.Context, cwd string) []string {
	out := gitOut(ctx, cwd, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		if br := strings.TrimSpace(line); br != "" {
			branches = append(branches, br)
		}
	}
	return branches
}

func aheadCount(ctx context.Context, cwd, branch, base string) int {
	if cnt, err := git(ctx, cwd, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		if n, err := strconv.Atoi(cnt); err == nil {
			return n
		}
	}
	if commit, name := branchBase(ctx, cwd); commit != "" && branch != "" && branch != name && branch != base {
		if cnt, err := git(ctx, cwd, "rev-list", "--count", commit+"..HEAD"); err == nil {
			if n, err := strconv.Atoi(cnt); err == nil {
				return n
			}
		}
	}
	return 0
}

func readPR(ctx context.Context, cwd, branch, base string) *proto.PullRequest {
	if ghRun == nil || branch == "" || branch == base {
		return nil
	}
	remote, err := git(ctx, cwd, "remote", "get-url", "origin")
	if err != nil || remote == "" {
		return nil
	}
	ghCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := ghRun(ghCtx, cwd, "pr", "view", "--json", "number,title,url,state,statusCheckRollup")
	if err != nil {
		return nil
	}
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		State  string `json:"state"`
		Checks []struct {
			State      string `json:"state"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &raw) != nil || raw.Number == 0 || raw.URL == "" {
		return nil
	}
	var hasFail, hasPending, hasPass bool
	for _, ch := range raw.Checks {
		c := strings.ToUpper(ch.Conclusion)
		st := strings.ToUpper(ch.Status)
		s := strings.ToUpper(ch.State)
		switch {
		case c == "FAILURE" || c == "ERROR" || c == "TIMED_OUT" || c == "CANCELLED" || c == "STARTUP_FAILURE" || c == "ACTION_REQUIRED" ||
			s == "FAILURE" || s == "ERROR":
			hasFail = true
		case st == "PENDING" || st == "IN_PROGRESS" || st == "QUEUED" || st == "WAITING" || st == "REQUESTED" ||
			s == "PENDING" || s == "EXPECTED":
			hasPending = true
		case c == "SUCCESS" || c == "NEUTRAL" || c == "SKIPPED" || s == "SUCCESS":
			hasPass = true
		}
	}
	checks := ""
	switch {
	case hasFail:
		checks = "fail"
	case hasPending:
		checks = "pending"
	case hasPass:
		checks = "pass"
	}
	return &proto.PullRequest{
		Number: raw.Number,
		Title:  raw.Title,
		URL:    raw.URL,
		State:  strings.ToLower(raw.State),
		Checks: checks,
	}
}

// numstat counts the lines each tracked file gained and lost since a
// commit, on disk, by its path in the repository.
func numstat(ctx context.Context, cwd, since string) map[string][2]int {
	counts := map[string][2]int{}
	stat := gitOut(ctx, cwd, "diff", since, "--numstat", "--no-renames", "-z")
	for _, rec := range strings.Split(stat, "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) < 3 {
			continue
		}
		add, _ := strconv.Atoi(f[0]) // "-" for a binary file
		del, _ := strconv.Atoi(f[1])
		counts[f[2]] = [2]int{add, del}
	}
	return counts
}

func lineCount(path string) int {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte("\n"))
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// branchBase is where the branch left the repository's main line, and
// that line's name: the remote's default branch, else main or master.
func branchBase(ctx context.Context, cwd string) (commit, name string) {
	refs := []string{"origin/main", "origin/master", "main", "master"}
	if def, err := git(ctx, cwd, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && def != "" {
		refs = append([]string{def}, refs...)
	}
	for _, ref := range refs {
		if _, err := git(ctx, cwd, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
			continue
		}
		if mb, err := git(ctx, cwd, "merge-base", "HEAD", ref); err == nil && mb != "" {
			return mb, strings.TrimPrefix(ref, "origin/")
		}
	}
	return "", ""
}

// repoTop is the repository's top folder spelled as cwd is, so that paths
// under it match the session's, symbolic links and all.
func repoTop(ctx context.Context, cwd string) (string, error) {
	prefix, err := git(ctx, cwd, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	top := filepath.Clean(cwd)
	if prefix != "" {
		for range strings.Split(strings.TrimSuffix(prefix, "/"), "/") {
			top = filepath.Dir(top)
		}
	}
	return top, nil
}

// workspaceDiff reads a file of the folder's repository as HEAD has it and
// as it is on disk; nil for a side that has no such file.
func workspaceDiff(ctx context.Context, q proto.WorkspaceQuery) (proto.ChangeDiff, error) {
	if !filepath.IsAbs(q.Cwd) || !filepath.IsAbs(q.Path) {
		return proto.ChangeDiff{}, proto.Errorf("bad_request", "cwd and path must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	top, err := repoTop(ctx, q.Cwd)
	if err != nil {
		return proto.ChangeDiff{}, proto.Errorf("not_found", "not in a git repository")
	}
	rel, err := filepath.Rel(top, filepath.Clean(q.Path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return proto.ChangeDiff{}, proto.Errorf("bad_request", "%s is outside the repository", q.Path)
	}
	d := proto.ChangeDiff{Path: q.Path}
	from := "HEAD"
	if q.Base {
		if from, _ = branchBase(ctx, q.Cwd); from == "" {
			return proto.ChangeDiff{}, proto.Errorf("not_found", "the branch has no base")
		}
	}
	if b, err := exec.CommandContext(ctx, "git", "-C", top, "--no-optional-locks", "show", from+":./"+filepath.ToSlash(rel)).Output(); err == nil {
		s := string(b)
		d.Before = &s
	}
	if b, err := os.ReadFile(q.Path); err == nil {
		s := string(b)
		d.After = &s
	}
	return d, nil
}

// commit stages everything in the folder and commits it.
func commit(ctx context.Context, p proto.Commit) (proto.Workspace, error) {
	if !filepath.IsAbs(p.Cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	if strings.TrimSpace(p.Message) == "" {
		return proto.Workspace{}, proto.Errorf("bad_request", "empty commit message")
	}
	run, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if out, err := git(run, p.Cwd, "add", "-A"); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git add: %s", out)
	}
	if out, err := git(run, p.Cwd, "commit", "-q", "-m", p.Message); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git commit: %s", out)
	}
	return workspace(ctx, p.Cwd)
}

func push(ctx context.Context, q proto.WorkspaceQuery) (proto.Workspace, error) {
	if !filepath.IsAbs(q.Cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	run, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if out, err := git(run, q.Cwd, "push", "-u", "origin", "HEAD"); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git push: %s", out)
	}
	return workspace(ctx, q.Cwd)
}

func createPR(ctx context.Context, p proto.CreatePR) (proto.Workspace, error) {
	if !filepath.IsAbs(p.Cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	if strings.TrimSpace(p.Title) == "" {
		return proto.Workspace{}, proto.Errorf("bad_request", "PR title is required")
	}
	run, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if out, err := git(run, p.Cwd, "push", "-u", "origin", "HEAD"); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git push: %s", out)
	}
	if ghRun == nil {
		return proto.Workspace{}, proto.Errorf("internal", "gh is not available")
	}
	if out, err := ghRun(run, p.Cwd, "pr", "create", "--title", p.Title, "--body", p.Body); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "gh pr create: %s", strings.TrimSpace(string(out)))
	}
	return workspace(ctx, p.Cwd)
}

func checkout(ctx context.Context, p proto.Checkout) (proto.Workspace, error) {
	if !filepath.IsAbs(p.Cwd) {
		return proto.Workspace{}, proto.Errorf("bad_request", "cwd must be absolute")
	}
	br := strings.TrimSpace(p.Branch)
	if br == "" {
		return proto.Workspace{}, proto.Errorf("bad_request", "branch is required")
	}
	run, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if out, err := git(run, p.Cwd, "checkout", br); err != nil {
		return proto.Workspace{}, proto.Errorf("internal", "git checkout: %s", out)
	}
	return workspace(ctx, p.Cwd)
}
