#!/bin/sh
# Prove that contrib/systemd/system/mw-seat-tmux.service actually respawns a
# tmux server that dies out from under it, the way the unit file promises
# (Restart=, RestartSec=, and no RemainAfterExit= to swallow the restart).
#
# check-timer-units.sh already holds the file's static directives and its
# ExecStart's has-session/new-session logic to stand-ins; it never starts a
# real unit. This script does, on purpose: a static read of the file cannot
# tell you whether systemd actually restarts the service once the process it
# was tracking is gone, only a live run can. So it loads the file's own
# Type=, Restart=, RestartSec= and KillMode= values into a transient --user
# unit (systemd-run --collect: no file written, unloaded when it stops), on
# a tmux socket named for this run alone — the real session "0" on this host
# is never touched — kills that tmux server, and watches the unit come back
# active and running, with a fresh session, within RestartSec.
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
grep -qx 'Type=forking' "$SERVICE" || fail "$SERVICE is no longer Type=forking; this check's transient unit assumes it is"
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
SOCK="mw-seat-respawn-test-$RUNID"
UNIT="mw-seat-tmux-respawn-test-$RUNID"

T=$(mktemp -d)
cleanup() {
	systemctl --user stop "$UNIT.service" >/dev/null 2>&1 || true
	tmux -L "$SOCK" kill-server >/dev/null 2>&1 || true
	# tmux does not always unlink its socket file on a killed server; take the
	# stale file with it rather than leaving it in the tmpdir run after run.
	rm -f "${TMUX_TMPDIR:-/tmp/tmux-$(id -u)}/$SOCK"
	rm -rf "$T"
}
trap cleanup EXIT INT TERM

# A stand-in "tmux" ahead of the real one on PATH, so the file's own
# ExecStart line runs unmodified against this run's own socket instead of
# the host's real session "0". The real tmux is called by absolute path: a
# bare "tmux" here would resolve back to this same wrapper first on PATH.
REALTMUX=$(command -v tmux)
mkdir -p "$T/bin"
{
	echo '#!/bin/sh'
	echo "exec $REALTMUX -L $SOCK \"\$@\""
} >"$T/bin/tmux"
chmod +x "$T/bin/tmux"

systemd-run --user --collect --unit="$UNIT" \
	--service-type=forking \
	-p "Restart=always" -p "RestartSec=$restartsec" -p "KillMode=process" -p "ExecStop=/bin/true" \
	-E "PATH=$T/bin:/usr/bin:/bin" \
	/bin/sh -c "$execcmd" >/dev/null

# systemctl show's `Name=Value` lines, one property per line: unambiguous
# regardless of how many -p names are asked for in one call (unlike --value,
# whose lines do not always come back in the order they were asked for).
show_field() { # <field>
	systemctl --user show "$UNIT.service" -p "$1" 2>/dev/null | sed -n "s/^$1=//p"
}

# Waits for ActiveState/SubState to read active/running with NRestarts above
# the given threshold -- not just active/running by itself, which a stale
# read can still show in the instant after kill-server, before systemd has
# noticed the process is gone. Up to restartsec+5 seconds.
wait_for_restart_above() {
	threshold=$1
	i=0
	limit=$((restartsec + 5))
	while [ "$i" -le "$limit" ]; do
		as=$(show_field ActiveState)
		ss=$(show_field SubState)
		nrestarts=$(show_field NRestarts)
		if [ "$as/$ss" = "active/running" ] && [ "${nrestarts:-0}" -gt "$threshold" ]; then
			return 0
		fi
		sleep 1
		i=$((i + 1))
	done
	fail "the unit did not reach active/running with more than $threshold restart(s) within ${limit}s of the server dying; last seen: ActiveState=$as SubState=$ss NRestarts=$nrestarts"
}

as=$(show_field ActiveState)
ss=$(show_field SubState)
[ "$as/$ss" = "active/running" ] || fail "the unit was not active/running right after it started: ActiveState=$as SubState=$ss"
tmux -L "$SOCK" list-sessions >/dev/null 2>&1 || fail "the unit says active/running but session $SOCK has no session"

tmux -L "$SOCK" kill-server

wait_for_restart_above 0
tmux -L "$SOCK" list-sessions >/dev/null 2>&1 ||
	fail "the unit reports active/running again after the server died, but a fresh tmux session is not there"

echo "OK: $SERVICE restarts a tmux server that dies out from under it, and \`systemctl --user show $UNIT.service -p ActiveState,SubState\` says active/running again within RestartSec (${restartsec}s)"
