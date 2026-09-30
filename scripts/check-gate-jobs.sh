#!/bin/sh
# Hold the Makefile to its promise that the gate (make test, make lint) uses the
# cores a host has: GOFLAGS -p and GOMAXPROCS follow JOBS, which defaults to
# nproc, and JOBS=1 pins both to one (the old hard-coded setting, for a small
# box). Reads only the Makefile; `make -pn` prints its variables and runs nothing.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$REPO_ROOT"

fail() {
	echo "check-gate-jobs: $*" >&2
	exit 1
}

# var <name> [make args...]: the value make settles on for <name>.
var() {
	name=$1
	shift
	make -pn "$@" test 2>/dev/null | sed -n "s/^$name :\{0,1\}= *//p" | tail -n 1
}

cores=$(nproc)

[ "$(var GOFLAGS)" = "-p=$cores" ] || fail "GOFLAGS is '$(var GOFLAGS)', want -p=$cores (nproc)"
[ "$(var GOMAXPROCS)" = "$cores" ] || fail "GOMAXPROCS is '$(var GOMAXPROCS)', want $cores (nproc)"
[ "$(var GOFLAGS JOBS=1)" = "-p=1" ] || fail "JOBS=1 does not give GOFLAGS -p=1"
[ "$(var GOMAXPROCS JOBS=1)" = "1" ] || fail "JOBS=1 does not give GOMAXPROCS 1"
[ "$(var GOFLAGS JOBS=3)" = "-p=3" ] || fail "JOBS=3 does not give GOFLAGS -p=3"

echo "check-gate-jobs: ok"
