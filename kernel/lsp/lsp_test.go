package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("TALOS_FAKE_LSP") == "1" {
		runFakeLSP()
		return
	}
	os.Exit(m.Run())
}

func runFakeLSP() {
	br := bufio.NewReader(os.Stdin)
	writeMsg := func(msg any) {
		b, _ := json.Marshal(msg)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
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
				contentLen, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		if contentLen <= 0 {
			continue
		}
		body := make([]byte, contentLen)
		if _, err := io.ReadFull(br, body); err != nil {
			return
		}
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			writeMsg(map[string]any{
				"jsonrpc": "2.0",
				"id":      *req.ID,
				"result": map[string]any{
					"capabilities": map[string]any{
						"hoverProvider":      true,
						"definitionProvider": true,
						"referencesProvider": true,
					},
				},
			})
		case "textDocument/didOpen", "textDocument/didChange":
			var p struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			writeMsg(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri": p.TextDocument.URI,
					"diagnostics": []map[string]any{{
						"range": map[string]any{
							"start": map[string]any{"line": 2, "character": 4},
							"end":   map[string]any{"line": 2, "character": 10},
						},
						"severity": 1,
						"source":   "fake-lsp",
						"message":  fmt.Sprintf("unused symbol (v%d)", p.TextDocument.Version),
					}},
				},
			})
		case "textDocument/definition":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			writeMsg(map[string]any{
				"jsonrpc": "2.0",
				"id":      *req.ID,
				"result": []map[string]any{{
					"uri": p.TextDocument.URI,
					"range": map[string]any{
						"start": map[string]any{"line": 0, "character": 5},
						"end":   map[string]any{"line": 0, "character": 9},
					},
				}},
			})
		case "textDocument/references":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			writeMsg(map[string]any{
				"jsonrpc": "2.0",
				"id":      *req.ID,
				"result": []map[string]any{
					{
						"uri": p.TextDocument.URI,
						"range": map[string]any{
							"start": map[string]any{"line": 0, "character": 5},
							"end":   map[string]any{"line": 0, "character": 9},
						},
					},
					{
						"uri": p.TextDocument.URI,
						"range": map[string]any{
							"start": map[string]any{"line": 3, "character": 1},
							"end":   map[string]any{"line": 3, "character": 5},
						},
					},
				},
			})
		case "textDocument/hover":
			writeMsg(map[string]any{
				"jsonrpc": "2.0",
				"id":      *req.ID,
				"result": map[string]any{
					"contents": map[string]any{
						"kind":  "markdown",
						"value": "```go\nfunc main()\n```",
					},
				},
			})
		case "shutdown":
			writeMsg(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": nil})
		case "exit":
			return
		}
	}
}

func TestLSPPersistentLifecycle(t *testing.T) {
	t.Setenv("TALOS_FAKE_LSP", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {\n\tprintln(1)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	mgr.Lookup = func(_, rel string) (ServerSpec, bool) {
		return ServerSpec{Language: "go", Command: exe}, true
	}
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	locs, err := mgr.Definition(ctx, root, "main.go", 4, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Path != "main.go" || locs[0].Line != 1 || locs[0].Column != 6 {
		t.Fatalf("unexpected definition: %+v", locs)
	}

	refs, err := mgr.References(ctx, root, "main.go", 1, 6, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %+v", refs)
	}

	hover, err := mgr.Hover(ctx, root, "main.go", 1, 6)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if !strings.Contains(hover, "func main()") {
		t.Fatalf("unexpected hover: %q", hover)
	}

	diags, err := mgr.Diagnostics(ctx, root, "main.go")
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diags) != 1 || diags[0].Severity != "error" || !strings.Contains(diags[0].Message, "(v1)") {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	// Modify file and verify persistent server receives didChange (v2) without restarting.
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {\n\tprintln(2)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diags2, err := mgr.Diagnostics(ctx, root, "main.go")
	if err != nil {
		t.Fatalf("Diagnostics after edit: %v", err)
	}
	if len(diags2) != 1 || !strings.Contains(diags2[0].Message, "(v2)") {
		t.Fatalf("expected v2 diagnostic after didChange, got %+v", diags2)
	}
}
