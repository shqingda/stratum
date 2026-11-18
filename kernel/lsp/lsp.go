// Package lsp manages persistent Language Server Protocol (LSP) daemons per
// workspace and language, providing definition, references, hover, and live
// diagnostics over JSON-RPC 2.0 stdio transport.
package lsp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ServerSpec describes how to launch a language server.
type ServerSpec struct {
	Language string
	Command  string
	Args     []string
}

// Location is a resolved source position returned by definition or references.
type Location struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`   // 1-based
	Column    int    `json:"column"` // 1-based
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
}

// Diagnostic is a live diagnostic published by a language server.
type Diagnostic struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`   // 1-based
	Column   int    `json:"column"` // 1-based
	Severity string `json:"severity"`
	Source   string `json:"source,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
}

// Manager keeps persistent LSP server processes alive across tool calls.
type Manager struct {
	// Lookup optionally overrides how a language server binary is resolved.
	// If nil, DefaultServerForFile is used.
	Lookup func(root, relPath string) (ServerSpec, bool)

	mu      sync.Mutex
	clients map[clientKey]*Client
}

type clientKey struct {
	root string
	lang string
}

// Default is the shared process-wide LSP manager.
var Default = &Manager{}

// NewManager creates an isolated LSP manager (useful for tests).
func NewManager() *Manager {
	return &Manager{clients: make(map[clientKey]*Client)}
}

// DefaultServerForFile detects the language server for a file path.
func DefaultServerForFile(root, relPath string) (ServerSpec, bool) {
	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".go":
		if _, err := exec.LookPath("gopls"); err == nil {
			return ServerSpec{Language: "go", Command: "gopls"}, true
		}
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		if _, err := exec.LookPath("typescript-language-server"); err == nil {
			return ServerSpec{Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}}, true
		}
	case ".rs":
		if _, err := exec.LookPath("rust-analyzer"); err == nil {
			return ServerSpec{Language: "rust", Command: "rust-analyzer"}, true
		}
	case ".py":
		if _, err := exec.LookPath("pyright-langserver"); err == nil {
			return ServerSpec{Language: "python", Command: "pyright-langserver", Args: []string{"--stdio"}}, true
		}
		if _, err := exec.LookPath("pylsp"); err == nil {
			return ServerSpec{Language: "python", Command: "pylsp"}, true
		}
	}
	return ServerSpec{}, false
}

func (m *Manager) lookup(root, relPath string) (ServerSpec, bool) {
	if m.Lookup != nil {
		return m.Lookup(root, relPath)
	}
	return DefaultServerForFile(root, relPath)
}

func (m *Manager) clientFor(ctx context.Context, root, relPath string) (*Client, ServerSpec, error) {
	spec, ok := m.lookup(root, relPath)
	if !ok {
		return nil, ServerSpec{}, fmt.Errorf("no language server available for %s", filepath.Ext(relPath))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	key := clientKey{root: absRoot, lang: spec.Language}

	m.mu.Lock()
	if m.clients == nil {
		m.clients = make(map[clientKey]*Client)
	}
	c := m.clients[key]
	if c != nil && !c.closed() {
		m.mu.Unlock()
		return c, spec, nil
	}
	delete(m.clients, key)
	m.mu.Unlock()

	c, err = startClient(ctx, absRoot, spec)
	if err != nil {
		return nil, spec, err
	}

	m.mu.Lock()
	if existing := m.clients[key]; existing != nil && !existing.closed() {
		m.mu.Unlock()
		_ = c.Close()
		return existing, spec, nil
	}
	m.clients[key] = c
	m.mu.Unlock()
	return c, spec, nil
}

// Definition returns definition locations for the symbol at 1-based line, col.
func (m *Manager) Definition(ctx context.Context, root, relPath string, line, col int) ([]Location, error) {
	c, _, err := m.clientFor(ctx, root, relPath)
	if err != nil {
		return nil, err
	}
	uri, err := c.syncFile(ctx, relPath)
	if err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": max(0, line-1), "character": max(0, col-1)},
	}
	raw, err := c.call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}
	return parseLocations(c.root, raw), nil
}

// References returns reference locations for the symbol at 1-based line, col.
func (m *Manager) References(ctx context.Context, root, relPath string, line, col int, includeDecl bool) ([]Location, error) {
	c, _, err := m.clientFor(ctx, root, relPath)
	if err != nil {
		return nil, err
	}
	uri, err := c.syncFile(ctx, relPath)
	if err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": max(0, line-1), "character": max(0, col-1)},
		"context":      map[string]any{"includeDeclaration": includeDecl},
	}
	raw, err := c.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}
	return parseLocations(c.root, raw), nil
}

// Hover returns type/documentation text for the symbol at 1-based line, col.
func (m *Manager) Hover(ctx context.Context, root, relPath string, line, col int) (string, error) {
	c, _, err := m.clientFor(ctx, root, relPath)
	if err != nil {
		return "", err
	}
	uri, err := c.syncFile(ctx, relPath)
	if err != nil {
		return "", err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": max(0, line-1), "character": max(0, col-1)},
	}
	raw, err := c.call(ctx, "textDocument/hover", params)
	if err != nil {
		return "", err
	}
	return parseHover(raw), nil
}

// Diagnostics syncs relPath and returns live diagnostics from the language server.
func (m *Manager) Diagnostics(ctx context.Context, root, relPath string) ([]Diagnostic, error) {
	c, _, err := m.clientFor(ctx, root, relPath)
	if err != nil {
		return nil, err
	}
	uri, changed, ch, err := c.syncFileWait(ctx, relPath)
	if err != nil {
		return nil, err
	}
	if changed {
		waitCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		defer cancel()
		select {
		case <-ch:
		case <-waitCtx.Done():
		}
	}
	return c.diagnosticsFor(uri), nil
}

// Close shuts down all active language servers.
func (m *Manager) Close() {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for k, c := range m.clients {
		clients = append(clients, c)
		delete(m.clients, k)
	}
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

// CloseWorkspace shuts down all language servers for root.
func (m *Manager) CloseWorkspace(root string) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	m.mu.Lock()
	var clients []*Client
	for k, c := range m.clients {
		if k.root == absRoot {
			clients = append(clients, c)
			delete(m.clients, k)
		}
	}
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

type docState struct {
	version int
	hash    [32]byte
}

// Client is a persistent connection to one LSP server process.
type Client struct {
	root string
	spec ServerSpec
	cmd  *exec.Cmd
	in   io.WriteCloser
	wmu  sync.Mutex

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[int64]chan rpcResponse

	dmu   sync.Mutex
	docs  map[string]docState
	diags map[string][]Diagnostic
	diagW map[string]chan struct{}

	done   chan struct{}
	isDead atomic.Bool
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func startClient(ctx context.Context, root string, spec ServerSpec) (*Client, error) {
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{
		root:    root,
		spec:    spec,
		cmd:     cmd,
		in:      stdin,
		pending: make(map[int64]chan rpcResponse),
		docs:    make(map[string]docState),
		diags:   make(map[string][]Diagnostic),
		diagW:   make(map[string]chan struct{}),
		done:    make(chan struct{}),
	}
	go c.readLoop(stdout)
	go func() {
		_ = cmd.Wait()
		c.isDead.Store(true)
		close(c.done)
		c.failPending()
	}()

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rootURI := pathToURI(root)
	initParams := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": filepath.Base(root)},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"publishDiagnostics": map[string]any{"relatedInformation": false},
			},
		},
	}
	if _, err := c.call(initCtx, "initialize", initParams); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("lsp initialize (%s): %w", spec.Language, err)
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) closed() bool {
	return c.isDead.Load()
}

// Close shuts down the LSP server process.
func (c *Client) Close() error {
	if c.isDead.Swap(true) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.call(ctx, "shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.in.Close()
	select {
	case <-c.done:
	case <-time.After(1 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
	return nil
}

func (c *Client) failPending() {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: "language server exited"}}
		delete(c.pending, id)
	}
}

func (c *Client) send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(b))
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := io.WriteString(c.in, header); err != nil {
		return err
	}
	_, err = c.in.Write(b)
	return err
}

func (c *Client) notify(method string, params any) error {
	return c.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed() {
		return nil, errors.New("language server is closed")
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()

	if err := c.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp %s: %s (%d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		contentLen := -1
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					contentLen = n
				}
			}
		}
		if contentLen <= 0 {
			continue
		}
		body := make([]byte, contentLen)
		if _, err := io.ReadFull(br, body); err != nil {
			return
		}
		c.handleMessage(body)
	}
}

func (c *Client) handleMessage(body []byte) {
	var envelope struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return
	}
	// Server-to-client request: reply with null result to keep server unblocked.
	if envelope.ID != nil && envelope.Method != "" {
		_ = c.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      *envelope.ID,
			"result":  nil,
		})
		return
	}
	// Response to our request.
	if envelope.ID != nil {
		c.pmu.Lock()
		ch := c.pending[*envelope.ID]
		delete(c.pending, *envelope.ID)
		c.pmu.Unlock()
		if ch != nil {
			ch <- rpcResponse{Result: envelope.Result, Error: envelope.Error}
		}
		return
	}
	// Notification.
	if envelope.Method == "textDocument/publishDiagnostics" {
		c.handleDiagnostics(envelope.Params)
	}
}

func (c *Client) handleDiagnostics(raw json.RawMessage) {
	var msg struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Source   string `json:"source"`
			Code     any    `json:"code"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.URI == "" {
		return
	}
	rel := uriToRel(c.root, msg.URI)
	out := make([]Diagnostic, 0, len(msg.Diagnostics))
	for _, d := range msg.Diagnostics {
		sev := "info"
		switch d.Severity {
		case 1:
			sev = "error"
		case 2:
			sev = "warning"
		case 3:
			sev = "info"
		case 4:
			sev = "hint"
		}
		var code string
		if d.Code != nil {
			code = fmt.Sprint(d.Code)
		}
		out = append(out, Diagnostic{
			Path:     rel,
			Line:     d.Range.Start.Line + 1,
			Column:   d.Range.Start.Character + 1,
			Severity: sev,
			Source:   d.Source,
			Code:     code,
			Message:  strings.TrimSpace(d.Message),
		})
	}
	c.dmu.Lock()
	c.diags[msg.URI] = out
	if ch := c.diagW[msg.URI]; ch != nil {
		close(ch)
		delete(c.diagW, msg.URI)
	}
	c.dmu.Unlock()
}

func (c *Client) diagnosticsFor(uri string) []Diagnostic {
	c.dmu.Lock()
	defer c.dmu.Unlock()
	src := c.diags[uri]
	out := make([]Diagnostic, len(src))
	copy(out, src)
	return out
}

func (c *Client) syncFile(ctx context.Context, relPath string) (string, error) {
	uri, _, _, err := c.syncFileWait(ctx, relPath)
	return uri, err
}

func (c *Client) syncFileWait(ctx context.Context, relPath string) (uri string, changed bool, waitCh chan struct{}, err error) {
	abs := filepath.Join(c.root, filepath.Clean(relPath))
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false, nil, err
	}
	uri = pathToURI(abs)
	sum := sha256.Sum256(data)

	c.dmu.Lock()
	st, opened := c.docs[uri]
	if opened && st.hash == sum {
		c.dmu.Unlock()
		return uri, false, nil, nil
	}
	ch := make(chan struct{})
	if old := c.diagW[uri]; old != nil {
		close(old)
	}
	c.diagW[uri] = ch
	ver := st.version + 1
	c.docs[uri] = docState{version: ver, hash: sum}
	c.dmu.Unlock()

	if !opened {
		err = c.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": languageIDFor(relPath, c.spec.Language),
				"version":    ver,
				"text":       string(data),
			},
		})
	} else {
		err = c.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": ver},
			"contentChanges": []map[string]any{{"text": string(data)}},
		})
	}
	return uri, true, ch, err
}

func languageIDFor(relPath, fallback string) string {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	default:
		return fallback
	}
}

func pathToURI(abs string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

func uriToRel(root, rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil || u.Scheme != "file" {
		return rawURI
	}
	p := filepath.FromSlash(u.Path)
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return p
}

func parseLocations(root string, raw json.RawMessage) []Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	type lspPos struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	}
	type lspRange struct {
		Start lspPos `json:"start"`
		End   lspPos `json:"end"`
	}
	type lspLoc struct {
		URI       string   `json:"uri"`
		Range     lspRange `json:"range"`
		TargetURI string   `json:"targetUri"`
		TargetSel lspRange `json:"targetSelectionRange"`
		TargetRng lspRange `json:"targetRange"`
	}
	convert := func(l lspLoc) (Location, bool) {
		uri := l.URI
		rng := l.Range
		if uri == "" {
			uri = l.TargetURI
			rng = l.TargetSel
			if rng.Start.Line == 0 && rng.Start.Character == 0 && rng.End.Line == 0 && rng.End.Character == 0 {
				rng = l.TargetRng
			}
		}
		if uri == "" {
			return Location{}, false
		}
		return Location{
			Path:      uriToRel(root, uri),
			Line:      rng.Start.Line + 1,
			Column:    rng.Start.Character + 1,
			EndLine:   rng.End.Line + 1,
			EndColumn: rng.End.Character + 1,
		}, true
	}

	var single lspLoc
	if json.Unmarshal(raw, &single) == nil && (single.URI != "" || single.TargetURI != "") {
		if loc, ok := convert(single); ok {
			return []Location{loc}
		}
	}
	var many []lspLoc
	if json.Unmarshal(raw, &many) == nil {
		out := make([]Location, 0, len(many))
		for _, item := range many {
			if loc, ok := convert(item); ok {
				out = append(out, loc)
			}
		}
		return out
	}
	return nil
}

func parseHover(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(raw, &h) != nil || len(h.Contents) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(h.Contents, &str) == nil {
		return strings.TrimSpace(str)
	}
	var obj struct {
		Kind     string `json:"kind"`
		Value    string `json:"value"`
		Language string `json:"language"`
	}
	if json.Unmarshal(h.Contents, &obj) == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	var arr []json.RawMessage
	if json.Unmarshal(h.Contents, &arr) == nil {
		var parts []string
		for _, item := range arr {
			var s string
			if json.Unmarshal(item, &s) == nil && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
				continue
			}
			var o struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(item, &o) == nil && strings.TrimSpace(o.Value) != "" {
				parts = append(parts, strings.TrimSpace(o.Value))
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}
