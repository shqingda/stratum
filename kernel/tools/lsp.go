package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shqingda/talos/kernel/lsp"
	"github.com/shqingda/talos/kernel/perm"
)

// LSP queries a persistent language server (gopls, typescript-language-server,
// rust-analyzer, pyright/pylsp) for definition, references, hover, and live diagnostics.
var LSP = &Tool{
	Name:        "lsp",
	Description: "Query a persistent Language Server (LSP) for definition, references, hover type/docs, or live diagnostics on a file.",
	Schema: json.RawMessage(`{
		"type": "object",
		"required": ["operation", "path"],
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["definition", "references", "hover", "diagnostics"],
				"description": "LSP operation to run"
			},
			"path": {
				"type": "string",
				"description": "Relative path to the file inside the workspace"
			},
			"line": {
				"type": "integer",
				"description": "1-based line number (required for definition, references, hover)"
			},
			"column": {
				"type": "integer",
				"description": "1-based column number (defaults to 1)"
			},
			"include_declaration": {
				"type": "boolean",
				"description": "Include declaration in references (default true)"
			}
		}
	}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct {
			Operation string `json:"operation"`
			Path      string `json:"path"`
		}
		if err := decode("lsp", args, &a); err != nil {
			return perm.Request{}, err
		}
		switch a.Operation {
		case "definition", "references", "hover", "diagnostics":
		default:
			return perm.Request{}, fmt.Errorf("lsp: unsupported operation %q", a.Operation)
		}
		return pathAccess("lsp", perm.Read, env, a.Path)
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		return runLSP(ctx, lsp.Default, env, args)
	},
}

func runLSP(ctx context.Context, mgr *lsp.Manager, env Env, args json.RawMessage) (string, error) {
	var a struct {
		Operation          string `json:"operation"`
		Path               string `json:"path"`
		Line               int    `json:"line"`
		Column             int    `json:"column"`
		IncludeDeclaration *bool  `json:"include_declaration"`
	}
	if err := decode("lsp", args, &a); err != nil {
		return "", err
	}
	col := a.Column
	if col <= 0 {
		col = 1
	}
	switch a.Operation {
	case "definition":
		if a.Line <= 0 {
			return "", fmt.Errorf("lsp: line (>= 1) is required for definition")
		}
		locs, err := mgr.Definition(ctx, env.Root, a.Path, a.Line, col)
		if err != nil {
			return "", err
		}
		if len(locs) == 0 {
			return "No definition found.", nil
		}
		return formatLocations(locs), nil
	case "references":
		if a.Line <= 0 {
			return "", fmt.Errorf("lsp: line (>= 1) is required for references")
		}
		incl := true
		if a.IncludeDeclaration != nil {
			incl = *a.IncludeDeclaration
		}
		locs, err := mgr.References(ctx, env.Root, a.Path, a.Line, col, incl)
		if err != nil {
			return "", err
		}
		if len(locs) == 0 {
			return "No references found.", nil
		}
		return formatLocations(locs), nil
	case "hover":
		if a.Line <= 0 {
			return "", fmt.Errorf("lsp: line (>= 1) is required for hover")
		}
		text, err := mgr.Hover(ctx, env.Root, a.Path, a.Line, col)
		if err != nil {
			return "", err
		}
		if text == "" {
			return "No hover information.", nil
		}
		return text, nil
	case "diagnostics":
		diags, err := mgr.Diagnostics(ctx, env.Root, a.Path)
		if err != nil {
			return "", err
		}
		if len(diags) == 0 {
			return "No diagnostics.", nil
		}
		var b strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&b, "%s:%d:%d [%s]", d.Path, d.Line, d.Column, d.Severity)
			if d.Source != "" {
				fmt.Fprintf(&b, " (%s)", d.Source)
			}
			fmt.Fprintf(&b, " %s\n", d.Message)
		}
		return strings.TrimSpace(b.String()), nil
	default:
		return "", fmt.Errorf("lsp: unsupported operation %q", a.Operation)
	}
}

func formatLocations(locs []lsp.Location) string {
	var b strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&b, "%s:%d:%d\n", l.Path, l.Line, l.Column)
	}
	return strings.TrimSpace(b.String())
}
