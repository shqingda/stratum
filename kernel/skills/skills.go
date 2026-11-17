// Package skills reads project instructions and installed skills as text only.
package skills

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
)

const agentsLimit = 32 << 10

type Skill struct {
	Name, Description, Dir, Body string
	Project                      bool
}

// Catalog is a turn's read-only snapshot; callers must not modify its fields.
type Catalog struct {
	Prompt       string
	Skills       []Skill
	agentsPrompt string
}

func (c *Catalog) Find(name string) (Skill, bool) {
	for _, skill := range c.Skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

type Loader struct {
	GlobalDir string // ~/.talos when empty
	Disabled  func(name string) bool
	mu        sync.Mutex
	cache     map[string]cached
}

// GlobalRoot returns the resolved global Talos directory (~/.talos by default).
func (l *Loader) GlobalRoot() string {
	global := l.GlobalDir
	if global == "" {
		if home, err := os.UserHomeDir(); err == nil {
			global = filepath.Join(home, ".talos")
		}
	}
	if global != "" {
		if abs, err := filepath.Abs(global); err == nil {
			global = abs
		}
	}
	return global
}

type cached struct {
	hash    [32]byte
	catalog *Catalog
}

type source struct {
	path    string
	data    []byte
	agents  bool
	memory  bool
	project bool
}

// Load returns the enabled skills and instructions for cwd.
func (l *Loader) Load(cwd string) *Catalog {
	full := l.LoadAll(cwd)
	if l.Disabled == nil || len(full.Skills) == 0 {
		return full
	}
	out := &Catalog{Prompt: full.agentsPrompt, Skills: []Skill{}, agentsPrompt: full.agentsPrompt}
	for _, sk := range full.Skills {
		if !l.Disabled(sk.Name) {
			out.Skills = append(out.Skills, sk)
		}
	}
	if len(out.Skills) > 0 {
		var summary strings.Builder
		summary.WriteString("\nAvailable skills (use the skill tool to read one):\n")
		for _, skill := range out.Skills {
			fmt.Fprintf(&summary, "- %s: %s\n", skill.Name, skill.Description)
		}
		out.Prompt += summary.String()
	}
	return out
}

// LoadAll rereads files every turn, then reuses the snapshot when their bytes match.
func (l *Loader) LoadAll(cwd string) *Catalog {
	l.mu.Lock()
	defer l.mu.Unlock()
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		log.Printf("skills: workspace: %v", err)
		return &Catalog{Skills: []Skill{}}
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	global := l.GlobalRoot()
	var files []source
	seen := map[string]bool{}
	agents := func(dir string) {
		path := filepath.Join(dir, "AGENTS.md")
		if !seen[path] {
			seen[path] = true
			if data, ok := readRegular(path); ok {
				files = append(files, source{path: path, data: data, agents: true})
			}
		}
	}
	if global != "" {
		agents(global)
	}
	for _, dir := range projectDirs(cwd) {
		agents(dir)
	}
	memory := func(path string) {
		if !seen[path] {
			seen[path] = true
			if data, ok := readRegular(path); ok {
				files = append(files, source{path: path, data: data, agents: true, memory: true})
			}
		}
	}
	if global != "" {
		memory(filepath.Join(global, "MEMORY.md"))
	}
	memory(filepath.Join(cwd, ".talos", "MEMORY.md"))
	if global != "" {
		files = append(files, skillFiles(false, global, "skills")...)
	}
	files = append(files, skillFiles(true, cwd, ".talos", "skills")...)
	hash := sha256.New()
	for _, file := range files {
		fmt.Fprintf(hash, "%t:%t:%t:%d:%s:%d:", file.agents, file.memory, file.project, len(file.path), file.path, len(file.data))
		hash.Write(file.data)
	}
	var sum [32]byte
	copy(sum[:], hash.Sum(nil))
	key := cwd + "\x00" + global
	if entry, ok := l.cache[key]; ok && entry.hash == sum {
		return entry.catalog
	}
	catalog := &Catalog{Skills: []Skill{}}
	var instructions strings.Builder
	byName := map[string]Skill{}
	for _, file := range files {
		if file.agents {
			if instructions.Len() == 0 {
				instructions.WriteString("\nAGENTS.md instructions (later, nearer-workspace instructions take precedence):\n")
			}
			if file.memory {
				fmt.Fprintf(&instructions, "\nMemory from %s:\n%s\n", file.path, file.data)
			} else {
				fmt.Fprintf(&instructions, "\nInstructions from %s:\n%s\n", file.path, file.data)
			}
			continue
		}
		skill, err := Parse(file.data)
		if err != nil {
			log.Printf("skills: skip %s: %v", file.path, err)
			continue
		}
		skill.Dir = filepath.Dir(file.path)
		skill.Project = file.project
		byName[skill.Name] = skill
	}
	catalog.agentsPrompt = clipAgents(instructions.String())
	catalog.Prompt = catalog.agentsPrompt
	for _, skill := range byName {
		catalog.Skills = append(catalog.Skills, skill)
	}
	sort.Slice(catalog.Skills, func(i, j int) bool { return catalog.Skills[i].Name < catalog.Skills[j].Name })
	if len(catalog.Skills) > 0 {
		var summary strings.Builder
		summary.WriteString("\nAvailable skills (use the skill tool to read one):\n")
		for _, skill := range catalog.Skills {
			fmt.Fprintf(&summary, "- %s: %s\n", skill.Name, skill.Description)
		}
		catalog.Prompt += summary.String()
	}
	if l.cache == nil {
		l.cache = map[string]cached{}
	}
	l.cache[key] = cached{hash: sum, catalog: catalog}
	return catalog
}

func projectDirs(cwd string) []string {
	var dirs []string
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		marker := filepath.Join(dir, ".git")
		info, err := os.Lstat(marker)
		git := err == nil && info.IsDir()
		if err == nil && info.Mode().IsRegular() {
			if data, ok := readRegular(marker); ok {
				value, found := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
				git = found && strings.TrimSpace(value) != ""
			}
		}
		if git {
			for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
			return dirs
		}
		if filepath.Dir(dir) == dir {
			return []string{cwd}
		}
	}
}

func skillFiles(project bool, base string, dirs ...string) []source {
	root, err := os.OpenRoot(base)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("skills: read %s: %v", base, err)
		}
		return nil
	}
	defer func() { root.Close() }()
	dir := base
	// shortcut: linked skill directories/files are ignored; add explicit trust handling before supporting them.
	for _, name := range dirs {
		dir = filepath.Join(dir, name)
		next, err := openChild(root, name)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("skills: skip %s: %v", dir, err)
			}
			return nil
		}
		root.Close()
		root = next
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		log.Printf("skills: read %s: %v", dir, err)
		return nil
	}
	var files []source
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "SKILL.md")
		child, err := openChild(root, entry.Name())
		if err != nil {
			log.Printf("skills: skip %s: %v", path, err)
			continue
		}
		f, err := child.OpenFile("SKILL.md", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		data, ok := readOpened(path, f, err)
		child.Close()
		if ok {
			files = append(files, source{path: path, data: data, project: project})
		}
	}
	return files
}

func openChild(root *os.Root, name string) (*os.Root, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("not a regular directory")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, errors.New("directory changed while loading skills")
	}
	return child, nil
}

func readRegular(path string) ([]byte, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	return readOpened(path, f, err)
}

func readOpened(path string, f *os.File, err error) ([]byte, bool) {
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("skills: skip %s: %v", path, err)
		}
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		log.Printf("skills: skip non-regular file %s", path)
		return nil, false
	}
	data, err := io.ReadAll(f)
	if err != nil || !utf8.Valid(data) {
		log.Printf("skills: skip unreadable or non-UTF8 file %s", path)
		return nil, false
	}
	return data, true
}

func clipAgents(s string) string {
	if len(s) <= agentsLimit {
		return s
	}
	const suffix = "\n[AGENTS.md instructions truncated at 32 KiB.]\n"
	end := agentsLimit - len(suffix)
	for !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + suffix
}

// Parse parses a SKILL.md document with flat, single-line YAML scalar fields.
func Parse(data []byte) (Skill, error) { return parseSkill(data) }

// parseSkill deliberately accepts only flat, single-line YAML scalar fields.
func parseSkill(data []byte) (Skill, error) {
	line, rest, ok := strings.Cut(string(data), "\n")
	if !ok || strings.TrimSuffix(line, "\r") != "---" {
		return Skill{}, errors.New("missing opening front matter delimiter")
	}
	fields := map[string]string{}
	for {
		line, rest, ok = strings.Cut(rest, "\n")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "---" {
			if fields["name"] == "" || fields["description"] == "" {
				return Skill{}, errors.New("name and description are required")
			}
			return Skill{Name: fields["name"], Description: fields["description"], Body: rest}, nil
		}
		if !ok {
			return Skill{}, errors.New("missing closing front matter delimiter")
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found || key == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, " \t[]{}") {
			return Skill{}, errors.New("expected a flat key: value field")
		}
		if _, exists := fields[key]; exists {
			return Skill{}, errors.New("duplicate front matter field")
		}
		value, err := scalar(strings.TrimSpace(value))
		if err != nil {
			return Skill{}, err
		}
		fields[key] = strings.Join(strings.Fields(value), " ")
	}
}

func scalar(value string) (string, error) {
	bad := errors.New("only simple single-line scalar fields are supported")
	if value == "" {
		return "", bad
	}
	switch value[0] {
	case '"':
		// shortcut: quoted values use JSON escapes; extend only when richer YAML scalars are needed.
		var text string
		if err := json.Unmarshal([]byte(value), &text); err != nil {
			return "", bad
		}
		return text, nil
	case '\'':
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", bad
		}
		inner := value[1 : len(value)-1]
		if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			return "", bad
		}
		return strings.ReplaceAll(inner, "''", "'"), nil
	case '|', '>', '[', '{', '&', '*', '!', '#':
		return "", bad
	}
	if strings.Contains(value, ": ") {
		return "", bad
	}
	if at := strings.Index(value, " #"); at >= 0 {
		value = strings.TrimSpace(value[:at])
	}
	return value, nil
}
