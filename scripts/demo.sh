#!/bin/sh
# Runs Stratum in the demo environment: a fast direct build (by default) or a
# MyGo dev build with live reload (--dev), one scripted account (llm.Play) and
# a data directory of its own, so no real account, ~/.stratum or the keychain
# is touched.
#
#   scripts/demo.sh [--dev] [--adhoc] [--fast] [--fresh] [folder]
#
# Options:
#   --dev     Run via go tool mygo dev (.app bundle with live reload and signing).
#   --adhoc   Run via go tool mygo dev -sign - (ad-hoc sign, keeps live reload).
#   --fast    Default: run directly with CGO_ENABLED=0 go run ./cmd/stratum
#             (skips .app bundling and codesign) and reuse
#             ${TMPDIR:-/tmp}/stratum-demo unless [folder] or --fresh is given.
#   --fresh   Create a new temporary folder via mktemp -d -t stratum-demo
#             even in fast mode.
#   -h,--help Show this usage message and exit.
#
# The folder (default: reused under fast mode, or a new temporary one with
# --dev/--adhoc/--fresh) gets three repositories: calc, whose test fails because
# Add subtracts, and site and notes, copies of it that fill the sidebar; the
# scripted answers are about this code, so they hold in any of the three. What a
# message to the demo account does depends on its words:
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

usage() {
	cat <<'EOF'
Usage: scripts/demo.sh [--dev] [--adhoc] [--fast] [--fresh] [folder]

Options:
  --dev        Run via go tool mygo dev (.app bundle, live reload, normal signing).
  --adhoc      Run via go tool mygo dev -sign - (ad-hoc sign, keeps live reload).
  --fast       Default: run via CGO_ENABLED=0 go run ./cmd/stratum (skips .app
               bundle and codesign); defaults folder to ${TMPDIR:-/tmp}/stratum-demo.
  --fresh      Force a new mktemp -d -t stratum-demo folder even in fast mode.
  -h, --help   Show this help message and exit.
EOF
}

fast=1
adhoc=0
fresh=0

while [ $# -gt 0 ]; do
	case "$1" in
	--dev)
		fast=0
		adhoc=0
		shift
		;;
	--fast)
		fast=1
		shift
		;;
	--adhoc)
		fast=0
		adhoc=1
		shift
		;;
	--fresh)
		fresh=1
		shift
		;;
	-h|--help)
		usage
		exit 0
		;;
	--)
		shift
		break
		;;
	-*)
		echo "scripts/demo.sh: unknown option: $1" >&2
		usage >&2
		exit 1
		;;
	*)
		break
		;;
	esac
done

if [ $# -gt 1 ]; then
	echo "scripts/demo.sh: too many arguments" >&2
	usage >&2
	exit 1
fi

project=$(cd "$(dirname "$0")/.." && pwd)
if [ $# -eq 1 ]; then
	root=$1
elif [ "$fast" = 1 ] && [ "$fresh" = 0 ]; then
	tmp=${TMPDIR:-/tmp}
	root="${tmp%/}/stratum-demo"
else
	root=$(mktemp -d -t stratum-demo)
fi
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
	mkdir -p docs internal/ops
	cat >docs/notes.md <<'EOF'
# Calc Notes

Sample nested documentation for demo directory tree.
EOF
	cat >internal/ops/sub.go <<'EOF'
package ops

// Sub returns a - b.
func Sub(a, b int) int {
	return a - b
}
EOF
	git add -A && git -c user.name=demo -c user.email=demo@example.test commit -qm "calc"
fi
for name in site notes; do
	if repo $name; then
		cp -R "$root/calc/go.mod" "$root/calc/add.go" "$root/calc/add_test.go" "$root/calc/docs" "$root/calc/internal" .
		git add -A && git -c user.name=demo -c user.email=demo@example.test commit -qm "$name"
	fi
done

echo "demo folder: $root"
cd "$project"
if [ "$fast" = 1 ]; then
	STRATUM_DEMO="$root" CGO_ENABLED=0 exec go run ./cmd/stratum
elif [ "$adhoc" = 1 ]; then
	STRATUM_DEMO="$root" exec go tool mygo dev -sign -
else
	STRATUM_DEMO="$root" exec go tool mygo dev
fi
