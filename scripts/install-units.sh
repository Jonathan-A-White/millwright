#!/bin/sh
# Put a host's timer units where systemd --user finds them, from one command.
#
#   sh scripts/install-units.sh                        list the pairs; change nothing
#   sh scripts/install-units.sh mw-health              link one pair, daemon-reload
#   sh scripts/install-units.sh --enable mw-dispatch mw-health
#   sh scripts/install-units.sh --dry-run mw-health    say what would happen
#
# A unit name is a timer/service pair in contrib/systemd/, named without its
# suffix (mw-dispatch, mw-millhand-tick, mw-millhand-review, mw-mail-notify,
# mw-health). Each named pair is LINKED, not copied, into
# ${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user, so a landing that changes a
# unit needs only `systemctl --user daemon-reload` on a host. Run it from the
# rig's own checkout: the links point there.
#
# A live timer starts real work and spends fuel, so a timer is enabled ONLY when
# --enable is given, and then with `systemctl --user enable --now`. Without it
# the pairs are linked and the host is told the command to arm them.
#
# With no unit named it lists the pairs and, for each, whether it is installed
# and whether its timer is active, and changes nothing.
#
# It never runs `loginctl enable-linger` (that needs root): it prints the line as
# a hand step. A file already in the way that is not this rig's link (a copy, or
# a link somewhere else) is never overwritten: it is named and the run stops
# before changing anything.
#
# Settings: XDG_CONFIG_HOME (where the user's systemd units live).

set -eu

DRY=0
ENABLE=0

usage() {
	cat <<'EOF'
Usage: sh install-units.sh [--enable] [--dry-run] [unit-name]...
       sh install-units.sh --help

Links the named timer/service pairs from contrib/systemd/ into
~/.config/systemd/user (or $XDG_CONFIG_HOME/systemd/user) and runs
`systemctl --user daemon-reload`. With no unit named, lists the pairs and
which are installed and active, and changes nothing.

  --enable    also `systemctl --user enable --now` each named timer; without
              it no timer is enabled
  --dry-run   print what would be done; change nothing
  --help      print this

`loginctl enable-linger` is printed as a hand step and never run.
EOF
}

die() {
	echo "install-units.sh: $*" >&2
	exit 1
}

# Options first, the rest are unit names.
UNITS=""
for arg in "$@"; do
	case $arg in
	--enable) ENABLE=1 ;;
	--dry-run) DRY=1 ;;
	--help | -h) usage; exit 0 ;;
	-*) echo "install-units.sh: unknown option: $arg" >&2; usage >&2; exit 2 ;;
	*) UNITS="$UNITS $arg" ;;
	esac
done

[ -n "${HOME:-}" ] || die "HOME is not set"
SELF_DIR=$(cd "$(dirname "$0")" && pwd)
RIG=$(cd "$SELF_DIR/.." && pwd)
SRC=$RIG/contrib/systemd
DEST=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
[ -d "$SRC" ] || die "$SRC does not exist: run this from the rig's checkout"

# Every pair the rig ships: a .timer with a .service beside it.
PAIRS=""
for t in "$SRC"/*.timer; do
	[ -f "$t" ] || continue
	n=$(basename "$t" .timer)
	[ -f "$SRC/$n.service" ] && PAIRS="$PAIRS $n"
done
[ -n "$PAIRS" ] || die "$SRC holds no timer/service pair"

is_pair() { case " $PAIRS " in *" $1 "*) return 0 ;; esac; return 1; }

# state <name>: how a pair stands in DEST: linked, copied, partial or no.
state() {
	linked=0
	present=0
	for f in "$1.service" "$1.timer"; do
		if [ -L "$DEST/$f" ]; then
			present=$((present + 1))
			[ "$(readlink "$DEST/$f")" = "$SRC/$f" ] && linked=$((linked + 1))
		elif [ -e "$DEST/$f" ]; then
			present=$((present + 1))
		fi
	done
	if [ "$linked" = 2 ]; then echo linked
	elif [ "$present" = 0 ]; then echo no
	elif [ "$present" = 2 ]; then echo copied
	else echo partial
	fi
}

# --- no unit named: the list ---------------------------------------------------
if [ -z "$UNITS" ]; then
	[ "$ENABLE" = 0 ] || { echo "install-units.sh: --enable needs a unit named" >&2; usage >&2; exit 2; }
	printf '%-22s %-10s %s\n' UNIT INSTALLED ACTIVE
	for n in $PAIRS; do
		active=unknown
		if command -v systemctl >/dev/null 2>&1; then
			if systemctl --user is-active --quiet "$n.timer" >/dev/null 2>&1; then active=yes; else active=no; fi
		fi
		printf '%-22s %-10s %s\n' "$n" "$(state "$n")" "$active"
	done
	echo
	echo "Name one or more to link them: sh scripts/install-units.sh <unit-name>..."
	echo "(--enable also arms the timer; --dry-run says what would happen.)"
	exit 0
fi

# --- the pairs named: check all of it before changing any of it -------------------
NAMED=""
for u in $UNITS; do
	n=${u%.timer}
	n=${n%.service}
	is_pair "$n" || die "no such unit: $u (the pairs are:$PAIRS)"
	case " $NAMED " in *" $n "*) ;; *) NAMED="$NAMED $n" ;; esac
done

for n in $NAMED; do
	for f in "$n.service" "$n.timer"; do
		if [ -L "$DEST/$f" ]; then
			[ "$(readlink "$DEST/$f")" = "$SRC/$f" ] ||
				die "$DEST/$f is a link to $(readlink "$DEST/$f"), not to this rig; remove it by hand first. Nothing was changed"
		elif [ -e "$DEST/$f" ]; then
			die "$DEST/$f exists and is not a link (a copy?); remove it by hand first. Nothing was changed"
		fi
	done
done

if [ "$DRY" = 0 ]; then
	command -v systemctl >/dev/null 2>&1 || die "systemctl is not on PATH; nothing was changed"
fi

run() { # <command>...: run it, or say it would be
	if [ "$DRY" = 1 ]; then echo "would run: $*"; else echo "run: $*"; "$@"; fi
}

# --- link, reload, and enable only when asked -------------------------------------
if [ "$DRY" = 1 ]; then echo "would make: $DEST"; else mkdir -p "$DEST"; fi
TIMERS=""
for n in $NAMED; do
	for f in "$n.service" "$n.timer"; do
		if [ -L "$DEST/$f" ]; then
			echo "skip: $DEST/$f already links to $SRC/$f"
		elif [ "$DRY" = 1 ]; then
			echo "would link: $DEST/$f -> $SRC/$f"
		else
			ln -s "$SRC/$f" "$DEST/$f"
			echo "linked: $DEST/$f -> $SRC/$f"
		fi
	done
	TIMERS="$TIMERS $n.timer"
done

run systemctl --user daemon-reload
if [ "$ENABLE" = 1 ]; then
	# shellcheck disable=SC2086 # the timer names hold no spaces; word splitting is the point
	run systemctl --user enable --now $TIMERS
else
	echo "not enabled: nothing is armed. To arm, run again with --enable, or:"
	echo "  systemctl --user enable --now$TIMERS"
fi

# --- what this script leaves to the host -------------------------------------------
who=${USER:-$(id -un)}
echo
echo "Hand steps (this script runs none of them):"
echo "  [linger]   a user timer fires with nobody logged in only after (needs root):"
echo "               loginctl enable-linger $who"
echo "             check: loginctl show-user $who -p Linger"
if [ ! -f "$HOME/.config/mw/dispatch.env" ]; then
	echo "  [env]      $HOME/.config/mw/dispatch.env is missing; the units get a bare PATH without it."
	echo "             One line: PATH=<where mw, bd, git, tmux, claude and go are>; see README.md"
fi
for n in $NAMED; do
	case $n in
	mw-mail-notify) script=$RIG/contrib/mail-notify; cmd=mw-mail-notify ;;
	mw-health) script=$RIG/contrib/health/mw-health.sh; cmd=mw-health ;;
	*) continue ;;
	esac
	echo "  [$cmd] the service runs \`$cmd\` from PATH; link the script in:"
	echo "               ln -s \"$script\" ~/.local/bin/$cmd"
done
