#!/bin/sh
# Prove two things live about contrib/systemd/system/mw-seat-tmux.service that
# a static read of the file cannot: that it does NOT restart-loop when it
# finds a session already up that it did not start (mw-gq6.111), and that it
# still respawns a tmux server that dies out from under it, the way the unit
# file promises (Restart=, RestartSec=, and no RemainAfterExit= to swallow
# the restart).
#
# mw-gq6.111: with the unit's old Type=forking, ExecStart's `has-session ||
# new-session` forked a new tmux server only in the branch where none existed
# yet. When a session was already up, has-session succeeded and the whole
# command exited immediately with nothing forked into the unit's cgroup, so
# systemd saw its tracked process gone a moment after starting it and, with
# Restart=always/RestartSec=2, restarted the unit every 2s forever. Type=
# simple fixes this: ExecStart now settles into a loop that watches
# has-session, so the process systemd tracks stays up for as long as the
# session does, whichever of them started it.
#
# check-timer-units.sh already holds the file's static directives and runs
# its ExecStart's has-session/new-session/watch logic against a stand-in
# tmux; it never starts a real unit. This script does, on purpose: only a
# live run can show whether systemd actually restarts (or does not restart)
# the service. So it loads the file's own Type=, Restart=, RestartSec= and
# KillMode= values into transient --user units (systemd-run --collect: no
# file written, unloaded when they stop), each on a tmux socket named for
# this run alone — the real session "0" on this host is never touched.
#
# --system (root, no login) is how the VPS actually runs this unit; that needs
# root and is what check-timer-units.sh's systemd-analyze pass (no --user)
# checks statically. Restart=/RestartSec=/KillMode= behave the same under
# --user, so a --user transient unit is a faithful, root-free stand-in for the
# one thing that can only be proven by actually running it.
#
# Skips (exit 0, one note on stderr) rather than fails when this host has no
# reachable --user systemd instance or no tmux: the static checks in
# check-timer-units.sh still cover the file's directives there.
#
# mw-gq6.112: with Type=simple, systemd marks the unit active/running the
# instant /bin/sh is exec'd -- before ExecStart's `tmux new-session` has
# actually made the session. A single list-sessions check made right after
# seeing active/running raced that gap and failed under load (reported by the
# Mayor from a `make lint` run on the VPS). Every list-sessions check here
# that follows an active/running read now polls, bounded, instead of reading
# once; scenario 3 below proves it against a simulated slow new-session.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SERVICE=contrib/systemd/system/mw-seat-tmux.service

cd "$REPO_ROOT"

fail() {
	echo "check-seat-tmux-respawn: $*" >&2
	exit 1
}

[ -f "$SERVICE" ] || fail "$SERVICE does not exist"

# --- 1. the directives that make a restart possible at all -----------------
if grep -q '^RemainAfterExit=' "$SERVICE"; then
	fail "$SERVICE sets RemainAfterExit=, which marks the unit active (exited) the moment its tracked process dies instead of restarting it -- confirmed live: with RemainAfterExit=yes and Restart=always both set, a killed server left the unit active/exited with 0 restarts"
fi
grep -qx 'Restart=always' "$SERVICE" ||
	fail "$SERVICE has no \`Restart=always\` -- a clean tmux exit (what \`tmux kill-server\` causes) does not count as a failure, so Restart=on-failure alone never fires"
grep -q '^RestartSec=' "$SERVICE" || fail "$SERVICE has no \`RestartSec=\`"
grep -qx 'Type=simple' "$SERVICE" || fail "$SERVICE is no longer Type=simple; this check's transient unit assumes it is"
grep -qx 'KillMode=process' "$SERVICE" || fail "$SERVICE has no \`KillMode=process\`"

restartsec=$(sed -n 's/^RestartSec=//p' "$SERVICE" | head -1)

# --- 2. does it actually come back? -----------------------------------------
if ! command -v systemd-run >/dev/null 2>&1 || ! systemctl --user show -p Version >/dev/null 2>&1; then
	echo "check-seat-tmux-respawn: no reachable --user systemd instance here, so the live restart was NOT proven; only the file's directives were checked" >&2
	echo "OK: $SERVICE (directives only)"
	exit 0
fi
if ! command -v tmux >/dev/null 2>&1; then
	echo "check-seat-tmux-respawn: tmux is not installed here, so the live restart was NOT proven; only the file's directives were checked" >&2
	echo "OK: $SERVICE (directives only)"
	exit 0
fi

execline=$(grep '^ExecStart=' "$SERVICE") || fail "$SERVICE has no ExecStart="
prefix="ExecStart=/bin/sh -c '"
case $execline in
"$prefix"*"'")
	execcmd=${execline#"$prefix"}
	execcmd=${execcmd%"'"}
	;;
*) fail "$SERVICE's ExecStart is not the /bin/sh -c '...' this check knows how to run: $execline" ;;
esac

RUNID=$$-$(date +%s 2>/dev/null || echo 0)
SOCK_PRE="mw-seat-preexist-test-$RUNID"
UNIT_PRE="mw-seat-tmux-preexist-test-$RUNID"
SOCK_KILL="mw-seat-respawn-test-$RUNID"
UNIT_KILL="mw-seat-tmux-respawn-test-$RUNID"
SOCK_SLOW="mw-seat-slow-test-$RUNID"
UNIT_SLOW="mw-seat-tmux-slow-test-$RUNID"

T=$(mktemp -d)
cleanup() {
	systemctl --user stop "$UNIT_PRE.service" >/dev/null 2>&1 || true
	systemctl --user stop "$UNIT_KILL.service" >/dev/null 2>&1 || true
	systemctl --user stop "$UNIT_SLOW.service" >/dev/null 2>&1 || true
	tmux -L "$SOCK_PRE" kill-server >/dev/null 2>&1 || true
	tmux -L "$SOCK_KILL" kill-server >/dev/null 2>&1 || true
	tmux -L "$SOCK_SLOW" kill-server >/dev/null 2>&1 || true
	# tmux does not always unlink its socket file on a killed server; take the
	# stale files with it rather than leaving them in the tmpdir run after run.
	rm -f "${TMUX_TMPDIR:-/tmp/tmux-$(id -u)}/$SOCK_PRE" "${TMUX_TMPDIR:-/tmp/tmux-$(id -u)}/$SOCK_KILL" "${TMUX_TMPDIR:-/tmp/tmux-$(id -u)}/$SOCK_SLOW"
	rm -rf "$T"
}
trap cleanup EXIT INT TERM

# A stand-in "tmux" ahead of the real one on PATH, so the file's own
# ExecStart line runs unmodified against a socket this run picks (via
# $MW_TEST_SEAT_SOCK, set per scenario below) instead of the host's real
# session "0". The real tmux is called by absolute path: a bare "tmux" here
# would resolve back to this same wrapper first on PATH.
REALTMUX=$(command -v tmux)
mkdir -p "$T/bin"
{
	echo '#!/bin/sh'
	echo "exec $REALTMUX -L \"\$MW_TEST_SEAT_SOCK\" \"\$@\""
} >"$T/bin/tmux"
chmod +x "$T/bin/tmux"

# systemctl show's `Name=Value` lines, one property per line: unambiguous
# regardless of how many -p names are asked for in one call (unlike --value,
# whose lines do not always come back in the order they were asked for).
show_field() { # <unit> <field>
	systemctl --user show "$1.service" -p "$2" 2>/dev/null | sed -n "s/^$2=//p"
}

# Bounded poll for a session to exist, rather than a single read straight
# after the unit reports active/running: with Type=simple that report lands
# the instant ExecStart's /bin/sh is exec'd, before its `tmux new-session` has
# necessarily run (mw-gq6.112) -- a few seconds of polling covers that gap
# without hiding a real failure, since it still returns failure once the
# bound runs out.
wait_for_session() { # <sock>
	i=0
	limit=5
	while [ "$i" -le "$limit" ]; do
		tmux -L "$1" list-sessions >/dev/null 2>&1 && return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}

# --- 1. a session already up before the unit ever starts --------------------
# The mw-gq6.111 bug: has-session succeeding on a session the unit did not
# start used to leave nothing forked into the unit's cgroup, so Type=forking
# saw its tracked process gone a moment after starting and restart-looped
# forever. Proves: after 10s the unit made zero restarts, and the
# pre-existing session (marked, so a replacement rather than a real leave-
# alone would be caught) is exactly as it was.
tmux -L "$SOCK_PRE" new-session -d -s 0
tmux -L "$SOCK_PRE" list-sessions >/dev/null 2>&1 || fail "could not set up a pre-existing session on $SOCK_PRE to test against"
tmux -L "$SOCK_PRE" set-environment -t =0 MW_TEST_MARKER "$RUNID"

systemd-run --user --collect --unit="$UNIT_PRE" \
	--service-type=simple \
	-p "Restart=always" -p "RestartSec=$restartsec" -p "KillMode=process" -p "ExecStop=/bin/true" \
	-E "PATH=$T/bin:/usr/bin:/bin" -E "MW_TEST_SEAT_SOCK=$SOCK_PRE" \
	/bin/sh -c "$execcmd" >/dev/null

sleep 10
as=$(show_field "$UNIT_PRE" ActiveState)
ss=$(show_field "$UNIT_PRE" SubState)
nrestarts=$(show_field "$UNIT_PRE" NRestarts)
[ "$as/$ss" = "active/running" ] && [ "${nrestarts:-0}" = 0 ] ||
	fail "a session already up before the unit started: expected active/running with 0 restarts 10s later, got ActiveState=$as SubState=$ss NRestarts=$nrestarts -- this is the mw-gq6.111 restart loop if NRestarts is climbing"

tmux -L "$SOCK_PRE" list-sessions >/dev/null 2>&1 ||
	fail "a session already up before the unit started: the session is gone now"
marker=$(tmux -L "$SOCK_PRE" show-environment -t =0 MW_TEST_MARKER 2>/dev/null | sed -n 's/^MW_TEST_MARKER=//p')
[ "$marker" = "$RUNID" ] ||
	fail "a session already up before the unit started: its marker environment variable is gone, so the unit replaced it instead of leaving it alone (got '$marker')"

systemctl --user stop "$UNIT_PRE.service" >/dev/null 2>&1 || true

# --- 2. does it still come back once a server it's watching dies? -----------
systemd-run --user --collect --unit="$UNIT_KILL" \
	--service-type=simple \
	-p "Restart=always" -p "RestartSec=$restartsec" -p "KillMode=process" -p "ExecStop=/bin/true" \
	-E "PATH=$T/bin:/usr/bin:/bin" -E "MW_TEST_SEAT_SOCK=$SOCK_KILL" \
	/bin/sh -c "$execcmd" >/dev/null

# Waits for ActiveState/SubState to read active/running with NRestarts above
# the given threshold -- not just active/running by itself, which a stale
# read can still show in the instant after kill-server, before systemd has
# noticed the process is gone. Up to restartsec+5 seconds.
wait_for_restart_above() {
	threshold=$1
	i=0
	limit=$((restartsec + 5))
	while [ "$i" -le "$limit" ]; do
		as=$(show_field "$UNIT_KILL" ActiveState)
		ss=$(show_field "$UNIT_KILL" SubState)
		nrestarts=$(show_field "$UNIT_KILL" NRestarts)
		if [ "$as/$ss" = "active/running" ] && [ "${nrestarts:-0}" -gt "$threshold" ]; then
			return 0
		fi
		sleep 1
		i=$((i + 1))
	done
	fail "the unit did not reach active/running with more than $threshold restart(s) within ${limit}s of the server dying; last seen: ActiveState=$as SubState=$ss NRestarts=$nrestarts"
}

as=$(show_field "$UNIT_KILL" ActiveState)
ss=$(show_field "$UNIT_KILL" SubState)
[ "$as/$ss" = "active/running" ] || fail "the unit was not active/running right after it started: ActiveState=$as SubState=$ss"
wait_for_session "$SOCK_KILL" || fail "the unit says active/running but session $SOCK_KILL has no session"

tmux -L "$SOCK_KILL" kill-server

wait_for_restart_above 0
wait_for_session "$SOCK_KILL" ||
	fail "the unit reports active/running again after the server died, but a fresh tmux session is not there"

systemctl --user stop "$UNIT_KILL.service" >/dev/null 2>&1 || true

# --- 3. does the check itself survive a slow new-session? -------------------
# mw-gq6.112 live: a stand-in tmux that sleeps before running new-session,
# standing in for a new-session slowed by host load. Right after the unit
# reports active/running, its own session is not there yet -- proving
# wait_for_session's poll, not a single read, is what makes this check pass.
mkdir -p "$T/bin-slow"
{
	echo '#!/bin/sh'
	echo 'case "$1" in'
	echo '	new-session) sleep 3 ;;'
	echo 'esac'
	echo "exec $REALTMUX -L \"\$MW_TEST_SEAT_SOCK\" \"\$@\""
} >"$T/bin-slow/tmux"
chmod +x "$T/bin-slow/tmux"

systemd-run --user --collect --unit="$UNIT_SLOW" \
	--service-type=simple \
	-p "Restart=always" -p "RestartSec=$restartsec" -p "KillMode=process" -p "ExecStop=/bin/true" \
	-E "PATH=$T/bin-slow:/usr/bin:/bin" -E "MW_TEST_SEAT_SOCK=$SOCK_SLOW" \
	/bin/sh -c "$execcmd" >/dev/null

as=$(show_field "$UNIT_SLOW" ActiveState)
ss=$(show_field "$UNIT_SLOW" SubState)
[ "$as/$ss" = "active/running" ] || fail "slow new-session: the unit was not active/running right after it started: ActiveState=$as SubState=$ss"
wait_for_session "$SOCK_SLOW" ||
	fail "slow new-session: session $SOCK_SLOW never appeared within wait_for_session's bound -- the poll is not covering a slow new-session the way it should"

systemctl --user stop "$UNIT_SLOW.service" >/dev/null 2>&1 || true

echo "OK: $SERVICE does not restart-loop on a session it found already up (0 restarts, untouched, over 10s), still restarts a tmux server that dies out from under it, reaching active/running again within RestartSec (${restartsec}s), and this check's own session waits survive a slow new-session"
