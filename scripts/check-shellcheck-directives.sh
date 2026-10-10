#!/bin/sh
# Hold that no comment in the rig's shell scripts is read by shellcheck as a
# directive that is not one. A comment line that begins '# shellcheck ' is a
# directive to shellcheck, so prose that starts that way ('# shellcheck is run
# over the scripts...') makes it fail with SC1072/SC1073 on any host that has it
# installed (mw-gq6.315). Reads only this repository.
#
# Two checks. The first always runs: every comment line beginning '# shellcheck '
# in scripts/ and contrib/ names a real directive key (disable, enable, source,
# source-path, shell, external-sources) with an equals sign. The second runs where
# the tool is installed and is skipped where it is not: it reports no SC1072 or
# SC1073 over those scripts.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$REPO_ROOT"

fail() {
	echo "check-shellcheck-directives: $*" >&2
	exit 1
}

# Every shell script under scripts/ and contrib/: a .sh file, or a file whose
# first line is a sh or bash shebang.
FILES=""
for f in scripts/* contrib/*; do
	[ -f "$f" ] || continue
	case $f in
	*.sh) ;;
	*)
		head -n 1 "$f" | grep -Eq '^#!.*(/|[[:space:]])(ba)?sh([[:space:]]|$)' || continue
		;;
	esac
	FILES="$FILES $f"
done
[ -n "$FILES" ] || fail "found no shell scripts to hold"

# 1. A comment line beginning '# shellcheck ' that is not key=value of a real key.
# shellcheck disable=SC2086 # the file names hold no spaces; word splitting is the point
BAD=$(grep -nE '^[[:space:]]*#[[:space:]]*shellcheck[[:space:]]' $FILES |
	grep -Ev ':[[:space:]]*#[[:space:]]*shellcheck[[:space:]]+(disable|enable|source|source-path|shell|external-sources)=' || true)
if [ -n "$BAD" ]; then
	echo "$BAD" >&2
	fail "a comment line begins '# shellcheck ' without being a directive; reword it so it does not start that way"
fi
echo "ok: no comment is read as a shellcheck directive"

# 2. shellcheck itself, where it is installed.
if command -v shellcheck >/dev/null 2>&1; then
	# shellcheck disable=SC2086
	OUT=$(shellcheck -f gcc $FILES 2>&1 || true)
	if printf '%s\n' "$OUT" | grep -E 'SC107[23]'; then
		fail "shellcheck reports SC1072 or SC1073"
	fi
	echo "ok: shellcheck reports no SC1072 or SC1073"
else
	echo "skip: shellcheck is not installed"
fi
