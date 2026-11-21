package app

import (
	"testing"

	"github.com/shqingda/talos/proto"
)

// BenchmarkNewFileDiff is a whole-file diff of 20,000 lines, as opening a
// file in the Review panel works it out.
func BenchmarkNewFileDiff(b *testing.B) {
	before, after := longText(20000, "Old"), longText(20000, "New")
	d := proto.ChangeDiff{Path: "big.go", Before: &before, After: &after}
	b.ReportAllocs()
	for b.Loop() {
		newFileDiff(d)
	}
}
