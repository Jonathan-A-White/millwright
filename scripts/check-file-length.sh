#!/bin/sh
# Hold the length of Go files (mw-6ww.175.6): a non-test .go file is at most
# LIMIT lines long unless it is on LISTED below, the files that were over it
# when this check began. The list may only shrink:
#   - a non-test .go file over LIMIT that is not on the list is refused;
#   - a listed file longer than its ceiling (its length when it was listed) is
#     refused, so a listed file cannot grow;
#   - a listed file that is LIMIT lines or fewer, or is gone, is refused until
#     it is taken off the list;
#   - a list of more than MAX_LISTED entries is refused, so the list cannot grow.
# To bring a file off the list, split it (as application/status_render.go was
# split off application/status.go), then delete its line below and lower
# MAX_LISTED by one. Test files (_test.go) are not counted.
#
# Reads only the tree: the repository this script is in, or the directory named
# by its first argument; a second argument names a list file to use instead of
# LISTED (the tests do). Writes nothing.

set -eu

LIMIT=2500
MAX_LISTED=5

# LISTED: one "path ceiling" per line, the path from the tree's root.
LISTED='
application/next.go 4590
application/dispatch.go 3447
application/postern.go 2680
application/apptest/faketracker.go 2837
infrastructure/config/config.go 4317
'

if [ $# -ge 1 ]; then
	ROOT=$1
else
	ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi
if [ $# -ge 2 ]; then
	LISTED=$(cat "$2")
fi

cd "$ROOT"
bad=0
say() {
	echo "check-file-length: $1" >&2
	bad=1
}

count=$(printf '%s\n' "$LISTED" | grep -c '[^[:space:]]' || true)
if [ "$count" -gt "$MAX_LISTED" ]; then
	say "the list of long files has $count entries, more than the $MAX_LISTED it may hold: the list only shrinks"
fi

# Each listed file: still there, still over LIMIT, and not past its ceiling.
printf '%s\n' "$LISTED" | while read -r path ceiling; do
	[ -n "$path" ] || continue
	if [ ! -f "$path" ]; then
		echo "check-file-length: $path is on the list of long files and is not there: take it off the list" >&2
		exit 1
	fi
	n=$(wc -l <"$path" | tr -d ' ')
	if [ "$n" -le "$LIMIT" ]; then
		echo "check-file-length: $path is $n lines, within $LIMIT: take it off the list of long files" >&2
		exit 1
	fi
	if [ "$n" -gt "$ceiling" ]; then
		echo "check-file-length: $path is $n lines, past its ceiling of $ceiling on the list of long files: split it, do not grow it" >&2
		exit 1
	fi
done || bad=1

# Every other non-test Go file: within LIMIT.
find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -not -path './node_modules/*' -not -path './vendor/*' | sed 's|^\./||' | sort | while read -r path; do
	n=$(wc -l <"$path" | tr -d ' ')
	[ "$n" -gt "$LIMIT" ] || continue
	if ! printf '%s\n' "$LISTED" | awk -v p="$path" '$1 == p { found = 1 } END { exit !found }'; then
		echo "check-file-length: $path is $n lines, over $LIMIT and not on the list of long files: split it along its own seam" >&2
		exit 1
	fi
done || bad=1

exit "$bad"
