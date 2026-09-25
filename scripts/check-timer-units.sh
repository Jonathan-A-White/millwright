#!/bin/sh
# Keep contrib/systemd/ honest. Reads only files inside this repository, writes
# nothing, and never enables, starts or installs a unit: a live timer starts
# real Builder sessions and spends fuel, so this only ever asks systemd to
# read the files.
#
# Four checks:
#
# 1. VERIFY. Where systemd-analyze exists, `systemd-analyze --user verify` must
#    accept every --user unit file (the dispatch, mail-notify, health and
#    Millhand pairs), and a separate `systemd-analyze verify` (no --user, since
#    they are system units) must accept the three under contrib/systemd/system/.
#    Where systemd-analyze does not exist (macOS, a container), the check says
#    so and skips both; it does not pass silently.
#
# 2. NO HOST PATHS. Nothing under contrib/systemd/ names a home directory or
#    /root. A host says where its tools are in ~/.config/mw/dispatch.env (or,
#    for the system units, ~/.config/mw/seat.env — %h, which is /root for the
#    system manager). contrib/seat.env.example is the one place root's own
#    path is named on purpose, and is outside this directory.
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
#    up") as success, on a timer at 07:30 and 19:30 that does catch up. The doctor
#    pair is a oneshot run straight from ~/.local/bin (not through `env`) on a timer
#    at 2, 7, 12, ... that does not catch up. And the scripts those two run
#    (contrib/mail-notify, contrib/health/mw-health.sh) are executable and parse.
#    The system pair carries the same shape, run as root; mw-seat-tmux is
#    forking, never kills the server it starts or found, restarts one that
#    dies (never with RemainAfterExit=, which swallows the restart — see
#    scripts/check-seat-tmux-respawn.sh for the live proof), and scores -900 —
#    exactly once each, since a stray second line would silently double up.
#
# 4. THE SEAT UNIT'S COMMAND. mw-seat-tmux.service's ExecStart, run with a
#    stand-in tmux on PATH, calls only has-session when a session named "0"
#    already exists, and starts one only when it does not.

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
DOCTOR_SERVICE=$DIR/mw-doctor.service
DOCTOR_TIMER=$DIR/mw-doctor.timer
SEAT_TMUX_SERVICE=$DIR/system/mw-seat-tmux.service
SYS_DOCTOR_SERVICE=$DIR/system/mw-doctor.service
SYS_DOCTOR_TIMER=$DIR/system/mw-doctor.timer

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
[ -f "$DOCTOR_SERVICE" ] || fail "$DOCTOR_SERVICE does not exist"
[ -f "$DOCTOR_TIMER" ] || fail "$DOCTOR_TIMER does not exist"
[ -f "$SEAT_TMUX_SERVICE" ] || fail "$SEAT_TMUX_SERVICE does not exist"
[ -f "$SYS_DOCTOR_SERVICE" ] || fail "$SYS_DOCTOR_SERVICE does not exist"
[ -f "$SYS_DOCTOR_TIMER" ] || fail "$SYS_DOCTOR_TIMER does not exist"

# --- 1. systemd accepts the files ----------------------------------------
if command -v systemd-analyze >/dev/null 2>&1; then
	# verify exits 0 on some faults and only prints them, so a line naming one of
	# this rig's unit files fails. With --user it also loads the host's own units
	# and complains about them (not the rig's to change): those lines are noted
	# and do not fail. A line names a rig unit by its path or, as systemd does for
	# some faults, by its file name alone.
	UNITS="$SERVICE $TIMER $MAIL_SERVICE $MAIL_TIMER $HEALTH_SERVICE $HEALTH_TIMER $TICK_SERVICE $TICK_TIMER $REVIEW_SERVICE $REVIEW_TIMER $DOCTOR_SERVICE $DOCTOR_TIMER"
	# shellcheck disable=SC2086 # the unit paths hold no spaces; word splitting is the point
	out=$(systemd-analyze --user verify $UNITS 2>&1) || {
		echo "$out" >&2
		fail "systemd-analyze rejected the unit files"
	}
	ours=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		mine=""
		for unit in $UNITS; do
			case $line in *"${unit##*/}"*) mine=1 ;; esac
		done
		if [ -n "$mine" ]; then
			echo "$line" >&2
			ours=1
		else
			echo "check-timer-units: ignored: not the rig's unit: $line" >&2
		fi
	done <<-END
	$out
	END
	[ -z "$ours" ] || fail "systemd-analyze had something to say about the unit files"

	# The system pair: no --user, since these are system units (run as root,
	# outside any user manager). Same "ignore what is not ours" treatment.
	SYS_UNITS="$SEAT_TMUX_SERVICE $SYS_DOCTOR_SERVICE $SYS_DOCTOR_TIMER"
	# shellcheck disable=SC2086
	sys_out=$(systemd-analyze verify $SYS_UNITS 2>&1) || {
		echo "$sys_out" >&2
		fail "systemd-analyze rejected the system unit files"
	}
	sys_ours=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		mine=""
		for unit in $SYS_UNITS; do
			case $line in *"${unit##*/}"*) mine=1 ;; esac
		done
		if [ -n "$mine" ]; then
			echo "$line" >&2
			sys_ours=1
		else
			echo "check-timer-units: ignored: not the rig's unit: $line" >&2
		fi
	done <<-END
	$sys_out
	END
	[ -z "$sys_ours" ] || fail "systemd-analyze had something to say about the system unit files"

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
need "$SERVICE" "SuccessExitStatus=5 7"
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
# The doctor pair.
need "$DOCTOR_SERVICE" "Type=oneshot"
need "$DOCTOR_SERVICE" "ExecStart=/usr/bin/env %h/.local/bin/mw doctor"
need "$DOCTOR_TIMER" "OnCalendar=*:2/5"
need "$DOCTOR_TIMER" "Persistent=false"
# The seat's tmux server, a system unit. No RemainAfterExit=yes: that marked
# the unit active (exited) the instant its tracked server died instead of
# restarting it. scripts/check-seat-tmux-respawn.sh proves the restart live.
need "$SEAT_TMUX_SERVICE" "Type=forking"
if grep -q '^RemainAfterExit=' "$SEAT_TMUX_SERVICE"; then
	fail "$SEAT_TMUX_SERVICE sets RemainAfterExit=, which swallows Restart="
fi
need "$SEAT_TMUX_SERVICE" "Restart=always"
need "$SEAT_TMUX_SERVICE" "RestartSec=2"
need "$SEAT_TMUX_SERVICE" "StartLimitIntervalSec=0"
need "$SEAT_TMUX_SERVICE" "ExecStop=/bin/true"
need "$SEAT_TMUX_SERVICE" "KillMode=process"
need "$SEAT_TMUX_SERVICE" "OOMScoreAdjust=-900"
need "$SEAT_TMUX_SERVICE" "WantedBy=multi-user.target"
[ "$(grep -c 'OOMScoreAdjust=-900' "$SEAT_TMUX_SERVICE")" = 1 ] ||
	fail "$SEAT_TMUX_SERVICE does not carry OOMScoreAdjust=-900 exactly once"
[ "$(grep -c 'KillMode=process' "$SEAT_TMUX_SERVICE")" = 1 ] ||
	fail "$SEAT_TMUX_SERVICE does not carry KillMode=process exactly once"
# The system doctor pair: same shape as the --user pair, run as root.
need "$SYS_DOCTOR_SERVICE" "Type=oneshot"
need "$SYS_DOCTOR_SERVICE" "User=root"
need "$SYS_DOCTOR_SERVICE" "ExecStart=/usr/bin/env %h/.local/bin/mw doctor"
need "$SYS_DOCTOR_TIMER" "OnCalendar=*:2/5"
need "$SYS_DOCTOR_TIMER" "Persistent=false"

# The scripts the services run must be there to run, and must parse.
for script in "$MAIL_SCRIPT" "$HEALTH_SCRIPT"; do
	[ -x "$script" ] || fail "$script is not executable"
	sh -n "$script" || fail "$script does not parse"
done

# --- 4. the seat unit's command, against a stand-in tmux ------------------
# Pull the shell command out of ExecStart=/bin/sh -c '...': the one line the
# unit runs. A change to the unit's quoting that this cannot parse is itself
# worth failing loudly on, rather than skipping the check.
execline=$(grep '^ExecStart=' "$SEAT_TMUX_SERVICE") || fail "$SEAT_TMUX_SERVICE has no ExecStart="
prefix="ExecStart=/bin/sh -c '"
case $execline in
"$prefix"*"'")
	seatcmd=${execline#"$prefix"}
	seatcmd=${seatcmd%"'"}
	;;
*) fail "$SEAT_TMUX_SERVICE's ExecStart is not the /bin/sh -c '...' this check knows how to run: $execline" ;;
esac

ST=$(mktemp -d)
trap 'rm -rf "$ST"' EXIT INT TERM
STSH=$(command -v sh)
mkdir -p "$ST/bin"
cat >"$ST/bin/tmux" <<'EOF'
#!/bin/sh
echo "tmux $*" >>"$SEAT_CALLS"
if [ "$1 $2 $3" = "has-session -t =0" ]; then
	[ "${SEAT_SESSION_UP:-0}" = 1 ] && exit 0 || exit 1
fi
exit 0
EOF
chmod +x "$ST/bin/tmux"

: >"$ST/calls"
PATH="$ST/bin" SEAT_CALLS="$ST/calls" SEAT_SESSION_UP=1 "$STSH" -c "$seatcmd"
grep -qx 'tmux has-session -t =0' "$ST/calls" || fail "a session already up: ExecStart did not call has-session"
grep -q '^tmux new-session' "$ST/calls" && fail "a session already up: ExecStart started a new one anyway"

: >"$ST/calls"
PATH="$ST/bin" SEAT_CALLS="$ST/calls" SEAT_SESSION_UP=0 "$STSH" -c "$seatcmd"
grep -qx 'tmux new-session -d -s 0' "$ST/calls" || fail "no session up: ExecStart did not start one with new-session -d -s 0"

echo "OK: $DIR: $verified, names no host's directory, carries the directives the README describes, and mw-seat-tmux's ExecStart is a no-op only when session 0 is already up; $MAIL_SCRIPT and $HEALTH_SCRIPT are executable and parse"
