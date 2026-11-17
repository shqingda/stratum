package rpc

import (
	"strings"
	"testing"
)

func TestWithDirs(t *testing.T) {
	got := withDirs([]string{"a/b/c.go", "a/d.go", "e.go"})
	want := []string{"a/b/", "a/", "a/b/c.go", "a/d.go", "e.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", got)
	}
}
