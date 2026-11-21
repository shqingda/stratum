package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileTreeExpandCollapseAndPreview(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "calc.go"), []byte("package pkg\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := task8App()
	a.C.Current.Cwd = dir
	a.rv.open = true
	a.rv.tab = "files"
	tt := task8Tester(a, false)

	// Root directory shows pkg/ and README.md, while calc.go is still collapsed inside pkg/.
	if !tt.HasText("pkg") || !tt.HasText("README.md") {
		t.Fatalf("expected root entries pkg and README.md")
	}
	if tt.HasText("calc.go") {
		t.Fatalf("expected calc.go hidden before expanding pkg")
	}

	// Click pkg to expand directory tree.
	if err := tt.Click("pkg"); err != nil {
		t.Fatalf("Click(pkg): %v", err)
	}
	if !tt.HasText("calc.go") {
		t.Fatalf("expected calc.go visible after expanding pkg")
	}

	// Click calc.go to open in-pane syntax-highlighted preview.
	if err := tt.Click("calc.go"); err != nil {
		t.Fatalf("Click(calc.go): %v", err)
	}
	if a.rv.previewPath != filepath.Join("pkg", "calc.go") {
		t.Fatalf("previewPath = %q, want pkg/calc.go", a.rv.previewPath)
	}
	if !tt.HasText("package pkg") {
		t.Fatalf("expected preview content 'package pkg'")
	}

	// Click back button to return to the directory tree.
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("Click(关闭): %v", err)
	}
	if a.rv.previewPath != "" {
		t.Fatalf("expected previewPath cleared after back, got %q", a.rv.previewPath)
	}
	if !tt.HasText("calc.go") {
		t.Fatalf("expected tree restored with calc.go visible")
	}
}

func TestMaximizePaneAndSideBySideTree(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "calc.go"), []byte("package pkg\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := task8App()
	a.C.Current.Cwd = dir
	a.rv.open = true
	a.rv.tab = "files"
	tt := task8Tester(a, false)

	// Maximize the review pane via the top-right button.
	if err := tt.Click("最大化面板"); err != nil {
		t.Fatalf("Click(最大化面板): %v", err)
	}
	if !a.rv.maximized {
		t.Fatal("expected rv.maximized = true")
	}
	// Empty preview prompt and Workspace tree are both visible side-by-side.
	if !tt.HasText("选择文件以预览代码") || !tt.HasText("pkg") {
		t.Fatal("expected split preview placeholder and workspace tree side-by-side")
	}
	if err := tt.Click("pkg"); err != nil {
		t.Fatalf("Click(pkg): %v", err)
	}
	if err := tt.Click("calc.go"); err != nil {
		t.Fatalf("Click(calc.go): %v", err)
	}
	// In side-by-side mode, both code preview AND the tree remain visible.
	if !tt.HasText("package pkg") || !tt.HasText("pkg") {
		t.Fatal("expected code preview and workspace tree visible simultaneously")
	}
	// Restore pane.
	if err := tt.Click("还原面板"); err != nil {
		t.Fatalf("Click(还原面板): %v", err)
	}
	if a.rv.maximized {
		t.Fatal("expected rv.maximized = false after restore")
	}
}

func TestMarkdownDefaultPreviewAndRawToggle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Title\n\nHello **world**\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := task8App()
	a.C.Current.Cwd = dir
	a.rv.open = true
	a.rv.tab = "files"
	tt := task8Tester(a, false)

	if err := tt.Click("README.md"); err != nil {
		t.Fatalf("Click(README.md): %v", err)
	}
	if a.rv.previewRaw {
		t.Fatal("expected markdown to open in rendered Preview by default")
	}
	// Rendered markdown shows "Title" without raw "# ".
	if !tt.HasText("Title") || tt.HasText("# Title") {
		t.Fatalf("expected rendered markdown heading without '# ', got texts=%v", tt.Texts())
	}
	// Toggle to Raw source.
	if err := tt.Click("源码"); err != nil {
		t.Fatalf("Click(源码): %v", err)
	}
	if !a.rv.previewRaw || !tt.HasText("# Title") {
		t.Fatal("expected raw markdown source '# Title' after clicking 源码")
	}
}
