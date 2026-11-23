package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shqingda/stratum/kernel/perm"
	"github.com/shqingda/stratum/kernel/preview"
)

func TestBrowserUploadAndDragTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: root}

	// Missing paths must fail Access.
	if _, err := Browser.Access(env, json.RawMessage(`{"action":"upload","ref":1}`)); err == nil {
		t.Fatal("expected error for upload without paths")
	}
	// Empty path must fail Access.
	if _, err := Browser.Access(env, json.RawMessage(`{"action":"upload","ref":1,"paths":[""]}`)); err == nil {
		t.Fatal("expected error for upload with empty path")
	}
	// Path outside workspace must be marked Inside == false for perm evaluation.
	outReq, err := Browser.Access(env, json.RawMessage(`{"action":"upload","ref":1,"paths":["../secret"]}`))
	if err != nil {
		t.Fatalf("upload Access outside root: %v", err)
	}
	if len(outReq.Paths) != 1 || outReq.Paths[0].Inside {
		t.Fatalf("expected Inside=false for ../secret, got %+v", outReq.Paths)
	}

	req, err := Browser.Access(env, json.RawMessage(`{"action":"upload","ref":1,"paths":["note.txt"]}`))
	if err != nil {
		t.Fatalf("upload Access: %v", err)
	}
	if len(req.Paths) != 1 || req.Paths[0].Rel != "note.txt" {
		t.Fatalf("upload Access paths = %+v", req.Paths)
	}
	if req.Kind != perm.Read {
		t.Fatalf("upload Access kind on local/empty page = %v, want Read", req.Kind)
	}

	files, err := loadUploadFiles(root, []string{"note.txt"})
	if err != nil {
		t.Fatalf("loadUploadFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "note.txt" || files[0].Base64 != "aGVsbG8=" || !strings.HasPrefix(files[0].MIME, "text/plain") {
		t.Fatalf("unexpected upload files: %+v", files)
	}

	// drag without driver returns ErrNoDriver.
	if _, err := Browser.Run(context.Background(), env, json.RawMessage(`{"action":"drag","ref":1,"to_ref":2}`)); err != preview.ErrNoDriver {
		t.Fatalf("expected ErrNoDriver, got %v", err)
	}
}
