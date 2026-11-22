package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shqingda/stratum/kernel/mcp"
	"github.com/shqingda/stratum/kernel/skills"
	"github.com/shqingda/stratum/proto"
)

const (
	trustKey          = "mcp.trusted"
	disabledSkillsKey = "skills.disabled"
)

func (m *Manager) disabledSkills(ctx context.Context) []string {
	var names []string
	if b, err := m.Store.Setting(ctx, disabledSkillsKey); err == nil && b != nil {
		_ = json.Unmarshal(b, &names)
	}
	return names
}

func (m *Manager) skillDisabled(ctx context.Context, name string) bool {
	return slices.Contains(m.disabledSkills(ctx), name)
}

func trustID(cwd string, e mcp.Entry) string {
	return cwd + "\x00" + e.Name + "\x00" + e.Fingerprint
}

func (m *Manager) trusted(ctx context.Context) []string {
	var ids []string
	if b, err := m.Store.Setting(ctx, trustKey); err == nil && b != nil {
		json.Unmarshal(b, &ids)
	}
	return ids
}

// UseMCP serves the tools of the servers in userFile and in each folder's
// .mcp.json, the latter once the user trusts them.
func (m *Manager) UseMCP(userFile string) {
	m.MCP = &mcp.Hub{
		UserFile: userFile,
		Trusted: func(cwd string, e mcp.Entry) bool {
			return slices.Contains(m.trusted(context.Background()), trustID(cwd, e))
		},
		ExtraEntries: m.pluginMCPEntries,
		Changed:      func() { m.Broadcast(proto.EventMCP, struct{}{}) },
	}
}

// MCPList says how each server and skill is for a folder.
func (m *Manager) MCPList(ctx context.Context, q proto.MCPQuery) (proto.MCPList, error) {
	out := proto.MCPList{Servers: []proto.MCPServer{}}
	if q.Cwd != "" {
		disabled := m.disabledSkills(ctx)
		for _, sk := range m.Skills.LoadAll(q.Cwd).Skills {
			out.Skills = append(out.Skills, proto.SkillInfo{
				Name:        sk.Name,
				Description: sk.Description,
				Dir:         sk.Dir,
				Body:        sk.Body,
				Project:     sk.Project,
				Disabled:    slices.Contains(disabled, sk.Name),
			})
		}
	}
	out.Commands = commands(q.Cwd)
	if m.MCP == nil {
		return out, nil
	}
	sts, err := m.MCP.Status(q.Cwd)
	if err != nil {
		out.Error = err.Error()
	}
	for _, st := range sts {
		target := st.Server.URL
		if st.Server.Command != "" {
			target = strings.Join(append([]string{st.Server.Command}, st.Server.Args...), " ")
		}
		out.Servers = append(out.Servers, proto.MCPServer{Name: st.Name, Project: st.Project, Target: target, State: st.State, Error: st.Error, Tools: st.Tools, Logs: st.Logs})
	}
	out.Resources = m.MCP.Resources(q.Cwd)
	out.Prompts = m.MCP.Prompts(q.Cwd)
	return out, nil
}

// MCPTrust lets a folder's server run, or stops trusting it.
func (m *Manager) MCPTrust(ctx context.Context, t proto.MCPTrust) (proto.MCPList, error) {
	if m.MCP == nil {
		return proto.MCPList{}, proto.Errorf("bad_request", "MCP is off")
	}
	es, _ := mcp.Load(m.MCP.UserFile, t.Cwd)
	i := slices.IndexFunc(es, func(e mcp.Entry) bool { return e.Name == t.Name && e.Project })
	if i < 0 {
		return proto.MCPList{}, proto.Errorf("not_found", "no server %q in %s/.mcp.json", t.Name, t.Cwd)
	}
	id := trustID(t.Cwd, es[i])
	ids := slices.DeleteFunc(m.trusted(ctx), func(x string) bool { return strings.HasPrefix(x, t.Cwd+"\x00"+t.Name+"\x00") })
	if t.Trust {
		ids = append(ids, id)
	}
	b, _ := json.Marshal(ids)
	if err := m.Store.SetSetting(ctx, trustKey, b); err != nil {
		return proto.MCPList{}, err
	}
	return m.MCPList(ctx, proto.MCPQuery{Cwd: t.Cwd})
}

func (m *Manager) mcpConfigPath(cwd string, project bool) (string, error) {
	if project {
		if strings.TrimSpace(cwd) == "" {
			return "", proto.Errorf("bad_request", "cwd is required for a project MCP server")
		}
		return filepath.Join(cwd, ".mcp.json"), nil
	}
	if m.MCP == nil || m.MCP.UserFile == "" {
		return "", proto.Errorf("bad_request", "user MCP config path is not set")
	}
	return m.MCP.UserFile, nil
}

// MCPSave adds or updates an MCP server in ~/.talos/mcp.json or <cwd>/.mcp.json.
func (m *Manager) MCPSave(ctx context.Context, s proto.MCPSave) (proto.MCPList, error) {
	if m.MCP == nil {
		return proto.MCPList{}, proto.Errorf("bad_request", "MCP is off")
	}
	name := strings.TrimSpace(s.Name)
	cmd := strings.TrimSpace(s.Command)
	url := strings.TrimSpace(s.URL)
	if name == "" || (cmd == "" && url == "") {
		return proto.MCPList{}, proto.Errorf("bad_request", "server name and command or URL are required")
	}
	path, err := m.mcpConfigPath(s.Cwd, s.Project)
	if err != nil {
		return proto.MCPList{}, err
	}
	doc := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return proto.MCPList{}, proto.Errorf("bad_request", "%s: %v", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return proto.MCPList{}, err
	}
	servers := map[string]mcp.Server{}
	if raw, ok := doc["mcpServers"]; ok {
		rb, _ := json.Marshal(raw)
		_ = json.Unmarshal(rb, &servers)
	}
	entry := servers[name]
	if url != "" {
		entry.URL = url
		entry.Command, entry.Args = "", nil
	} else {
		entry.Command = cmd
		entry.Args = s.Args
		entry.URL = ""
	}
	servers[name] = entry
	doc["mcpServers"] = servers
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return proto.MCPList{}, err
	}
	outBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return proto.MCPList{}, err
	}
	if err := os.WriteFile(path, append(outBytes, '\n'), 0o600); err != nil {
		return proto.MCPList{}, err
	}
	if s.Project {
		es, _ := mcp.Load(m.MCP.UserFile, s.Cwd)
		if i := slices.IndexFunc(es, func(e mcp.Entry) bool { return e.Name == name && e.Project }); i >= 0 {
			ids := slices.DeleteFunc(m.trusted(ctx), func(x string) bool { return strings.HasPrefix(x, s.Cwd+"\x00"+name+"\x00") })
			ids = append(ids, trustID(s.Cwd, es[i]))
			tb, _ := json.Marshal(ids)
			_ = m.Store.SetSetting(ctx, trustKey, tb)
		}
	}
	m.MCP.Reset()
	m.Broadcast(proto.EventMCP, struct{}{})
	return m.MCPList(ctx, proto.MCPQuery{Cwd: s.Cwd})
}

// MCPRemove deletes an MCP server from ~/.talos/mcp.json or <cwd>/.mcp.json.
func (m *Manager) MCPRemove(ctx context.Context, r proto.MCPRemove) (proto.MCPList, error) {
	if m.MCP == nil {
		return proto.MCPList{}, proto.Errorf("bad_request", "MCP is off")
	}
	path, err := m.mcpConfigPath(r.Cwd, r.Project)
	if err != nil {
		return proto.MCPList{}, err
	}
	doc := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return proto.MCPList{}, proto.Errorf("bad_request", "%s: %v", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return proto.MCPList{}, err
	}
	servers := map[string]mcp.Server{}
	if raw, ok := doc["mcpServers"]; ok {
		rb, _ := json.Marshal(raw)
		_ = json.Unmarshal(rb, &servers)
	}
	delete(servers, r.Name)
	doc["mcpServers"] = servers
	if fileExists(path) || len(servers) > 0 {
		outBytes, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return proto.MCPList{}, err
		}
		if err := os.WriteFile(path, append(outBytes, '\n'), 0o600); err != nil {
			return proto.MCPList{}, err
		}
	}
	if r.Project {
		ids := slices.DeleteFunc(m.trusted(ctx), func(x string) bool { return strings.HasPrefix(x, r.Cwd+"\x00"+r.Name+"\x00") })
		tb, _ := json.Marshal(ids)
		_ = m.Store.SetSetting(ctx, trustKey, tb)
	}
	m.MCP.Reset()
	m.Broadcast(proto.EventMCP, struct{}{})
	return m.MCPList(ctx, proto.MCPQuery{Cwd: r.Cwd})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// SkillToggle enables or disables a skill by name.
func (m *Manager) SkillToggle(ctx context.Context, t proto.SkillToggle) (proto.MCPList, error) {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return proto.MCPList{}, proto.Errorf("bad_request", "skill name is required")
	}
	disabled := slices.DeleteFunc(m.disabledSkills(ctx), func(n string) bool { return n == name })
	if !t.Enabled {
		disabled = append(disabled, name)
	}
	b, _ := json.Marshal(disabled)
	if err := m.Store.SetSetting(ctx, disabledSkillsKey, b); err != nil {
		return proto.MCPList{}, err
	}
	return m.MCPList(ctx, proto.MCPQuery{Cwd: t.Cwd})
}

func (m *Manager) skillsRoot(cwd string, project bool) (string, error) {
	if project {
		if strings.TrimSpace(cwd) == "" {
			return "", proto.Errorf("bad_request", "cwd is required for a project skill")
		}
		return filepath.Join(cwd, ".talos", "skills"), nil
	}
	global := m.Skills.GlobalRoot()
	if global == "" {
		return "", proto.Errorf("bad_request", "global skills folder is unavailable")
	}
	return filepath.Join(global, "skills"), nil
}

func validSkillName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && name != "." && name != ".."
}

// SkillSave creates a skill or installs skills from a local folder or git URL.
func (m *Manager) SkillSave(ctx context.Context, s proto.SkillSave) (proto.MCPList, error) {
	root, err := m.skillsRoot(s.Cwd, s.Project)
	if err != nil {
		return proto.MCPList{}, err
	}
	if src := strings.TrimSpace(s.Source); src != "" {
		if err := installSkillsFromSource(ctx, src, root); err != nil {
			return proto.MCPList{}, err
		}
		return m.MCPList(ctx, proto.MCPQuery{Cwd: s.Cwd})
	}
	name := strings.TrimSpace(s.Name)
	desc := strings.TrimSpace(s.Description)
	if !validSkillName(name) || desc == "" {
		return proto.MCPList{}, proto.Errorf("bad_request", "skill name and description are required")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return proto.MCPList{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(formatSkillDoc(name, desc, s.Body)), 0o644); err != nil {
		return proto.MCPList{}, err
	}
	return m.MCPList(ctx, proto.MCPQuery{Cwd: s.Cwd})
}

// SkillRemove deletes an installed skill directory.
func (m *Manager) SkillRemove(ctx context.Context, r proto.SkillRemove) (proto.MCPList, error) {
	name := strings.TrimSpace(r.Name)
	if !validSkillName(name) {
		return proto.MCPList{}, proto.Errorf("bad_request", "invalid skill name")
	}
	root, err := m.skillsRoot(r.Cwd, r.Project)
	if err != nil {
		return proto.MCPList{}, err
	}
	if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
		return proto.MCPList{}, err
	}
	return m.MCPList(ctx, proto.MCPQuery{Cwd: r.Cwd})
}

func formatSkillDoc(name, desc, body string) string {
	nb, _ := json.Marshal(strings.Join(strings.Fields(name), " "))
	db, _ := json.Marshal(strings.Join(strings.Fields(desc), " "))
	body = strings.TrimSpace(body)
	if body != "" {
		body += "\n"
	}
	return "---\nname: " + string(nb) + "\ndescription: " + string(db) + "\n---\n" + body
}

func installSkillsFromSource(ctx context.Context, src, targetRoot string) error {
	srcDir := src
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		tmp, err := os.MkdirTemp("", "talos-skill-src-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		cloneCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", src, tmp).CombinedOutput()
		if err != nil {
			return proto.Errorf("bad_request", "git clone: %s", strings.TrimSpace(string(out)))
		}
		srcDir = tmp
	}
	candidates := findSkillDirs(srcDir)
	if len(candidates) == 0 {
		return proto.Errorf("not_found", "no SKILL.md found in %s", src)
	}
	for _, c := range candidates {
		data, err := os.ReadFile(filepath.Join(c, "SKILL.md"))
		if err != nil {
			return err
		}
		sk, err := skills.Parse(data)
		if err != nil {
			return proto.Errorf("bad_request", "invalid SKILL.md in %s: %v", c, err)
		}
		name := sk.Name
		if !validSkillName(name) {
			name = filepath.Base(c)
		}
		if !validSkillName(name) {
			return proto.Errorf("bad_request", "invalid skill name %q", sk.Name)
		}
		if err := copySkillDir(c, filepath.Join(targetRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

func findSkillDirs(root string) []string {
	if fileExists(filepath.Join(root, "SKILL.md")) {
		return []string{root}
	}
	var out []string
	for _, pattern := range []string{
		filepath.Join(root, "*", "SKILL.md"),
		filepath.Join(root, "skills", "*", "SKILL.md"),
	} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			out = append(out, filepath.Dir(m))
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}

func copySkillDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 || e.Name() == ".git" {
			continue
		}
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copySkillDir(sp, dp); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(sp)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		if closeErr := out.Close(); copyErr != nil {
			return copyErr
		} else if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// commands reads the saved prompts for cwd: the folder's own first, then
// the user's; a name already taken is skipped. The first line is the
// description, unless the file starts with front matter.
func commands(cwd string) []proto.CommandInfo {
	var dirs []string
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, ".stratum", "commands"), filepath.Join(cwd, ".talos", "commands"), filepath.Join(cwd, ".claude", "commands"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".stratum", "commands"), filepath.Join(home, ".talos", "commands"))
	}
	var out []proto.CommandInfo
	seen := map[string]bool{}
	for _, dir := range dirs {
		names, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, path := range names {
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			b, err := os.ReadFile(path)
			if err != nil || seen[name] || len(b) > 64<<10 {
				continue
			}
			seen[name] = true
			body, desc := strings.TrimSpace(string(b)), ""
			if rest, ok := strings.CutPrefix(body, "---\n"); ok {
				if front, after, ok := strings.Cut(rest, "\n---"); ok {
					body = strings.TrimSpace(after)
					for _, line := range strings.Split(front, "\n") {
						if v, ok := strings.CutPrefix(line, "description:"); ok {
							desc = strings.Trim(strings.TrimSpace(v), `"'`)
						}
					}
				}
			}
			if desc == "" {
				desc, _, _ = strings.Cut(body, "\n")
				desc = strings.TrimLeft(desc, "# ")
			}
			out = append(out, proto.CommandInfo{Name: name, Description: desc, Body: body})
		}
	}
	return out
}
