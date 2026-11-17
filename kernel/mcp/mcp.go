// Package mcp connects to the MCP servers the user configured and offers
// their tools to sessions.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shqingda/talos/kernel/perm"
	"github.com/shqingda/talos/kernel/tools"
	"github.com/shqingda/talos/proto"
)

// Server is a server's configuration, as .mcp.json files write it: a
// command to run, or a URL.
type Server struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Entry is a configured server: Project ones come from the folder's
// .mcp.json and run only once the user trusts them.
type Entry struct {
	Name        string
	Server      Server
	Project     bool
	Fingerprint string // of the configuration, so that a changed one is trusted again
}

// Status is how a server is for a folder.
type Status struct {
	Entry
	State string // untrusted, connecting, ok, fail
	Error string
	Tools []string
}

// connectTimeout bounds starting a server and listing its tools.
const connectTimeout = 20 * time.Second

// Load reads the user's file and the folder's .mcp.json; a project server
// does not replace a user one of the same name.
func Load(userFile, cwd string) ([]Entry, error) {
	var out []Entry
	var errs []error
	for _, f := range []struct {
		path    string
		project bool
	}{{userFile, false}, {filepath.Join(cwd, ".mcp.json"), true}} {
		if f.path == "" {
			continue
		}
		b, err := os.ReadFile(f.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		var file struct {
			Servers map[string]Server `json:"mcpServers"`
		}
		if err == nil {
			err = json.Unmarshal(b, &file)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.path, err))
			continue
		}
		names := make([]string, 0, len(file.Servers))
		for name := range file.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if slices.ContainsFunc(out, func(e Entry) bool { return e.Name == name }) {
				continue
			}
			s := file.Servers[name]
			fp, _ := json.Marshal(s)
			sum := sha256.Sum256(fp)
			out = append(out, Entry{Name: name, Server: s, Project: f.project, Fingerprint: hex.EncodeToString(sum[:8])})
		}
	}
	return out, errors.Join(errs...)
}

// Hub keeps the connections, one for each server and folder it runs in.
type Hub struct {
	UserFile string
	// Trusted tells whether the user let a project server run in cwd.
	Trusted func(cwd string, e Entry) bool
	// Changed is told when a server connects or fails.
	Changed func()

	mu    sync.Mutex
	conns map[string]*conn
}

type conn struct {
	ready     chan struct{}
	session   *sdk.ClientSession
	tools     []*sdk.Tool
	resources []proto.MCPResource
	err       error
}

func key(cwd string, e Entry) string {
	if !e.Project {
		cwd = ""
	}
	return cwd + "\x00" + e.Name + "\x00" + e.Fingerprint
}

func (h *Hub) entries(cwd string) ([]Entry, error) {
	es, err := Load(h.UserFile, cwd)
	return slices.DeleteFunc(es, func(e Entry) bool {
		return e.Project && (h.Trusted == nil || !h.Trusted(cwd, e))
	}), err
}

// conn returns the connection of e, starting it the first time.
func (h *Hub) conn(cwd string, e Entry) *conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := key(cwd, e)
	if c := h.conns[k]; c != nil {
		return c
	}
	if h.conns == nil {
		h.conns = map[string]*conn{}
	}
	c := &conn{ready: make(chan struct{})}
	h.conns[k] = c
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		c.session, c.tools, c.resources, c.err = dial(ctx, cwd, e)
		close(c.ready)
		if h.Changed != nil {
			h.Changed()
		}
	}()
	return c
}

// drop forgets a connection that broke, so the next turn dials again.
func (h *Hub) drop(cwd string, e Entry, c *conn) {
	h.mu.Lock()
	if h.conns[key(cwd, e)] == c {
		delete(h.conns, key(cwd, e))
	}
	h.mu.Unlock()
	if c.session != nil {
		c.session.Close()
	}
}

func dial(ctx context.Context, cwd string, e Entry) (*sdk.ClientSession, []*sdk.Tool, []proto.MCPResource, error) {
	var t sdk.Transport
	s := e.Server
	switch {
	case s.Command != "":
		cmd := exec.Command(os.ExpandEnv(s.Command), expand(s.Args)...)
		cmd.Env = os.Environ()
		for k, v := range s.Env {
			cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
		}
		cmd.Dir, _ = os.UserHomeDir()
		if e.Project {
			cmd.Dir = cwd
		}
		t = &sdk.CommandTransport{Command: cmd}
	case s.URL != "":
		headers := map[string]string{}
		for k, v := range s.Headers {
			headers[k] = os.ExpandEnv(v)
		}
		t = &sdk.StreamableClientTransport{Endpoint: os.ExpandEnv(s.URL), DisableStandaloneSSE: true,
			HTTPClient: &http.Client{Transport: headerTransport(headers)}}
	default:
		return nil, nil, nil, errors.New("neither command nor url is set")
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "talos", Version: "0.2.0"}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	var list []*sdk.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			session.Close()
			return nil, nil, nil, err
		}
		list = append(list, tool)
	}
	// A server with no resources, or a list that fails, still keeps its tools.
	var resources []proto.MCPResource
	for r, err := range session.Resources(ctx, nil) {
		if err != nil || len(resources) == 50 {
			break
		}
		if r == nil || r.URI == "" || strings.ContainsAny(r.URI, " \t") {
			continue
		}
		resources = append(resources, proto.MCPResource{URI: r.URI, Name: r.Name, Description: r.Description})
	}
	return session, list, resources, nil
}

func expand(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = os.ExpandEnv(a)
	}
	return out
}

type headerTransport map[string]string

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// Status says how each configured server is for cwd, without waiting:
// servers not tried yet start connecting.
func (h *Hub) Status(cwd string) ([]Status, error) {
	all, err := Load(h.UserFile, cwd)
	out := make([]Status, 0, len(all))
	for _, e := range all {
		st := Status{Entry: e, State: "untrusted"}
		if !e.Project || h.Trusted != nil && h.Trusted(cwd, e) {
			c := h.conn(cwd, e)
			select {
			case <-c.ready:
				st.State = "ok"
				if c.err != nil {
					st.State, st.Error = "fail", c.err.Error()
				}
				for _, t := range c.tools {
					st.Tools = append(st.Tools, t.Name)
				}
			default:
				st.State = "connecting"
			}
		}
		out = append(out, st)
	}
	return out, err
}

// Tools are the tools of the servers cwd may use, waiting for servers
// still connecting; ones that failed are left out.
func (h *Hub) Tools(ctx context.Context, cwd string) tools.Set {
	es, _ := h.entries(cwd)
	var out tools.Set
	for _, e := range es {
		c := h.conn(cwd, e)
		select {
		case <-c.ready:
		case <-ctx.Done():
			return out
		}
		if c.err != nil {
			continue
		}
		for _, t := range c.tools {
			out = append(out, h.tool(cwd, e, c, t))
		}
	}
	return out
}

var unsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ToolName is how a server's tool is named to the model.
func ToolName(server, tool string) string {
	name := "mcp__" + unsafe.ReplaceAllString(server, "_") + "__" + unsafe.ReplaceAllString(tool, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// ServerOf splits a tool name made by ToolName; ok is false for other tools.
func ServerOf(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	return server, tool, ok
}

func (h *Hub) tool(cwd string, e Entry, c *conn, t *sdk.Tool) *tools.Tool {
	name := ToolName(e.Name, t.Name)
	schema, _ := json.Marshal(t.InputSchema)
	if t.InputSchema == nil {
		schema = []byte(`{"type":"object"}`)
	}
	return &tools.Tool{
		Name:        name,
		Description: t.Description,
		Schema:      schema,
		Access: func(env tools.Env, args json.RawMessage) (perm.Request, error) {
			return perm.Request{Tool: name, Kind: perm.MCP, Command: e.Name + " · " + t.Name}, nil
		},
		Run: func(ctx context.Context, env tools.Env, args json.RawMessage) (string, error) {
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			res, err := c.session.CallTool(ctx, &sdk.CallToolParams{Name: t.Name, Arguments: args})
			if err != nil {
				if ctx.Err() == nil {
					h.drop(cwd, e, c)
				}
				return "", err
			}
			var parts []string
			for _, ct := range res.Content {
				switch ct := ct.(type) {
				case *sdk.TextContent:
					parts = append(parts, ct.Text)
				case *sdk.ImageContent:
					parts = append(parts, "[image "+ct.MIMEType+"]")
				default:
					b, _ := json.Marshal(ct)
					parts = append(parts, string(b))
				}
			}
			out := strings.Join(parts, "\n")
			if out == "" && res.StructuredContent != nil {
				b, _ := json.Marshal(res.StructuredContent)
				out = string(b)
			}
			if res.IsError {
				return "", errors.New(out)
			}
			return out, nil
		},
	}
}

// HasServer reports whether cwd may use a server of that name.
func (h *Hub) HasServer(cwd, name string) bool {
	es, _ := h.entries(cwd)
	return slices.ContainsFunc(es, func(e Entry) bool { return e.Name == name && !strings.ContainsAny(e.Name, " \t:") })
}

// Resources are the resources of servers cwd may use that have already
// connected. It does not wait for one still connecting.
func (h *Hub) Resources(cwd string) []proto.MCPResource {
	es, _ := h.entries(cwd)
	var out []proto.MCPResource
	for _, e := range es {
		c := h.conn(cwd, e)
		select {
		case <-c.ready:
		default:
			continue
		}
		if c.err != nil {
			continue
		}
		for _, r := range c.resources {
			r.Server = e.Name
			out = append(out, r)
		}
	}
	return out
}

// ReadResource reads one resource. A missing URI returns the server's error
// and leaves the connection up.
func (h *Hub) ReadResource(ctx context.Context, cwd, server, uri string) (string, error) {
	es, err := h.entries(cwd)
	if err != nil && len(es) == 0 {
		return "", err
	}
	for _, e := range es {
		if e.Name != server {
			continue
		}
		c := h.conn(cwd, e)
		select {
		case <-c.ready:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if c.err != nil {
			return "", c.err
		}
		res, err := c.session.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
		if err != nil {
			return "", err
		}
		return resourceText(res), nil
	}
	return "", fmt.Errorf("no MCP server %q", server)
}

func resourceText(res *sdk.ReadResourceResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Contents {
		if c == nil {
			continue
		}
		switch {
		case c.Text != "":
			parts = append(parts, c.Text)
		case len(c.Blob) > 0:
			parts = append(parts, "[binary "+c.MIMEType+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// Close stops every server.
func (h *Hub) Close() {
	h.mu.Lock()
	conns := h.conns
	h.conns = nil
	h.mu.Unlock()
	for _, c := range conns {
		<-c.ready
		if c.session != nil {
			c.session.Close()
		}
	}
}
