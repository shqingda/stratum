package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/talos/kernel/perm"
)

const diagnosticsTimeout = 30 * time.Second

// Diagnostics runs the workspace's fast static/type checker and returns any
// compiler or linter diagnostics.
var Diagnostics = &Tool{
	Name:        "diagnostics",
	Description: "Check the workspace or a path for compile, type, and static analysis diagnostics.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Optional relative file or directory inside the workspace to check"}}}`),
	Access: func(env Env, args json.RawMessage) (perm.Request, error) {
		var a struct {
			Path string `json:"path"`
		}
		if err := decode("diagnostics", args, &a); err != nil {
			return perm.Request{}, err
		}
		if strings.TrimSpace(a.Path) != "" {
			return pathAccess("diagnostics", perm.Read, env, a.Path)
		}
		return perm.Request{Tool: "diagnostics", Kind: perm.Read}, nil
	},
	Run: func(ctx context.Context, env Env, args json.RawMessage) (string, error) {
		var a struct {
			Path string `json:"path"`
		}
		if err := decode("diagnostics", args, &a); err != nil {
			return "", err
		}
		target := strings.TrimSpace(a.Path)
		dir := env.Root
		if target != "" {
			clean := filepath.Clean(target)
			if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
				return "", fmt.Errorf("diagnostics: path %q is outside workspace", target)
			}
			info, err := os.Stat(filepath.Join(env.Root, clean))
			if err != nil {
				return "", fmt.Errorf("diagnostics: %w", err)
			}
			if !info.IsDir() {
				dir = filepath.Dir(filepath.Join(env.Root, clean))
			} else {
				dir = filepath.Join(env.Root, clean)
			}
		}

		cmdName, cmdArgs, workDir, ok := detectDiagnosticCmd(env.Root, dir, target)
		if !ok {
			return "No supported diagnostic checker found in workspace.", nil
		}
		cctx, cancel := context.WithTimeout(ctx, diagnosticsTimeout)
		defer cancel()
		cmd := exec.CommandContext(cctx, cmdName, cmdArgs...)
		cmd.Dir = workDir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		text := strings.TrimSpace(out.String())
		if err == nil && text == "" {
			return "No diagnostics.", nil
		}
		if text == "" && err != nil {
			return "", fmt.Errorf("diagnostics: %w", err)
		}
		lines := strings.Split(text, "\n")
		if len(lines) > 80 {
			text = strings.Join(lines[:80], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-80)
		}
		if len(text) > 8192 {
			text = text[:8192] + "\n... (truncated)"
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return text + "\n(diagnostics timed out)", nil
		}
		return text, nil
	},
}

func detectDiagnosticCmd(root, targetDir, rawTarget string) (string, []string, string, bool) {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		pkg := "./..."
		if rawTarget != "" {
			rel, err := filepath.Rel(root, targetDir)
			if err == nil {
				rel = filepath.ToSlash(rel)
				if rel == "." || rel == "" {
					pkg = "."
				} else {
					pkg = "./" + rel
				}
			}
		}
		return "go", []string{"vet", pkg}, root, true
	}
	if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err == nil {
		if _, err := exec.LookPath("tsc"); err == nil {
			return "tsc", []string{"--noEmit"}, root, true
		}
	}
	if _, err := os.Stat(filepath.Join(root, "Cargo.toml")); err == nil {
		if _, err := exec.LookPath("cargo"); err == nil {
			return "cargo", []string{"check", "--message-format=short"}, root, true
		}
	}
	if strings.HasSuffix(rawTarget, ".py") {
		if _, err := exec.LookPath("python3"); err == nil {
			return "python3", []string{"-m", "py_compile", rawTarget}, root, true
		}
	}
	return "", nil, "", false
}
