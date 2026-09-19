#!/bin/sh
# Keep contrib/systemd/ honest. Reads only files inside this repository, writes
# nothing, and never enables, starts or installs a unit: a live timer starts
# real Builder sessions and spends fuel, so this only ever asks systemd to
# read the files.
#
# Three checks:
#
# 1. VERIFY. Where systemd-analyze exists, `systemd-analyze --user verify` must
#    accept every unit file (the dispatch, mail-notify, health and Millhand
#    pairs). Where
#    it does not exist (macOS, a container), the check says so and skips this
#    one; it does not pass silently.
#
# 2. NO HOST PATHS. Nothing under contrib/systemd/ names a home directory or
#    /root. A host says where its tools are in ~/.config/mw/dispatch.env.
#
# 3. THE DIRECTIVES THE README PROMISES. The service is a oneshot that treats
#    exit 5 as success and leaves tmux sessions alive when it exits, and the
#    timer does not catch up. Each is one line, so grep can hold them. Each
#    pair has its own rules below and none borrows another's: the mail-notify
#    pair is a oneshot that runs mw-mail-notify on a timer that does not catch
#    up, and the health pair a oneshot that runs mw-health every 15 minutes on
#    a timer that does not catch up. The Millhand's tick pair is a oneshot that
#    outlives its tick (KillMode=process) on a timer at 7, 22, 37 and 52 that
#    does not catch up; its review pair a oneshot that treats exit 5 ("already
#    up") as success, on a timer at 07:30 and 19:30 that does catch up. And the scripts those two run
#    (contrib/mail-notify, contrib/health/mw-health.sh) are executable and parse.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR=contrib/systemd
SERVICE=$DIR/mw-dispatch.service
TIMER=$DIR/mw-dispatch.timer
MAIL_SERVICE=$DIR/mw-mail-notify.service
MAIL_TIMER=$DIR/mw-mail-notify.timer
MAIL_SCRIPT=contrib/mail-notify
HEALTH_SERVICE=$DIR/mw-health.service
HEALTH_TIMER=$DIR/mw-health.timer
HEALTH_SCRIPT=contrib/health/mw-health.sh
TICK_SERVICE=$DIR/mw-millhand-tick.service
TICK_TIMER=$DIR/mw-millhand-tick.timer
REVIEW_SERVICE=$DIR/mw-millhand-review.service
REVIEW_TIMER=$DIR/mw-millhand-review.timer

cd "$REPO_ROOT"

fail() {
	echo "check-timer-units: $*" >&2
	exit 1
}

[ -f "$SERVICE" ] || fail "$SERVICE does not exist"
[ -f "$TIMER" ] || fail "$TIMER does not exist"
[ -f "$MAIL_SERVICE" ] || fail "$MAIL_SERVICE does not exist"
[ -f "$MAIL_TIMER" ] || fail "$MAIL_TIMER does not exist"
[ -f "$MAIL_SCRIPT" ] || fail "$MAIL_SCRIPT does not exist"
[ -f "$HEALTH_SERVICE" ] || fail "$HEALTH_SERVICE does not exist"
[ -f "$HEALTH_TIMER" ] || fail "$HEALTH_TIMER does not exist"
[ -f "$HEALTH_SCRIPT" ] || fail "$HEALTH_SCRIPT does not exist"
[ -f "$TICK_SERVICE" ] || fail "$TICK_SERVICE does not exist"
[ -f "$TICK_TIMER" ] || fail "$TICK_TIMER does not exist"
[ -f "$REVIEW_SERVICE" ] || fail "$REVIEW_SERVICE does not exist"
[ -f "$REVIEW_TIMER" ] || fail "$REVIEW_TIMER does not exist"

# --- 1. systemd accepts the files ----------------------------------------
if command -v systemd-analyze >/dev/null 2>&1; then
	# verify exits 0 on some faults and only prints them, so any output at all fails.
	out=$(systemd-analyze --user verify "$SERVICE" "$TIMER" "$MAIL_SERVICE" "$MAIL_TIMER" "$HEALTH_SERVICE" "$HEALTH_TIMER" "$TICK_SERVICE" "$TICK_TIMER" "$REVIEW_SERVICE" "$REVIEW_TIMER" 2>&1) || {
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
	grep -qxF -- "$2" "$1" || fail "$1 has no line \`$2\`"
}
# The dispatch pair.
need "$SERVICE" "Type=oneshot"
need "$SERVICE" "SuccessExitStatus=5"
need "$SERVICE" "KillMode=process"
need "$SERVICE" "ExecStart=/usr/bin/env mw dispatch"
need "$TIMER" "Persistent=false"
need "$TIMER" "OnCalendar=*:0/5"
# The mail-notify pair.
need "$MAIL_SERVICE" "Type=oneshot"
need "$MAIL_SERVICE" "ExecStart=/usr/bin/env mw-mail-notify"
need "$MAIL_TIMER" "Persistent=false"
need "$MAIL_TIMER" "OnCalendar=minutely"
# The health pair.
need "$HEALTH_SERVICE" "Type=oneshot"
need "$HEALTH_SERVICE" "ExecStart=/usr/bin/env mw-health"
need "$HEALTH_TIMER" "Persistent=false"
need "$HEALTH_TIMER" "OnCalendar=*:0/15"
# The Millhand's tick pair.
need "$TICK_SERVICE" "Type=oneshot"
need "$TICK_SERVICE" "ExecStart=/usr/bin/env mw millhand tick"
need "$TICK_SERVICE" "KillMode=process"
need "$TICK_SERVICE" "TimeoutStartSec=5min"
need "$TICK_TIMER" "OnCalendar=*:7/15"
need "$TICK_TIMER" "Persistent=false"
# The Millhand's review pair. KillMode=process is here too: the wake it starts
# is a tmux window that must outlive the unit's main process.
need "$REVIEW_SERVICE" "Type=oneshot"
need "$REVIEW_SERVICE" "ExecStart=/usr/bin/env mw millhand --wake review --reason timer"
need "$REVIEW_SERVICE" "KillMode=process"
need "$REVIEW_SERVICE" "TimeoutStartSec=5min"
need "$REVIEW_SERVICE" "SuccessExitStatus=5"
need "$REVIEW_TIMER" "OnCalendar=*-*-* 07,19:30"
need "$REVIEW_TIMER" "Persistent=true"

# The scripts the services run must be there to run, and must parse.
for script in "$MAIL_SCRIPT" "$HEALTH_SCRIPT"; do
	[ -x "$script" ] || fail "$script is not executable"
	sh -n "$script" || fail "$script does not parse"
done

echo "OK: $DIR: $verified, names no host's directory, and carries the directives the README describes; $MAIL_SCRIPT and $HEALTH_SCRIPT are executable and parse"
