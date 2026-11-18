package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shqingda/talos/proto"
)

const disabledPluginsKey = "plugins:disabled"

type pluginManifest struct {
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	Description string                     `json:"description"`
	Author      string                     `json:"author"`
	Skills      []string                   `json:"skills"`
	Hooks       string                     `json:"hooks"`
	MCPServers  map[string]json.RawMessage `json:"mcpServers"`
}

func (m *Manager) globalPluginsDir() string {
	global := m.Skills.GlobalDir
	if global == "" {
		if home, err := os.UserHomeDir(); err == nil {
			global = filepath.Join(home, ".talos")
		}
	}
	if global == "" {
		return ""
	}
	return filepath.Join(global, "plugins")
}

func (m *Manager) disabledPlugins(ctx context.Context) map[string]bool {
	out := make(map[string]bool)
	if m.Store == nil {
		return out
	}
	b, err := m.Store.Setting(ctx, disabledPluginsKey)
	if err != nil || len(b) == 0 {
		return out
	}
	var names []string
	if json.Unmarshal(b, &names) == nil {
		for _, n := range names {
			out[n] = true
		}
	}
	return out
}

func (m *Manager) setPluginDisabled(ctx context.Context, name string, disabled bool) error {
	cur := m.disabledPlugins(ctx)
	if disabled {
		cur[name] = true
	} else {
		delete(cur, name)
	}
	names := make([]string, 0, len(cur))
	for k := range cur {
		names = append(names, k)
	}
	sort.Strings(names)
	b, _ := json.Marshal(names)
	return m.Store.SetSetting(ctx, disabledPluginsKey, b)
}

func readPluginManifest(dir string) (pluginManifest, bool) {
	candidates := []string{
		filepath.Join(dir, ".talos-plugin", "plugin.json"),
		filepath.Join(dir, ".claude-plugin", "plugin.json"),
		filepath.Join(dir, "plugin.json"),
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var man pluginManifest
		if json.Unmarshal(b, &man) == nil {
			if man.Name == "" {
				man.Name = filepath.Base(dir)
			}
			return man, true
		}
	}
	return pluginManifest{}, false
}

func (m *Manager) scanPluginDirs(cwd string) []struct {
	dir     string
	project bool
} {
	var roots []struct {
		dir     string
		project bool
	}
	if cwd != "" {
		roots = append(roots, struct {
			dir     string
			project bool
		}{filepath.Join(cwd, ".talos", "plugins"), true})
	}
	if gd := m.globalPluginsDir(); gd != "" {
		roots = append(roots, struct {
			dir     string
			project bool
		}{gd, false})
	}
	var out []struct {
		dir     string
		project bool
	}
	for _, r := range roots {
		entries, err := os.ReadDir(r.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			out = append(out, struct {
				dir     string
				project bool
			}{filepath.Join(r.dir, e.Name()), r.project})
		}
	}
	return out
}

func (m *Manager) pluginHookFiles(cwd string) []string {
	disabled := m.disabledPlugins(context.Background())
	var files []string
	for _, p := range m.scanPluginDirs(cwd) {
		man, ok := readPluginManifest(p.dir)
		if !ok || disabled[man.Name] {
			continue
		}
		hookRel := man.Hooks
		if hookRel == "" {
			hookRel = filepath.Join("hooks", "hooks.json")
		}
		hp := filepath.Join(p.dir, hookRel)
		if _, err := os.Stat(hp); err == nil {
			files = append(files, hp)
		}
	}
	return files
}

// ListPlugins lists installed global and project plugins.
func (m *Manager) ListPlugins(ctx context.Context, cwd string) (proto.PluginList, error) {
	disabled := m.disabledPlugins(ctx)
	var out proto.PluginList
	for _, p := range m.scanPluginDirs(cwd) {
		man, ok := readPluginManifest(p.dir)
		if !ok {
			man = pluginManifest{Name: filepath.Base(p.dir)}
		}
		skillCount := countPluginSkills(p.dir, man.Skills)
		hookCount := countPluginHooks(p.dir, man.Hooks)
		mcpNames := make([]string, 0, len(man.MCPServers))
		for k := range man.MCPServers {
			mcpNames = append(mcpNames, k)
		}
		sort.Strings(mcpNames)
		out.Plugins = append(out.Plugins, proto.PluginInfo{
			Name:        man.Name,
			Version:     man.Version,
			Description: man.Description,
			Author:      man.Author,
			Dir:         p.dir,
			Project:     p.project,
			Enabled:     !disabled[man.Name],
			Skills:      skillCount,
			Hooks:       hookCount,
			MCPServers:  mcpNames,
		})
	}
	return out, nil
}

func countPluginSkills(dir string, roots []string) int {
	if len(roots) == 0 {
		roots = []string{"skills"}
	}
	n := 0
	for _, r := range roots {
		entries, err := os.ReadDir(filepath.Join(dir, r))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(dir, r, e.Name(), "SKILL.md")); err == nil {
					n++
				}
			}
		}
	}
	return n
}

func countPluginHooks(dir, hookRel string) int {
	if hookRel == "" {
		hookRel = filepath.Join("hooks", "hooks.json")
	}
	b, err := os.ReadFile(filepath.Join(dir, hookRel))
	if err != nil {
		return 0
	}
	var doc hookDoc
	if json.Unmarshal(b, &doc) != nil {
		return 0
	}
	n := len(doc.PreToolUse) + len(doc.PostToolUse) + len(doc.Stop)
	for _, g := range doc.Hooks {
		n += len(g)
	}
	return n
}

// InstallPlugin installs a plugin package from a local directory or git URL.
func (m *Manager) InstallPlugin(ctx context.Context, p proto.PluginInstall) (proto.PluginList, error) {
	src := strings.TrimSpace(p.Source)
	if src == "" {
		return proto.PluginList{}, proto.Errorf("bad_request", "plugin source is required")
	}
	var baseDir string
	if p.Project {
		if strings.TrimSpace(p.Cwd) == "" {
			return proto.PluginList{}, proto.Errorf("bad_request", "cwd is required for project plugin")
		}
		baseDir = filepath.Join(p.Cwd, ".talos", "plugins")
	} else {
		baseDir = m.globalPluginsDir()
	}
	if baseDir == "" {
		return proto.PluginList{}, proto.Errorf("internal", "cannot determine plugins directory")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return proto.PluginList{}, err
	}

	isGit := strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "git@") || strings.HasSuffix(src, ".git")
	if isGit {
		name := strings.TrimSuffix(filepath.Base(strings.TrimRight(src, "/")), ".git")
		if name == "" || name == "." {
			name = "plugin"
		}
		dst := filepath.Join(baseDir, name)
		_ = os.RemoveAll(dst)
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", src, dst)
		if out, err := cmd.CombinedOutput(); err != nil {
			return proto.PluginList{}, proto.Errorf("internal", "git clone plugin: %s", strings.TrimSpace(string(out)))
		}
		return m.ListPlugins(ctx, p.Cwd)
	}

	absSrc, err := filepath.Abs(src)
	if err != nil {
		return proto.PluginList{}, err
	}
	info, err := os.Stat(absSrc)
	if err != nil || !info.IsDir() {
		return proto.PluginList{}, proto.Errorf("bad_request", "plugin source %q is not a directory", src)
	}
	name := filepath.Base(absSrc)
	if man, ok := readPluginManifest(absSrc); ok && man.Name != "" {
		name = man.Name
	}
	dst := filepath.Join(baseDir, name)
	_ = os.RemoveAll(dst)
	if err := copyIncludePath(absSrc, dst); err != nil {
		return proto.PluginList{}, err
	}
	return m.ListPlugins(ctx, p.Cwd)
}

// TogglePlugin enables or disables an installed plugin.
func (m *Manager) TogglePlugin(ctx context.Context, p proto.PluginToggle) (proto.PluginList, error) {
	if strings.TrimSpace(p.Name) == "" {
		return proto.PluginList{}, proto.Errorf("bad_request", "plugin name is required")
	}
	if err := m.setPluginDisabled(ctx, p.Name, !p.Enabled); err != nil {
		return proto.PluginList{}, err
	}
	return m.ListPlugins(ctx, p.Cwd)
}

// RemovePlugin uninstalls a plugin by name.
func (m *Manager) RemovePlugin(ctx context.Context, p proto.PluginRemove) (proto.PluginList, error) {
	for _, pl := range m.scanPluginDirs(p.Cwd) {
		man, ok := readPluginManifest(pl.dir)
		name := filepath.Base(pl.dir)
		if ok && man.Name != "" {
			name = man.Name
		}
		if name == p.Name {
			if err := os.RemoveAll(pl.dir); err != nil {
				return proto.PluginList{}, err
			}
		}
	}
	return m.ListPlugins(ctx, p.Cwd)
}
