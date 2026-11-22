package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/shqingda/stratum/app/web"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/proto"
)

func TestWebAssetsEmbedded(t *testing.T) {
	assets := web.Assets()
	for _, name := range []string{"index.html", "bundle.js", "style.css"} {
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestPierrePayloadsAndBridge(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := New(client.New(dir))
	a.C.Current = &proto.Session{ID: "s1", Cwd: dir}
	a.p = PaletteFor(true)

	// Theme payload
	th := a.buildPierreTheme()
	if !th.Dark || th.Bg == "" || th.Fg == "" {
		t.Fatalf("unexpected theme payload: %+v", th)
	}

	// Diffs payload
	before, after := "a\n", "b\n"
	filePath := filepath.Join(dir, "README.md")
	a.C.Changes.Diffs = map[string]proto.ChangeDiff{
		client.DiffKey(1, filePath): {Path: filePath, Before: &before, After: &after},
	}
	diffs := a.buildPierreDiffs([]proto.ChangeTurn{{
		TurnSeq: 1,
		Files:   []proto.ChangeFile{{Path: filePath, Added: 1, Removed: 1}},
	}})
	if len(diffs.Files) != 1 || diffs.Files[0].RelPath != "README.md" || diffs.Files[0].Hash == "" {
		t.Fatalf("unexpected diffs payload: %+v", diffs)
	}

	// Tree payload
	a.C.Workspace.Files = []proto.ChangeFile{{Path: filePath}}
	tree := a.buildPierreTree(dir)
	if len(tree.Paths) < 3 || len(tree.GitStatus) != 1 || tree.GitStatus[0].Path != "README.md" {
		t.Fatalf("unexpected tree payload: %+v", tree)
	}

	// Bridge actions
	b := a.PierreBridge()
	b.SelectFile("README.md")
	if a.rv.previewPath != "README.md" || len(a.pierre.openTabs) != 1 {
		t.Fatalf("SelectFile failed: path=%q tabs=%v", a.rv.previewPath, a.pierre.openTabs)
	}
	fp := a.buildPierreFile(dir)
	if !fp.IsMarkdown || fp.Raw || fp.Content != "# Hello\n" {
		t.Fatalf("unexpected file payload: %+v", fp)
	}

	b.ToggleMarkdownRaw(true)
	if !a.rv.previewRaw {
		t.Fatal("ToggleMarkdownRaw(true) did not set previewRaw")
	}

	b.SelectFile("docs/note.txt")
	if len(a.pierre.openTabs) != 2 || a.rv.previewPath != "docs/note.txt" {
		t.Fatalf("second SelectFile failed: path=%q tabs=%v", a.rv.previewPath, a.pierre.openTabs)
	}
	b.CloseTab("docs/note.txt")
	if a.rv.previewPath != "README.md" || len(a.pierre.openTabs) != 1 {
		t.Fatalf("CloseTab failed: path=%q tabs=%v", a.rv.previewPath, a.pierre.openTabs)
	}

	b.ToggleTree()
	if !a.rv.treeHidden {
		t.Fatal("ToggleTree failed")
	}

	b.ToggleDiffStyle()
	if a.pierre.diffStyle != "split" {
		t.Fatalf("diffStyle = %q, want split", a.pierre.diffStyle)
	}

	b.AddRemark("README.md", 3, "new", "looks good")
	if len(a.rv.remarks) != 1 || a.rv.remarks[0].line != 3 || a.rv.remarks[0].old {
		t.Fatalf("AddRemark failed: %+v", a.rv.remarks)
	}

	b.EscapeToComposer()
	if !a.pierre.focusComposerNext {
		t.Fatal("EscapeToComposer failed")
	}
}
