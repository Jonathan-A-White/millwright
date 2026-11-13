#!/bin/sh
# Hold that no function in the tree is unreachable: from a main package or from
# any test, by golang.org/x/tools/cmd/deadcode (a tool in go.mod). It prints
# one line for each function nothing can call, and when it prints any, this
# script prints them and exits 1: remove the function, or, if only a test
# helper's own test used it, the helper too.
#
# Reads only the repository this script is in, builds with the same tags as
# make test (TAGS, default beads_integration) and writes nothing.

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GO=${GO:-go}
TAGS=${TAGS:-beads_integration}

if ! command -v "$GO" >/dev/null 2>&1; then
	if [ -x /usr/local/go/bin/go ]; then
		GO=/usr/local/go/bin/go
	else
		echo "check-deadcode: go not found on PATH" >&2
		exit 2
	fi
fi

cd "$ROOT"
errs=$(mktemp)
trap 'rm -f "$errs"' EXIT
if ! listed=$("$GO" run golang.org/x/tools/cmd/deadcode -test -tags "$TAGS" ./... 2>"$errs"); then
	echo "check-deadcode: deadcode did not run:" >&2
	cat "$errs" >&2
	exit 2
fi
if [ -n "$listed" ]; then
	echo "check-deadcode: these functions are unreachable from main and from every test; remove them:" >&2
	echo "$listed" >&2
	exit 1
fi
