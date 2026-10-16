#!/bin/sh
# Hold that the repository tracks no build output (mw-gq6.517: a 23.6 MB `go
# build` binary, mw, rode to main in a commit and every clone carries it now).
# Two rules, each naming every file that breaks it:
#
# 1. No tracked file is an executable: an ELF or Mach-O binary, read off its
#    first four bytes, wherever it lives.
# 2. No tracked file is over 2 MB (MAX_BYTES), unless it is under a testdata/
#    directory or is named in ALLOW below.
#
# Reads only the repository: the one this script is in, or the one named by its
# first argument (the tests run it on throwaway repositories). Writes nothing.

set -eu

MAX_BYTES=2097152

# Tracked files over MAX_BYTES that are meant to be there, one repo-relative
# path per line. Empty: a file added here needs a reason in the commit.
ALLOW=""

if [ $# -ge 1 ]; then
	REPO_ROOT=$1
else
	REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi
cd "$REPO_ROOT"

bad=$(mktemp)
sizes=$(mktemp)
binaries=$(mktemp)
trap 'rm -f "$bad" "$sizes" "$binaries"' EXIT

# What is tracked is what the index holds: every blob's size, in one git call
# (a process per file is minutes on a slow box), and the files git itself takes
# for binary (its i/-text), the only ones whose first four bytes are read.
git -c core.quotepath=off ls-files -s | awk '$1 != "120000" { path = $0; sub(/^[^\t]*\t/, "", path); print $2 " " path }' |
	git cat-file --batch-check='%(objectsize) %(rest)' >"$sizes"
git -c core.quotepath=off ls-files --eol -s | awk -F'\t' '$2 ~ /i\/-text/ { print $3 }' >"$binaries"

while IFS= read -r file; do
	[ -n "$file" ] || continue
	magic=$(git show ":$file" | od -An -tx1 -N4 | tr -d ' \n')
	case $magic in
	7f454c46 | feedface | feedfacf | cefaedfe | cffaedfe)
		echo "$file: an executable (ELF or Mach-O) is tracked" >>"$bad"
		;;
	esac
done <"$binaries"

while IFS= read -r line; do
	size=${line%% *}
	file=${line#* }
	[ "$size" -gt "$MAX_BYTES" ] 2>/dev/null || continue
	case $file in
	testdata/* | */testdata/*) continue ;;
	esac
	if printf '%s\n' "$ALLOW" | grep -Fxq -- "$file"; then
		continue
	fi
	echo "$file: $size bytes tracked, over $MAX_BYTES, and not under testdata/ or allowed in this script" >>"$bad"
done <"$sizes"

if [ -s "$bad" ]; then
	echo "check-tracked-binaries: the repository tracks build output or a very large file:" >&2
	sed 's/^/  /' "$bad" >&2
	echo "check-tracked-binaries: git rm --cached the file and ignore it in .gitignore" >&2
	exit 1
fi

echo "check-tracked-binaries: ok"
