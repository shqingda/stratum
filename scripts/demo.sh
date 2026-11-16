#!/bin/sh
# Runs Talos in the demo environment: a development build with live reload,
# one scripted account (llm.Play) and a data directory of its own, so no
# real account, ~/.talos or the keychain is touched.
#
#   scripts/demo.sh [folder]
#
# The folder (default: a new temporary one) gets three repositories: calc,
# whose test fails because Add subtracts, and site and notes, copies of it
# that fill the sidebar; the scripted answers are about this code, so they
# hold in any of the three. What a message to the demo account does depends
# on its words:
#
#   修 / fix          read, test, edit, test again (the default)
#   跑测试 / test      run the tests and report the failure
#   问 / ask          ask a question with options
#   依赖 / deps        a command outside the sandbox, which asks first
#   子任务 / task      two sub-agents at once
#   计划 / plan        keep a plan
#   长 / long         a long answer with code and a table
#   出错 / error      fail mid-turn
set -e
project=$(cd "$(dirname "$0")/.." && pwd)
root=${1:-$(mktemp -d -t talos-demo)}
mkdir -p "$root"
root=$(cd "$root" && pwd)

repo() {
	mkdir -p "$root/$1"
	cd "$root/$1"
	[ -d .git ] && return 1
	git init -q
}

if repo calc; then
	cat >go.mod <<'EOF'
module calc

go 1.22
EOF
	cat >add.go <<'EOF'
// Package calc does arithmetic.
package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a - b
}
EOF
	cat >add_test.go <<'EOF'
package calc

import "testing"

func TestAdd(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{
		{2, 3, 5},
		{-1, 1, 0},
		{0, 0, 0},
	} {
		if got := Add(c.a, c.b); got != c.want {
			t.Errorf("Add(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
EOF
	git add -A && git -c user.name=demo -c user.email=demo@example.test commit -qm "calc"
fi
for name in site notes; do
	if repo $name; then
		cp "$root/calc/go.mod" "$root/calc/add.go" "$root/calc/add_test.go" .
		git add -A && git -c user.name=demo -c user.email=demo@example.test commit -qm "$name"
	fi
done

echo "demo folder: $root"
cd "$project"
TALOS_DEMO=$root exec go tool mygo dev
