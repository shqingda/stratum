package app

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestArchBoundary(t *testing.T) {
	fset := token.NewFileSet()

	parseImports := func(dir string) map[string][]string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%q): %v", dir, err)
		}
		out := make(map[string][]string)
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", path, err)
			}
			var imps []string
			for _, spec := range f.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("Unquote(%q) in %s: %v", spec.Path.Value, path, err)
				}
				imps = append(imps, imp)
			}
			out[path] = imps
		}
		if len(out) == 0 {
			t.Fatalf("no non-test Go files found in %q", dir)
		}
		return out
	}

	matchesPkgOrSub := func(imp, root string) bool {
		return imp == root || strings.HasPrefix(imp, root+"/")
	}

	for file, imps := range parseImports(filepath.Join("..", "client")) {
		for _, imp := range imps {
			if matchesPkgOrSub(imp, "github.com/shqingda/stratum/app") ||
				matchesPkgOrSub(imp, "github.com/shqingda/stratum/kernel") ||
				matchesPkgOrSub(imp, "github.com/egoist/mygo") {
				t.Errorf("%s imports forbidden package %q", file, imp)
			}
		}
	}

	for file, imps := range parseImports(".") {
		for _, imp := range imps {
			if matchesPkgOrSub(imp, "github.com/shqingda/stratum/kernel") && imp != "github.com/shqingda/stratum/kernel/rpc" {
				t.Errorf("%s imports forbidden kernel package %q", file, imp)
			}
		}
	}
}
