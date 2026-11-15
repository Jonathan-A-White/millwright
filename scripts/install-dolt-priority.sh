#!/bin/sh
# Install the CPUWeight drop-in for the user's dolt-beads.service (mw-gq6.1309).
#
#   sh scripts/install-dolt-priority.sh             install, reload, restart, read the weight back
#   sh scripts/install-dolt-priority.sh --dry-run   say what would happen; change nothing
#
# Copies contrib/dolt-beads/priority.conf to
# ${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/dolt-beads.service.d/, runs
# `systemctl --user daemon-reload` and `systemctl --user restart dolt-beads`,
# then reads `systemctl --user show dolt-beads -p CPUWeight` and prints it. It exits
# non-zero when the weight read back is not the one the drop-in sets (the cpu
# controller is not delegated to the user manager, or the unit is not there).
# Run it on the host that serves the beads. Running it again changes nothing
# but the restart.

set -eu

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC=$REPO_ROOT/contrib/dolt-beads/priority.conf
DIR=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/dolt-beads.service.d

fail() {
	echo "install-dolt-priority: $*" >&2
	exit 1
}

dry=
case ${1:-} in
"") ;;
--dry-run) dry=1 ;;
-h | --help)
	sed -n '2,14p' "$0"
	exit 0
	;;
*) fail "unknown argument: $1 (only --dry-run)" ;;
esac

[ -f "$SRC" ] || fail "$SRC does not exist"
want=$(sed -n 's/^CPUWeight=\([0-9][0-9]*\)$/\1/p' "$SRC")
[ -n "$want" ] || fail "$SRC has no CPUWeight=<n> line"

if [ -n "$dry" ]; then
	echo "would copy $SRC to $DIR/priority.conf"
	echo "would run: systemctl --user daemon-reload"
	echo "would run: systemctl --user restart dolt-beads"
	echo "would read: systemctl --user show dolt-beads -p CPUWeight (expect CPUWeight=$want)"
	exit 0
fi

mkdir -p "$DIR"
cp "$SRC" "$DIR/priority.conf"
echo "installed $DIR/priority.conf"
systemctl --user daemon-reload
systemctl --user restart dolt-beads
got=$(systemctl --user show dolt-beads -p CPUWeight)
echo "$got"
[ "$got" = "CPUWeight=$want" ] || fail "read back '$got', expected CPUWeight=$want: is the cpu controller delegated to the user manager, and is dolt-beads.service installed?"
