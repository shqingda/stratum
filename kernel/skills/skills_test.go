package skills

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

func tempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func skillText(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
}

func TestInstructionHierarchy(t *testing.T) {
	for _, marker := range []string{"directory", "worktree-file", "none"} {
		t.Run(marker, func(t *testing.T) {
			base := tempDir(t)
			global := filepath.Join(base, "global")
			repo := filepath.Join(base, "repo")
			middle := filepath.Join(repo, "middle")
			cwd := filepath.Join(middle, "workspace")
			for path, body := range map[string]string{
				filepath.Join(global, "AGENTS.md"): "global instructions",
				filepath.Join(base, "AGENTS.md"):   "above-repository instructions",
				filepath.Join(repo, "AGENTS.md"):   "repository instructions",
				filepath.Join(middle, "AGENTS.md"): "middle instructions",
				filepath.Join(cwd, "AGENTS.md"):    "workspace instructions",
			} {
				write(t, path, body)
			}
			if marker == "directory" {
				if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else if marker == "worktree-file" {
				write(t, filepath.Join(repo, ".git"), "gitdir: ../main/.git/worktrees/example\n")
			}
			catalog := (&Loader{GlobalDir: global}).Load(cwd)
			want := []string{"global instructions", "repository instructions", "middle instructions", "workspace instructions"}
			paths := []string{global, repo, middle, cwd}
			if marker == "none" {
				want = []string{"global instructions", "workspace instructions"}
				paths = []string{global, cwd}
				if strings.Contains(catalog.Prompt, "repository instructions") || strings.Contains(catalog.Prompt, "middle instructions") {
					t.Fatal("non-git ancestor instructions were included")
				}
			}
			at := -1
			for i, text := range want {
				next := strings.Index(catalog.Prompt, "\n"+text+"\n")
				if next <= at || !strings.Contains(catalog.Prompt, filepath.Join(paths[i], "AGENTS.md")) {
					t.Fatalf("wrong source/order: %s", catalog.Prompt)
				}
				at = next
			}
			if strings.Contains(catalog.Prompt, "above-repository instructions") || !strings.Contains(catalog.Prompt, "take precedence") {
				t.Fatalf("wrong scope/priority: %s", catalog.Prompt)
			}
		})
	}
}

func TestSymlinkWorkspaceFindsRealGitRoot(t *testing.T) {
	base := tempDir(t)
	repo := filepath.Join(base, "repo")
	cwd := filepath.Join(repo, "sub")
	write(t, filepath.Join(repo, "AGENTS.md"), "root-through-alias")
	write(t, filepath.Join(cwd, "AGENTS.md"), "cwd-through-alias")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(cwd, alias); err != nil {
		t.Fatal(err)
	}
	l := &Loader{GlobalDir: filepath.Join(base, "global")}
	catalog := l.Load(alias)
	if !strings.Contains(catalog.Prompt, "root-through-alias") || !strings.Contains(catalog.Prompt, filepath.Join(cwd, "AGENTS.md")) {
		t.Fatalf("alias lost hierarchy: %s", catalog.Prompt)
	}
	if catalog != l.Load(cwd) {
		t.Fatal("alias and real workspace did not share identical snapshot")
	}
}

func TestInstructionsTruncateUTF8AndKeepSkillList(t *testing.T) {
	base := tempDir(t)
	cwd := filepath.Join(base, "workspace")
	write(t, filepath.Join(cwd, "AGENTS.md"), strings.Repeat("中文说明", 10000))
	write(t, filepath.Join(cwd, ".talos", "skills", "one", "SKILL.md"), skillText("one", "One line description.", "BODY_MUST_NOT_APPEAR"))
	catalog := (&Loader{GlobalDir: filepath.Join(base, "global")}).Load(cwd)
	at := strings.Index(catalog.Prompt, "\nAvailable skills")
	if at < 0 || at > agentsLimit || !utf8.ValidString(catalog.Prompt) || !strings.Contains(catalog.Prompt[:at], "truncated at 32 KiB") {
		t.Fatalf("unsafe truncation or missing list: agent bytes=%d total=%d", at, len(catalog.Prompt))
	}
	if !strings.Contains(catalog.Prompt, "- one: One line description.") || strings.Contains(catalog.Prompt, "BODY_MUST_NOT_APPEAR") {
		t.Fatal("prompt must list metadata only after truncated instructions")
	}
	if len(catalog.Prompt[:at]) != agentsLimit && len(catalog.Prompt[:at]) < agentsLimit-3 {
		t.Fatalf("unexpected truncation length: %d", at)
	}
}

func TestSkillFrontMatter(t *testing.T) {
	for _, tc := range []struct{ name, source, wantName, wantDescription, wantBody string }{
		{"plain", skillText("review", "Review code.", "# Instructions\nDo things.\n"), "review", "Review code.", "# Instructions\nDo things.\n"},
		{"quotes", "---\nname: 'review'\ndescription: 'It''s a review: be brief.'\nlicense: MIT\n---\nbody", "review", "It's a review: be brief.", "body"},
		{"escaped", "---\nname: \"review\"\ndescription: \"Review\\ncode briefly.\"\n---\nbody", "review", "Review code briefly.", "body"},
		{"crlf", "---\r\nname: review\r\ndescription: Review code.\r\n---\r\nbody\r\n", "review", "Review code.", "body\r\n"},
		{"comments", "---\n# note\nname: review # inline\ndescription: Review code. # inline\n---\n", "review", "Review code.", ""},
		{"empty-body", "---\nname: review\ndescription: Review code.\n---", "review", "Review code.", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skill, err := parseSkill([]byte(tc.source))
			if err != nil || skill.Name != tc.wantName || skill.Description != tc.wantDescription || skill.Body != tc.wantBody {
				t.Fatalf("parse = %+v, %v", skill, err)
			}
		})
	}
	for name, source := range map[string]string{
		"missing-opening":     "name: review\ndescription: desc\n---\nbody",
		"missing-name":        "---\ndescription: desc\n---\nbody",
		"missing-description": "---\nname: review\n---\nbody",
		"empty-description":   "---\nname: review\ndescription: ''\n---\nbody",
		"missing-closing":     "---\nname: review\ndescription: desc\nbody",
		"broken-field":        "---\nname review\ndescription: desc\n---\nbody",
		"duplicate":           "---\nname: first\nname: second\ndescription: desc\n---\nbody",
		"multiline":           "---\nname: review\ndescription: |\n  text\n---\nbody",
		"collection":          "---\nname: review\ndescription: [one, two]\n---\nbody",
		"bad-quote":           "---\nname: review\ndescription: 'not closed\n---\nbody",
		"comment-only":        "---\nname: review\ndescription: # no value\n---\nbody",
		"bad-byte-escape":     "---\nname: review\ndescription: \"\\xE9\"\n---\nbody",
		"bad-octal-escape":    "---\nname: review\ndescription: \"\\123\"\n---\nbody",
	} {
		t.Run(name, func(t *testing.T) {
			if skill, err := parseSkill([]byte(source)); err == nil {
				t.Fatalf("accepted malformed skill: %+v", skill)
			}
		})
	}
}

func TestSkillsOverrideAndSnapshots(t *testing.T) {
	base := tempDir(t)
	global, cwd := filepath.Join(base, "global"), filepath.Join(base, "workspace")
	write(t, filepath.Join(global, "skills", "directory-name", "SKILL.md"), skillText("shared", "Global summary.", "GLOBAL_BODY"))
	write(t, filepath.Join(global, "skills", "z", "SKILL.md"), skillText("z-last", "Last summary.", "LAST_BODY"))
	local := filepath.Join(cwd, ".talos", "skills", "other-directory", "SKILL.md")
	write(t, local, skillText("shared", "Local summary.", "LOCAL_BODY"))
	var logged bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(oldLog)
	write(t, filepath.Join(cwd, ".talos", "skills", "bad", "SKILL.md"), "---\nname: invalid\n---\nINVALID_BODY")
	l := &Loader{GlobalDir: global}
	first := l.Load(cwd)
	if got := []string{first.Skills[0].Name, first.Skills[1].Name}; !reflect.DeepEqual(got, []string{"shared", "z-last"}) {
		t.Fatalf("skill ordering/override: %+v", first.Skills)
	}
	shared, ok := first.Find("shared")
	if !ok || shared.Body != "LOCAL_BODY" || shared.Dir != filepath.Dir(local) || shared.Description != "Local summary." {
		t.Fatalf("workspace did not override: %+v", shared)
	}
	for _, text := range []string{"GLOBAL_BODY", "LOCAL_BODY", "LAST_BODY", "INVALID_BODY", "Global summary."} {
		if strings.Contains(first.Prompt, text) {
			t.Errorf("prompt leaked body/overridden skill: %q", text)
		}
	}
	if _, ok := first.Find("../shared"); ok {
		t.Fatal("Find treated name as a path")
	}
	if _, ok := first.Find("Shared"); ok {
		t.Fatal("Find was not exact")
	}
	if !strings.Contains(logged.String(), "bad/SKILL.md") {
		t.Fatal("malformed skill was not logged")
	}
	if l.Load(cwd) != first {
		t.Fatal("unchanged files did not reuse snapshot")
	}
	write(t, local, skillText("shared", "Local summary.", "UPDATED_BODY"))
	second := l.Load(cwd)
	if second == first || second.Prompt != first.Prompt {
		t.Fatal("body-only edit did not refresh snapshot with unchanged metadata prompt")
	}
	if old, _ := first.Find("shared"); old.Body != "LOCAL_BODY" {
		t.Fatal("previous turn snapshot was mutated")
	}
	if changed, _ := second.Find("shared"); changed.Body != "UPDATED_BODY" {
		t.Fatal("new snapshot missed changed body")
	}
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	third := l.Load(cwd)
	if fallback, _ := third.Find("shared"); fallback.Body != "GLOBAL_BODY" {
		t.Fatal("deleting workspace skill did not restore global entry")
	}
}

func TestCacheRefreshesContentAndMembership(t *testing.T) {
	base := tempDir(t)
	repo, cwd := filepath.Join(base, "repo"), filepath.Join(base, "repo", "child")
	path := filepath.Join(cwd, "AGENTS.md")
	write(t, path, "version one")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	l := &Loader{GlobalDir: filepath.Join(base, "global")}
	first := l.Load(cwd)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, "version two")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := l.Load(cwd)
	if second == first || !strings.Contains(second.Prompt, "version two") || strings.Contains(first.Prompt, "version two") {
		t.Fatal("same-mtime content edit was missed or old snapshot changed")
	}
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l.Load(cwd) != second {
		t.Fatal("mtime-only edit unnecessarily regenerated snapshot")
	}
	rootAgents := filepath.Join(repo, "AGENTS.md")
	write(t, rootAgents, "new root instructions")
	third := l.Load(cwd)
	if third == second || !strings.Contains(third.Prompt, "new root instructions") {
		t.Fatal("new AGENTS file was missed")
	}
	if err := os.Remove(rootAgents); err != nil {
		t.Fatal(err)
	}
	fourth := l.Load(cwd)
	if fourth == third || strings.Contains(fourth.Prompt, "new root instructions") {
		t.Fatal("deleted AGENTS file remained cached")
	}
	newSkill := filepath.Join(cwd, ".talos", "skills", "new", "SKILL.md")
	write(t, newSkill, skillText("new", "New summary.", "body"))
	fifth := l.Load(cwd)
	if _, ok := fifth.Find("new"); !ok || fifth == fourth {
		t.Fatal("new skill missed")
	}
	if err := os.Remove(newSkill); err != nil {
		t.Fatal(err)
	}
	if last := l.Load(cwd); last == fifth || len(last.Skills) != 0 {
		t.Fatal("deleted skill retained")
	}
}

func TestDefaultGlobalAndNoExecution(t *testing.T) {
	home, cwd := tempDir(t), tempDir(t)
	t.Setenv("HOME", home)
	marker := filepath.Join(home, "must-not-exist")
	command := "$(touch " + marker + ")"
	write(t, filepath.Join(home, ".talos", "AGENTS.md"), command)
	write(t, filepath.Join(home, ".talos", "skills", "safe", "SKILL.md"), skillText("safe", "Text only.", command))
	var l Loader
	catalog := l.Load(cwd)
	if !strings.Contains(catalog.Prompt, command) {
		t.Fatal("zero-value loader did not read global instructions")
	}
	if skill, ok := catalog.Find("safe"); !ok || skill.Body != command {
		t.Fatal("global skill body unavailable")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("instruction contents were executed: %v", err)
	}
}

func TestSymlinkInstructionsAreIgnored(t *testing.T) {
	base := tempDir(t)
	cwd, global := filepath.Join(base, "workspace"), filepath.Join(base, "global")
	outside := filepath.Join(base, "outside", "SKILL.md")
	write(t, outside, skillText("secret", "Must not load.", "PRIVATE_BODY"))
	if err := os.MkdirAll(filepath.Join(cwd, ".talos", "skills", "file-link"), 0o700); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(cwd, "AGENTS.md"):                                 outside,
		filepath.Join(cwd, ".talos", "skills", "file-link", "SKILL.md"): outside,
		filepath.Join(cwd, ".talos", "skills", "directory-link"):        filepath.Dir(outside),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	catalog := (&Loader{GlobalDir: global}).Load(cwd)
	if catalog.Prompt != "" || len(catalog.Skills) != 0 {
		t.Fatalf("linked instruction escaped read boundary: %+v", catalog)
	}
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(global, "skills")); err != nil {
		t.Fatal(err)
	}
	if catalog := (&Loader{GlobalDir: global}).Load(cwd); catalog.Prompt != "" || len(catalog.Skills) != 0 {
		t.Fatal("linked skills root was followed")
	}
}

func TestWorkspaceTalosSymlinkIsIgnored(t *testing.T) {
	base := tempDir(t)
	cwd, outside := filepath.Join(base, "workspace"), filepath.Join(base, "outside")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(outside, "skills", "linked", "SKILL.md"), skillText("linked", "External summary.", "PRIVATE_BODY"))
	if err := os.Symlink(outside, filepath.Join(cwd, ".talos")); err != nil {
		t.Fatal(err)
	}
	catalog := (&Loader{GlobalDir: filepath.Join(base, "global")}).Load(cwd)
	if len(catalog.Skills) != 0 || catalog.Prompt != "" {
		t.Fatal("workspace .talos symlink loaded external instructions")
	}
}

func TestPinnedSkillDirectoryRejectsReplacement(t *testing.T) {
	base := tempDir(t)
	dir, outside := filepath.Join(base, "skill"), filepath.Join(base, "outside")
	write(t, filepath.Join(dir, "SKILL.md"), "ORIGINAL_BODY")
	write(t, filepath.Join(outside, "SKILL.md"), "PRIVATE_BODY")
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	pinned, err := openChild(root, "skill")
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	if err := os.Rename(dir, dir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	f, err := pinned.OpenFile("SKILL.md", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	data, ok := readOpened(filepath.Join(dir, "SKILL.md"), f, err)
	if !ok || string(data) != "ORIGINAL_BODY" {
		t.Fatalf("pinned directory followed replacement: %q", data)
	}
	if replaced, err := openChild(root, "skill"); err == nil {
		replaced.Close()
		t.Fatal("new load accepted directory symlink")
	}
}
