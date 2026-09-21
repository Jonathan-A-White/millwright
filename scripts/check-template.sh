#!/bin/sh
# Keep template/ (the skeleton a fresh vault is born from) free of anything
# that belongs to one person or one host. Reads only files inside this
# repository and writes nothing.
#
# Two checks:
#
# 1. LEAKS. No file under template/ may contain, in any case, "jonathan",
#    "jawhite", "allmymind", "/root/", "/home/", "vultr" or an email address
#    (something@domain.tld). Each offending file is named, with the line.
#
# 2. REQUIRED FILES. These must exist: the three seat charters, the Mayor's
#    vision, the Builder's ledger and the vault's CLAUDE.md.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR=template
LEAKS='jonathan|jawhite|allmymind|/root/|/home/|vultr|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'

cd "$REPO_ROOT"

fail() {
	echo "check-template: $*" >&2
	exit 1
}

[ -d "$DIR" ] || fail "$DIR does not exist"

bad=0

# --- 1. nothing personal or host-bound ------------------------------------
files=$(find "$DIR" -type f | sort)
for f in $files; do
	if hits=$(grep -n -i -E "$LEAKS" "$f"); then
		echo "check-template: $f holds something that is not for a fresh vault:" >&2
		echo "$hits" | sed 's/^/    /' >&2
		bad=1
	fi
done

# --- 2. the files a fresh vault needs ------------------------------------
for f in \
	seats/mayor/charter.md \
	seats/builder/charter.md \
	seats/millhand/charter.md \
	seats/mayor/vision.md \
	seats/builder/ledger.md \
	CLAUDE.md; do
	if [ ! -f "$DIR/$f" ]; then
		echo "check-template: $DIR/$f is missing" >&2
		bad=1
	fi
done

[ "$bad" -eq 0 ] || exit 1
echo "OK: $DIR holds the required files and nothing personal or host-bound"
