#!/bin/sh
# Hold that every Go file in the tree is gofmt-formatted (mw-gq6.1156: two
# unformatted files reached main because nothing in the gate ran gofmt). Runs
# `gofmt -l .` and, when it lists a file, prints each one and exits 1.
#
# Reads only the tree: the repository this script is in, or the directory named
# by its first argument (the tests run it on throwaway directories). Writes
# nothing.

set -eu

if [ $# -ge 1 ]; then
	ROOT=$1
else
	ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi

if command -v gofmt >/dev/null 2>&1; then
	GOFMT=gofmt
elif [ -x /usr/local/go/bin/gofmt ]; then
	GOFMT=/usr/local/go/bin/gofmt
else
	echo "check-gofmt: gofmt not found on PATH" >&2
	exit 2
fi

cd "$ROOT"
listed=$("$GOFMT" -l .)
if [ -n "$listed" ]; then
	echo "check-gofmt: gofmt -l lists these files; run gofmt -w on them:" >&2
	echo "$listed" >&2
	exit 1
fi
