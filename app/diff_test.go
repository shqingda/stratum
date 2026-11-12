package app

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// lcsLen is the length of the longest common subsequence, by table.
func lcsLen(a, b []string) int {
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = 1 + t[i+1][j+1]
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	return t[0][0]
}

func check(t *testing.T, a, b []string) []edit {
	t.Helper()
	edits, ok := myers(a, b)
	if !ok {
		t.Fatalf("gave up on %d+%d lines", len(a), len(b))
	}
	var olds, news []string
	kept := 0
	for _, e := range edits {
		switch e.op {
		case ' ':
			if a[e.a] != b[e.b] {
				t.Fatalf("kept %q as %q", a[e.a], b[e.b])
			}
			olds, news = append(olds, a[e.a]), append(news, b[e.b])
			kept++
		case '-':
			olds = append(olds, a[e.a])
		case '+':
			news = append(news, b[e.b])
		}
	}
	if strings.Join(olds, "\n") != strings.Join(a, "\n") || strings.Join(news, "\n") != strings.Join(b, "\n") {
		t.Fatalf("edits do not rebuild the texts:\n%q\n%q\n%+v", a, b, edits)
	}
	if want := lcsLen(a, b); kept != want {
		t.Fatalf("kept %d lines, the most is %d: %q %q", kept, want, a, b)
	}
	return edits
}

func TestMyers(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"", ""}, {"", "a b"}, {"a b", ""}, {"a b c", "a b c"}, {"a b c", "a x c"},
		{"a b c", "x b c"}, {"a b c", "a b x"}, {"a a a", "a a"}, {"a b a b", "b a b a"},
		{"a b c d e f", "a c e f g"},
	} {
		check(t, strings.Fields(tc.a), strings.Fields(tc.b))
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		gen := func() []string {
			s := make([]string, r.IntN(14))
			for i := range s {
				s[i] = string(rune('a' + r.IntN(4)))
			}
			return s
		}
		check(t, gen(), gen())
	}
}

func TestMyersLongFileSmallEdit(t *testing.T) {
	a := make([]string, 50000)
	for i := range a {
		a[i] = strings.Repeat("x", i%7) + string(rune('0'+i%10))
	}
	b := append([]string(nil), a...)
	b[100], b[30000] = "changed", "changed too"
	b = append(b[:40000], append([]string{"new"}, b[40000:]...)...)
	edits, ok := myers(a, b)
	if !ok {
		t.Fatal("gave up on a small edit")
	}
	hs := hunks(edits, 3)
	if len(hs) != 3 {
		t.Fatalf("%d hunks", len(hs))
	}
	if h := hs[0]; h.oldStart != 98 || h.oldLen != 7 || h.newStart != 98 || h.newLen != 7 {
		t.Errorf("first hunk %+v", h)
	}
	if h := hs[2]; h.oldLen != 6 || h.newLen != 7 {
		t.Errorf("insertion hunk %+v", h)
	}
}

func TestMyersGivesUp(t *testing.T) {
	a, b := make([]string, 6000), make([]string, 6000)
	for i := range a {
		a[i], b[i] = "a"+string(rune(i)), "b"+string(rune(i))
	}
	edits, ok := myers(a, b)
	if ok || len(edits) != 12000 {
		t.Fatalf("ok %v, %d edits", ok, len(edits))
	}
}

func TestHunksJoin(t *testing.T) {
	a := strings.Fields("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20")
	b := append([]string(nil), a...)
	b[2], b[8] = "x", "y" // 5 kept lines apart: one hunk at ctx 3
	b[18] = "z"           // 9 apart: its own hunk
	edits := check(t, a, b)
	hs := hunks(edits, 3)
	if len(hs) != 2 {
		t.Fatalf("%d hunks: %+v", len(hs), hs)
	}
	if h := hs[0]; h.oldStart != 1 || h.oldLen != 12 {
		t.Errorf("%+v", h)
	}
	if h := hs[1]; h.oldStart != 16 || h.oldLen != 5 || h.newLen != 5 {
		t.Errorf("%+v", h)
	}
}
