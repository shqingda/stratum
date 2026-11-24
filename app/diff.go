package app

import (
	"fmt"
	"math"
	"slices"
)

// A line diff for whole files, as the review panel shows them: Myers'
// O(ND) algorithm, fast on long files with small edits, cut into hunks
// with context around them.

// edit is one line of a diff: kept (' '), removed ('-') or added ('+');
// a and b are its indexes in the old and the new lines, -1 where it has
// none.
type edit struct {
	op   byte
	a, b int
}

// maxEdits bounds the work: a diff of more edits than this shows every
// old line removed and every new one added.
const maxEdits = 4000

// myers returns the edits that turn a into b, and false when they were
// too many to search for and are a plain replacement.
func myers(a, b []string) ([]edit, bool) {
	// Equal lines at both ends need no search.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	out := make([]edit, 0, pre+suf)
	for i := 0; i < pre; i++ {
		out = append(out, edit{' ', i, i})
	}
	mid, ok := middle(a[pre:len(a)-suf], b[pre:len(b)-suf], pre)
	out = append(out, mid...)
	for i := 0; i < suf; i++ {
		out = append(out, edit{' ', len(a) - suf + i, len(b) - suf + i})
	}
	return out, ok
}

const (
	traceChunkShift = 14 // 16,384 int32s (64 KiB) per chunk to avoid slice-growth copies
	traceChunkSize  = 1 << traceChunkShift
	traceChunkMask  = traceChunkSize - 1
)

// middle diffs a and b, whose first lines are line off of the files.
func middle(a, b []string, off int) ([]edit, bool) {
	n, m := len(a), len(b)
	replace := func() []edit {
		out := make([]edit, 0, n+m)
		for i := range a {
			out = append(out, edit{'-', off + i, -1})
		}
		for j := range b {
			out = append(out, edit{'+', -1, off + j})
		}
		return out
	}
	if n == 0 || m == 0 {
		return replace(), true
	}
	if n > math.MaxInt32 || m > math.MaxInt32 {
		return replace(), false
	}
	limit := min(n+m, maxEdits)
	v := make([]int32, 2*limit+2)
	// Store only the d active entries v[-(d-1), ..., d-1] before step d >= 1
	// across fixed chunks: step d starts at logical index d*(d-1)/2.
	var trace [][]int32
	total := 0
	at := func(k int) int { return k + limit + 1 }
	for d := 0; d <= limit; d++ {
		if d > 0 {
			for k := -(d - 1); k <= d-1; k += 2 {
				ci := total >> traceChunkShift
				if ci >= len(trace) {
					trace = append(trace, make([]int32, traceChunkSize))
				}
				trace[ci][total&traceChunkMask] = v[at(k)]
				total++
			}
		}
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || k != d && v[at(k-1)] < v[at(k+1)] {
				x = int(v[at(k+1)]) // down: an insertion
			} else {
				x = int(v[at(k-1)]) + 1 // right: a deletion
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[at(k)] = int32(x)
			if x >= n && y >= m {
				return backtrack(trace, d, n, m, off), true
			}
		}
	}
	return replace(), false
}

// backtrack walks the chunked frontiers from the end back to the start.
func backtrack(trace [][]int32, lastD, n, m, off int) []edit {
	rev := make([]edit, 0, max(n, m)+lastD)
	x, y := n, m
	for d := lastD; d >= 1; d-- {
		base := d * (d - 1) / 2
		prev := func(pk int) int {
			idx := base + (pk+d-1)/2
			return int(trace[idx>>traceChunkShift][idx&traceChunkMask])
		}
		k := x - y
		var pk int
		if k == -d || k != d && prev(k-1) < prev(k+1) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := prev(pk)
		py := px - pk
		for x > px && y > py {
			x, y = x-1, y-1
			rev = append(rev, edit{' ', off + x, off + y})
		}
		if x == px {
			y--
			rev = append(rev, edit{'+', -1, off + y})
		} else {
			x--
			rev = append(rev, edit{'-', off + x, -1})
		}
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		rev = append(rev, edit{' ', off + x, off + y})
	}
	slices.Reverse(rev)
	return rev
}

// hunk is a run of edits with the kept lines around them.
type hunk struct {
	oldStart, oldLen int // 1-based, as in "@@ -l,s +l,s @@"
	newStart, newLen int
	edits            []edit
}

// head is the hunk's "@@ -l,s +l,s @@" line.
func (h hunk) head() string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.oldStart, h.oldLen, h.newStart, h.newLen)
}

// hunks cuts edits into hunks with ctx kept lines on each side, joining
// those whose context touches.
func hunks(edits []edit, ctx int) []hunk {
	var out []hunk
	last := -1 // index of the last changed edit taken
	for i, e := range edits {
		if e.op == ' ' {
			continue
		}
		from := max(i-ctx, 0)
		if len(out) > 0 && from <= last+ctx+1 {
			h := &out[len(out)-1]
			h.edits = append(h.edits, edits[last+1:i+1]...)
		} else {
			if len(out) > 0 {
				h := &out[len(out)-1]
				h.edits = append(h.edits, edits[last+1:min(last+1+ctx, len(edits))]...)
			}
			out = append(out, hunk{edits: append([]edit(nil), edits[from:i+1]...)})
		}
		last = i
	}
	if len(out) > 0 {
		h := &out[len(out)-1]
		h.edits = append(h.edits, edits[last+1:min(last+1+ctx, len(edits))]...)
	}
	for i := range out {
		h := &out[i]
		for _, e := range h.edits {
			if e.a >= 0 {
				if h.oldLen == 0 {
					h.oldStart = e.a + 1
				}
				h.oldLen++
			}
			if e.b >= 0 {
				if h.newLen == 0 {
					h.newStart = e.b + 1
				}
				h.newLen++
			}
		}
	}
	return out
}
