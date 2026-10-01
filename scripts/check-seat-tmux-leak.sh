#!/bin/sh
# Prove that scripts/check-seat-tmux-respawn.sh leaves nothing running behind
# when its run is killed with SIGKILL, which its cleanup trap cannot catch
# (mw-gq6.195). Seen live: a killed run left
# mw-seat-tmux-preexist-test-...service active for 24 h on the laptop. Its
# command was the unit file's own `tmux ...` found through a PATH wrapper dir
# the dead run had deleted, so PATH fell through to the real tmux and the unit
# polled the real default socket's session "0" for a day.
#
# Three things are checked, against the real script and a --user systemd:
#   1. a run killed mid-scenario leaves test units behind, and every one of
#      them runs tmux by absolute path with -L (it cannot reach the default
#      socket whatever happens to PATH) and carries a RuntimeMaxSec bound;
#   2. a start limit is set beside that bound: with Restart=always a bare
#      RuntimeMaxSec only restarts the unit, forever;
#   3. the next run's start sweeps a stale mw-seat-*-test-* unit and tmux
#      socket (a stale one is planted here under a long-past run id; the
#      script is run with MW_SEAT_RESPAWN_SWEEP_ONLY=1, which stops it right
#      after the sweep so this check does not pay for a whole second run).
#
# Skips (exit 0, one note on stderr) where there is no reachable --user
# systemd instance or no tmux, as check-seat-tmux-respawn.sh itself does.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$REPO_ROOT"
RESPAWN=scripts/check-seat-tmux-respawn.sh

fail() {
	echo "check-seat-tmux-leak: $*" >&2
	exit 1
}

if ! command -v systemd-run >/dev/null 2>&1 || ! systemctl --user show -p Version >/dev/null 2>&1; then
	echo "check-seat-tmux-leak: no reachable --user systemd instance here, so the leak was NOT checked" >&2
	echo "OK: (skipped)"
	exit 0
fi
if ! command -v tmux >/dev/null 2>&1; then
	echo "check-seat-tmux-leak: tmux is not installed here, so the leak was NOT checked" >&2
	echo "OK: (skipped)"
	exit 0
fi

T=$(mktemp -d)
STALE_RUN=$$-1000000000
STALE_UNIT=mw-seat-tmux-stale-test-$STALE_RUN
STALE_SOCK=mw-seat-stale-test-$STALE_RUN
SOCKDIR=${TMUX_TMPDIR:-/tmp/tmux-$(id -u)}
RUNPID=

# Only the units this check's own killed run made are named here: a run of the
# real script by someone else at the same moment is not touched.
mine() { # the unit names this check's killed run left behind
	[ -f "$T/units" ] && cat "$T/units"
	return 0
}
cleanup() {
	[ -n "$RUNPID" ] && kill -9 "$RUNPID" >/dev/null 2>&1 || true
	for u in $(mine) "$STALE_UNIT"; do
		systemctl --user stop "$u.service" >/dev/null 2>&1 || true
		sock=$(printf '%s' "$u" | sed 's/^mw-seat-tmux-/mw-seat-/')
		tmux -L "$sock" kill-server >/dev/null 2>&1 || true
		rm -f "$SOCKDIR/$sock"
	done
	tmux -L "$STALE_SOCK" kill-server >/dev/null 2>&1 || true
	rm -f "$SOCKDIR/$STALE_SOCK"
	rm -rf "$T"
}
trap cleanup EXIT INT TERM

test_units() { # list the loaded mw-seat-tmux-*-test-* units
	systemctl --user list-units 'mw-seat-tmux-*-test-*' --all --plain --no-legend 2>/dev/null | awk '{print $1}' | sed 's/\.service$//'
}

before=$(test_units | sort)

# --- 1. kill a run mid-scenario ------------------------------------------------
sh "$RESPAWN" >"$T/run.out" 2>&1 &
RUNPID=$!

# The first unit appears as soon as the script has set up its first scenario.
printf '%s\n' "$before" >"$T/before"
i=0
while [ "$i" -lt 30 ]; do
	fresh=$(test_units | sort | grep -vxF -f "$T/before" 2>/dev/null || true)
	[ -n "$fresh" ] && break
	sleep 1
	i=$((i + 1))
done
[ -n "${fresh:-}" ] || fail "the respawn check never started a test unit within 30s; its output: $(cat "$T/run.out")"
kill -9 "$RUNPID"
wait "$RUNPID" 2>/dev/null || true
RUNPID=
printf '%s\n' "$fresh" >"$T/units"

for u in $fresh; do
	exec_prop=$(systemctl --user show "$u.service" -p ExecStart --value 2>/dev/null)
	case $exec_prop in
	*" -L "*) ;;
	*) fail "$u: its ExecStart has no -L, so it can reach the default tmux socket: $exec_prop" ;;
	esac
	if printf '%s\n' "$exec_prop" | grep -Eq '(^|[^/])tmux '; then
		fail "$u: its ExecStart runs tmux through PATH, not by absolute path, so a missing wrapper dir falls through to the real tmux: $exec_prop"
	fi
	rt=$(systemctl --user show "$u.service" -p RuntimeMaxUSec --value 2>/dev/null)
	case $rt in
	"" | infinity) fail "$u: has no RuntimeMaxSec, so a killed run leaves it for ever (RuntimeMaxUSec=$rt)" ;;
	esac
	burst=$(systemctl --user show "$u.service" -p StartLimitBurst --value 2>/dev/null)
	interval=$(systemctl --user show "$u.service" -p StartLimitIntervalUSec --value 2>/dev/null)
	if [ "$interval" = 0 ] || [ "${burst:-0}" -ge 100 ]; then
		fail "$u: RuntimeMaxSec alone is not a bound under Restart=always (the unit restarts after each timeout); StartLimitBurst=$burst StartLimitIntervalUSec=$interval lets it restart for ever"
	fi
done

# --- 2. the next run's start sweeps stale test units and sockets ----------------
systemd-run --user --collect --unit="$STALE_UNIT" --service-type=simple /bin/sleep 600 >/dev/null
tmux -L "$STALE_SOCK" new-session -d -s 0 || fail "could not start a stale tmux server to sweep"

MW_SEAT_RESPAWN_SWEEP_ONLY=1 sh "$RESPAWN" >"$T/sweep.out" 2>&1 || fail "the sweep-only run failed: $(cat "$T/sweep.out")"

if test_units | grep -qxF "$STALE_UNIT"; then
	fail "the next run's start did not sweep the stale unit $STALE_UNIT"
fi
if tmux -L "$STALE_SOCK" has-session >/dev/null 2>&1; then
	fail "the next run's start did not sweep the stale tmux server on $STALE_SOCK"
fi
# ... and it left alone the (fresh, maybe someone's live) units of the killed run.
for u in $fresh; do
	test_units | grep -qxF "$u" || fail "the sweep stopped $u, a unit of a run only seconds old"
done

echo "OK: a SIGKILLed $RESPAWN leaves only units that run tmux by absolute path with -L and are bounded by RuntimeMaxSec and a start limit; the next run's start sweeps stale test units and sockets"
