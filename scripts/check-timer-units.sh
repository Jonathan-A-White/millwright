#!/bin/sh
# Keep contrib/systemd/ honest. Reads only files inside this repository, writes
# nothing, and never enables, starts or installs a unit: a live timer starts
# real Builder sessions and spends fuel, so this only ever asks systemd to
# read the files.
#
# Three checks:
#
# 1. VERIFY. Where systemd-analyze exists, `systemd-analyze --user verify` must
#    accept both unit files. Where it does not (macOS, a container), the check
#    says so and skips this one; it does not pass silently.
#
# 2. NO HOST PATHS. Nothing under contrib/systemd/ names a home directory or
#    /root. A host says where its tools are in ~/.config/mw/dispatch.env.
#
# 3. THE DIRECTIVES THE README PROMISES. The service is a oneshot that treats
#    exit 5 as success and leaves tmux sessions alive when it exits, and the
#    timer does not catch up. Each is one line, so grep can hold them.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR=contrib/systemd
SERVICE=$DIR/mw-dispatch.service
TIMER=$DIR/mw-dispatch.timer

cd "$REPO_ROOT"

fail() {
	echo "check-timer-units: $*" >&2
	exit 1
}

[ -f "$SERVICE" ] || fail "$SERVICE does not exist"
[ -f "$TIMER" ] || fail "$TIMER does not exist"

# --- 1. systemd accepts both files ---------------------------------------
if command -v systemd-analyze >/dev/null 2>&1; then
	# verify exits 0 on some faults and only prints them, so any output at all fails.
	out=$(systemd-analyze --user verify "$SERVICE" "$TIMER" 2>&1) || {
		echo "$out" >&2
		fail "systemd-analyze rejected the unit files"
	}
	if [ -n "$out" ]; then
		echo "$out" >&2
		fail "systemd-analyze had something to say about the unit files"
	fi
	verified="verified by systemd-analyze"
else
	echo "check-timer-units: systemd-analyze is not installed here, so the unit files were NOT verified; only checks 2 and 3 ran" >&2
	verified="NOT verified by systemd-analyze (not installed)"
fi

# --- 2. no host's paths --------------------------------------------------
if grep -rn '/home/\|/root/' "$DIR"; then
	fail "$DIR names a host's directory; put it in ~/.config/mw/dispatch.env instead"
fi

# --- 3. the directives the README promises -------------------------------
need() {
	# $1 is the file, $2 the exact line it must carry.
	grep -qx -- "$2" "$1" || fail "$1 has no line \`$2\`"
}
need "$SERVICE" "Type=oneshot"
need "$SERVICE" "SuccessExitStatus=5"
need "$SERVICE" "KillMode=process"
need "$SERVICE" "ExecStart=/usr/bin/env mw dispatch"
need "$TIMER" "Persistent=false"

echo "OK: $DIR: $verified, names no host's directory, and carries the directives the README describes"
