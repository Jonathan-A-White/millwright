#!/bin/sh
# Run scripts/install-units.sh against a stand-in systemctl and hold what it does
# to what its header promises. Reads only this repository and a temporary
# directory it makes and removes; it never touches the real host's user manager:
# the script runs with an empty environment, a PATH holding only stand-ins and a
# short list of real coreutils (so the real systemctl and loginctl cannot be
# reached), and a HOME inside the temporary directory.
#
# What it holds: with no unit named it lists every pair in contrib/systemd/ and
# changes nothing (--enable then is refused); naming a pair leaves two symlinks
# into this checkout, calls `systemctl --user daemon-reload` once and never
# `enable`; --enable adds one `enable --now <timer>`; a second run changes
# nothing; --dry-run changes nothing and calls nothing that changes anything;
# an unknown unit, a copy in the way, a link elsewhere and a missing systemctl
# each stop the run with nothing changed; XDG_CONFIG_HOME is honoured; the
# linger line is printed and loginctl is never called; the README's copy-paste
# install blocks are gone in favour of the script.
#
# shellcheck is run over both scripts when it is installed, and skipped when not.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=scripts/install-units.sh
UNITDIR=contrib/systemd

cd "$REPO_ROOT"

fail() {
	echo "check-install-units: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
sh -n "$SCRIPT" || fail "$SCRIPT does not parse"
sh -n scripts/check-install-units.sh || fail "scripts/check-install-units.sh does not parse"

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(MW_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

# The pairs the rig ships, from the directory.
PAIRS=""
for t in "$UNITDIR"/*.timer; do
	n=$(basename "$t" .timer)
	[ -f "$UNITDIR/$n.service" ] || fail "$t has no $n.service beside it"
	PAIRS="$PAIRS $n"
done
[ "$(echo $PAIRS | wc -w)" -ge 5 ] || fail "expected the five timer pairs in $UNITDIR, found:$PAIRS"

# --- 1. what it may run --------------------------------------------------------
# loginctl appears only inside text it prints, never at the start of a command.
if grep -Eq '^[[:space:]]*(run |command |exec )?loginctl' "$SCRIPT"; then
	fail "$SCRIPT runs loginctl: the linger line is a hand step"
fi
if grep -Eq 'sudo|\bcp ' "$SCRIPT"; then
	fail "$SCRIPT copies a file or runs sudo: it links, and needs no root"
fi

# --- 2. the sandbox ------------------------------------------------------------
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
SH=$(command -v sh)
STAND=$T/stand
REAL=$T/real
W=$T/w
mkdir -p "$STAND" "$REAL"

# The only real programs the script may reach: coreutils.
for c in basename cat cut dirname head id ln ls mkdir readlink rm sed sort tr wc; do
	for d in /usr/bin /bin; do
		if [ -x "$d/$c" ]; then ln -s "$d/$c" "$REAL/$c"; break; fi
	done
done

standin() { # <dir> <name>, the body on stdin
	cat >"$1/$2"
	chmod +x "$1/$2"
}

# systemctl logs every call. `is-active` answers from FAKE_ACTIVE, a list of
# timer names that are active; nothing else it is asked does anything.
standin "$STAND" systemctl <<'EOF'
#!/bin/sh
echo "systemctl $*" >>"$FAKE_CALLS"
[ "$1" = --user ] || exit 99
shift
case $1 in
is-active)
	shift
	for a in "$@"; do
		case " $FAKE_ACTIVE " in *" $a "*) exit 0 ;; esac
	done
	exit 3 ;;
daemon-reload | enable) exit 0 ;;
*) exit 99 ;;
esac
EOF
# loginctl is only here to be caught if the script runs it.
standin "$STAND" loginctl <<'EOF'
#!/bin/sh
echo "loginctl $*" >>"$FAKE_CALLS"
exit 99
EOF

# world: a fresh host: an empty HOME, the stand-ins on PATH.
world() {
	rm -rf "$W"
	mkdir -p "$W/bin" "$W/home" "$W/xdg"
	for c in "$STAND"/*; do ln -s "$c" "$W/bin/$(basename "$c")"; done
	: >"$W/calls"
	ENVX=""
	USERDIR=$W/home/.config/systemd/user
}
# snap: everything under the world but the call log.
snap() {
	(
		cd "$W"
		find . ! -path ./calls 2>/dev/null | sort
		find . -type l -exec sh -c 'for l; do echo "$l -> $(readlink "$l")"; done' sh {} + 2>/dev/null | sort
	)
}
# run <arg>...: run the script with those arguments; OUT is what it printed, RC its status.
run() {
	: >"$W/calls"
	RC=0
	# shellcheck disable=SC2086
	OUT=$(cd "$W" && env -i PATH="$W/bin:$REAL" HOME="$W/home" USER=tester FAKE_CALLS="$W/calls" \
		$ENVX "$SH" "$REPO_ROOT/$SCRIPT" "$@" 2>&1) || RC=$?
}

# --- assertions ------------------------------------------------------------------
NAME=""
ok() { echo "ok: $NAME"; }
has() { printf '%s\n' "$OUT" | grep -Fq -- "$1" || fail "$NAME: the output lacks: $1
$OUT"; }
lacks() { if printf '%s\n' "$OUT" | grep -Fq -- "$1"; then fail "$NAME: the output has: $1
$OUT"; fi; }
rc_is() { [ "$RC" = "$1" ] || fail "$NAME: exit status $RC, wanted $1
$OUT"; }
calls_are() { # <n> <pattern>: exactly n calls match
	got=$(grep -Ec -- "$2" "$W/calls" || true)
	[ "$got" = "$1" ] || fail "$NAME: $got calls matched '$2', wanted $1
$(cat "$W/calls")"
}
not_called() { calls_are 0 "$1"; }
# The calls that would change something; loginctl of any kind is one.
MUTATING='^(systemctl --user (daemon-reload|enable)|loginctl)'
only_reads() { not_called "$MUTATING"; }
unchanged() { # <before>
	[ "$(snap)" = "$1" ] || fail "$NAME: the run changed the world:
$(snap | diff - "$T/before" || true)"
}
is_link_to_rig() { # <path> <unit file>
	[ -L "$1" ] || fail "$NAME: $1 is not a symlink"
	[ "$(readlink "$1")" = "$REPO_ROOT/$UNITDIR/$2" ] || fail "$NAME: $1 links to $(readlink "$1"), wanted $REPO_ROOT/$UNITDIR/$2"
	[ -f "$1" ] || fail "$NAME: $1 links to nothing"
}
nlinks() { find "$1" -type l 2>/dev/null | wc -l | tr -d ' '; }

# --- 3. --help and a flag it does not know ------------------------------------------
NAME="--help"
world
run --help
rc_is 0
has '--enable'
has '--dry-run'
[ ! -s "$W/calls" ] || fail "$NAME: --help ran a command: $(cat "$W/calls")"
ok

NAME="an unknown flag"
world
run --frobnicate
rc_is 2
has 'unknown'
has 'Usage'
only_reads
ok

# --- 4. no unit named: the list, and nothing changed ------------------------------------
NAME="no unit named lists every pair and changes nothing"
world
ENVX="FAKE_ACTIVE=mw-dispatch.timer"
snap >"$T/before"
run
rc_is 0
for n in $PAIRS; do has "$n"; done
has 'INSTALLED'
printf '%s\n' "$OUT" | grep -E '^mw-dispatch +no +yes$' >/dev/null || fail "$NAME: mw-dispatch is not shown as not installed and active
$OUT"
printf '%s\n' "$OUT" | grep -E '^mw-health +no +no$' >/dev/null || fail "$NAME: mw-health is not shown as not installed and inactive
$OUT"
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="--enable with no unit named is refused"
world
snap >"$T/before"
run --enable
rc_is 2
has 'needs a unit'
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="--dry-run with no unit named lists and changes nothing"
world
snap >"$T/before"
run --dry-run
rc_is 0
has 'INSTALLED'
only_reads
unchanged "$(cat "$T/before")"
ok

# --- 5. one pair: two symlinks, one reload, no enable --------------------------------------
NAME="mw-health links two files, reloads once, enables never"
world
run mw-health
rc_is 0
is_link_to_rig "$USERDIR/mw-health.service" mw-health.service
is_link_to_rig "$USERDIR/mw-health.timer" mw-health.timer
[ "$(nlinks "$W/home")" = 2 ] || fail "$NAME: $(nlinks "$W/home") symlinks, wanted 2"
calls_are 1 '^systemctl --user daemon-reload$'
calls_are 0 'enable'
calls_are 0 '^loginctl'
has 'not enabled'
has 'loginctl enable-linger tester'
ok

NAME="a linked pair is listed as linked"
ENVX="FAKE_ACTIVE=mw-health.timer"
run
printf '%s\n' "$OUT" | grep -E '^mw-health +linked +yes$' >/dev/null || fail "$NAME: mw-health is not shown as linked and active
$OUT"
printf '%s\n' "$OUT" | grep -E '^mw-dispatch +no +no$' >/dev/null || fail "$NAME: mw-dispatch is not shown as not installed
$OUT"
ok

NAME="a second run changes nothing but reloads again"
ENVX=""
snap >"$T/before"
run mw-health
rc_is 0
has 'skip'
calls_are 1 '^systemctl --user daemon-reload$'
calls_are 0 'enable'
unchanged "$(cat "$T/before")"
ok

# --- 6. --enable ------------------------------------------------------------------------
NAME="--enable calls enable --now once, after the reload"
world
run --enable mw-health
rc_is 0
is_link_to_rig "$USERDIR/mw-health.service" mw-health.service
is_link_to_rig "$USERDIR/mw-health.timer" mw-health.timer
calls_are 1 '^systemctl --user daemon-reload$'
calls_are 1 '^systemctl --user enable --now mw-health.timer$'
calls_are 1 'enable'
calls_are 0 '^loginctl'
[ "$(grep -n 'daemon-reload' "$W/calls" | cut -d: -f1)" -lt "$(grep -n 'enable' "$W/calls" | cut -d: -f1)" ] || fail "$NAME: enable came before the reload"
lacks 'not enabled'
has 'loginctl enable-linger tester'
ok

NAME="--enable is accepted after the unit name"
world
run mw-health --enable
rc_is 0
calls_are 1 '^systemctl --user enable --now mw-health.timer$'
ok

# --- 7. several pairs, and the suffixes ---------------------------------------------------
NAME="two pairs named: four links, one reload, one enable for both timers"
world
run --enable mw-millhand-tick mw-millhand-review.timer
rc_is 0
for n in mw-millhand-tick mw-millhand-review; do
	is_link_to_rig "$USERDIR/$n.service" "$n.service"
	is_link_to_rig "$USERDIR/$n.timer" "$n.timer"
done
[ "$(nlinks "$W/home")" = 4 ] || fail "$NAME: $(nlinks "$W/home") symlinks, wanted 4"
calls_are 1 '^systemctl --user daemon-reload$'
calls_are 1 '^systemctl --user enable --now mw-millhand-tick.timer mw-millhand-review.timer$'
ok

NAME="every pair the rig ships can be linked"
world
# shellcheck disable=SC2086
run $PAIRS
rc_is 0
want=$(($(echo $PAIRS | wc -w) * 2))
[ "$(nlinks "$W/home")" = "$want" ] || fail "$NAME: $(nlinks "$W/home") symlinks, wanted $want"
calls_are 1 '^systemctl --user daemon-reload$'
calls_are 0 'enable'
has 'ln -s'
has 'contrib/mail-notify'
has 'contrib/health/mw-health.sh'
ok

# --- 8. --dry-run ---------------------------------------------------------------------------
NAME="--dry-run --enable changes nothing and calls nothing that would"
world
snap >"$T/before"
run --dry-run --enable mw-health
rc_is 0
has 'would link'
has 'would run: systemctl --user daemon-reload'
has 'would run: systemctl --user enable --now mw-health.timer'
has 'loginctl enable-linger tester'
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="--dry-run works with no systemctl at all"
world
rm "$W/bin/systemctl"
snap >"$T/before"
run --dry-run mw-health
rc_is 0
has 'would link'
unchanged "$(cat "$T/before")"
ok

# --- 9. the places it may put things ---------------------------------------------------------
NAME="XDG_CONFIG_HOME is honoured"
world
ENVX="XDG_CONFIG_HOME=$W/xdg"
run mw-health
rc_is 0
is_link_to_rig "$W/xdg/systemd/user/mw-health.service" mw-health.service
is_link_to_rig "$W/xdg/systemd/user/mw-health.timer" mw-health.timer
[ ! -e "$W/home/.config" ] || fail "$NAME: it wrote under HOME/.config as well"
ok

# --- 10. what it refuses, with nothing changed --------------------------------------------------
NAME="an unknown unit is refused before anything is linked"
world
snap >"$T/before"
run mw-health mw-frobnicate
rc_is 1
has 'no such unit: mw-frobnicate'
has 'mw-dispatch'
[ ! -s "$W/calls" ] || fail "$NAME: it ran a command: $(cat "$W/calls")"
unchanged "$(cat "$T/before")"
ok

NAME="a copy in the way is never overwritten, and nothing else is linked"
world
mkdir -p "$USERDIR"
echo "a hand copy" >"$USERDIR/mw-health.timer"
snap >"$T/before"
run mw-dispatch mw-health
rc_is 1
has 'mw-health.timer'
has 'not a link'
has 'Nothing was changed'
[ "$(cat "$USERDIR/mw-health.timer")" = "a hand copy" ] || fail "$NAME: the copy was changed"
[ ! -s "$W/calls" ] || fail "$NAME: it ran a command: $(cat "$W/calls")"
unchanged "$(cat "$T/before")"
ok

NAME="a link somewhere else is never replaced"
world
mkdir -p "$USERDIR"
ln -s /elsewhere/mw-health.service "$USERDIR/mw-health.service"
snap >"$T/before"
run --enable mw-health
rc_is 1
has '/elsewhere/mw-health.service'
has 'Nothing was changed'
[ "$(readlink "$USERDIR/mw-health.service")" = /elsewhere/mw-health.service ] || fail "$NAME: the link was changed"
[ ! -s "$W/calls" ] || fail "$NAME: it ran a command: $(cat "$W/calls")"
unchanged "$(cat "$T/before")"
ok

NAME="a host without systemctl is refused before anything is linked"
world
rm "$W/bin/systemctl"
snap >"$T/before"
run mw-health
rc_is 1
has 'systemctl'
unchanged "$(cat "$T/before")"
ok

# --- 11. the README points here ----------------------------------------------------------------------
NAME="the README's copy-paste install blocks are gone"
if grep -n 'cp contrib/systemd/' README.md; then
	fail "$NAME: README.md still copies units by hand; it should name $SCRIPT"
fi
[ "$(grep -c "$SCRIPT" README.md)" -ge 3 ] || fail "$NAME: README.md names $SCRIPT fewer than three times (one line for each of its three install blocks)"
ok

# --- 12. shellcheck, where it exists ---------------------------------------------------------------------
if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT" scripts/check-install-units.sh || fail "shellcheck found something"
	echo "ok: shellcheck"
else
	echo "check-install-units: shellcheck is not installed here, so the scripts were NOT linted" >&2
fi

echo "OK: $SCRIPT links, reloads, enables only on --enable, changes nothing on --dry-run or on a refusal, and runs no loginctl"
