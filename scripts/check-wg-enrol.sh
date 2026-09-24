#!/bin/sh
# Run contrib/wg-enrol against temp files and hold it to what its header
# promises. Reads only this repository and a temporary directory it makes
# and removes; WG_SYNC=0 in most scenarios so no real wg or wg-quick is
# needed and no root is required. The one scenario that checks the syncconf
# call puts fake wg and wg-quick scripts on PATH instead of reaching real
# ones.
#
# The scenarios are the story's acceptance criteria:
#   a. laptop, desktop and a third name get 10.88.0.2, .3 and .4; the peer
#      block, the hosts line and the printed client config are all correct
#   b. a second run with the same name and key changes nothing and prints
#      the same config again
#   c. a bad name, a bad key and a taken name are refused, unchanged
#   d. --remove deletes the peer block and its hosts line
#   e. with WG_SYNC unset, wg syncconf wg0 <(wg-quick strip wg0) runs after
#      every change

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=$REPO_ROOT/contrib/wg-enrol

cd "$REPO_ROOT"

fail() {
	echo "check-wg-enrol: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
bash -n "$SCRIPT" || fail "$SCRIPT does not parse"

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(WG_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM

KEY1="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
KEY2="BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="
KEY3="CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC="
HUBKEY="HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH="

new_files() {
	CONF=$T/wg0-$1.conf
	PUB=$T/vps-$1.pub
	HOSTS=$T/hosts-$1
	cat >"$CONF" <<EOF
[Interface]
Address = 10.88.0.1/24
ListenPort = 51820
EOF
	printf '%s\n' "$HUBKEY" >"$PUB"
	cat >"$HOSTS" <<EOF
127.0.0.1 localhost
10.88.0.1 vps.mw # mw-wg
EOF
}

run() {
	WG_CONF="$CONF" WG_PUB="$PUB" WG_HOSTS="$HOSTS" WG_ENDPOINT="203.0.113.9:51820" WG_SYNC=0 "$SCRIPT" "$@"
}

# --- a. laptop, desktop and a third name get .2, .3 and .4 -------------------
NAME="laptop, desktop and a third name get 10.88.0.2, .3 and .4"
new_files a
RC=0
OUT=$(run laptop "$KEY1" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: laptop enrol exited $RC: $OUT"
grep -Fq 'AllowedIPs = 10.88.0.2/32' "$CONF" || fail "$NAME: no AllowedIPs 10.88.0.2/32 in conf: $(cat "$CONF")"
grep -Fq 'PublicKey = '"$KEY1" "$CONF" || fail "$NAME: no PublicKey line for laptop: $(cat "$CONF")"
grep -Fq 'PersistentKeepalive = 25' "$CONF" || fail "$NAME: no PersistentKeepalive: $(cat "$CONF")"
grep -Fxq '10.88.0.2 laptop.mw # mw-wg' "$HOSTS" || fail "$NAME: no hosts line for laptop: $(cat "$HOSTS")"
printf '%s\n' "$OUT" | grep -Fq 'Address = 10.88.0.2/24' || fail "$NAME: config lacks Address 10.88.0.2/24: $OUT"
printf '%s\n' "$OUT" | grep -Fq "PublicKey = $HUBKEY" || fail "$NAME: config lacks the hub key: $OUT"
printf '%s\n' "$OUT" | grep -Fq 'Endpoint = 203.0.113.9:51820' || fail "$NAME: config lacks the endpoint: $OUT"
printf '%s\n' "$OUT" | grep -Fq 'AllowedIPs = 10.88.0.0/24' || fail "$NAME: config lacks AllowedIPs 10.88.0.0/24: $OUT"

RC=0
OUT=$(run desktop "$KEY2" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: desktop enrol exited $RC: $OUT"
grep -Fq 'AllowedIPs = 10.88.0.3/32' "$CONF" || fail "$NAME: no AllowedIPs 10.88.0.3/32 in conf: $(cat "$CONF")"
grep -Fxq '10.88.0.3 desktop.mw # mw-wg' "$HOSTS" || fail "$NAME: no hosts line for desktop: $(cat "$HOSTS")"
printf '%s\n' "$OUT" | grep -Fq 'Address = 10.88.0.3/24' || fail "$NAME: config lacks Address 10.88.0.3/24: $OUT"

RC=0
OUT=$(run pi "$KEY3" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: third name enrol exited $RC: $OUT"
grep -Fq 'AllowedIPs = 10.88.0.4/32' "$CONF" || fail "$NAME: no AllowedIPs 10.88.0.4/32 in conf: $(cat "$CONF")"
grep -Fxq '10.88.0.4 pi.mw # mw-wg' "$HOSTS" || fail "$NAME: no hosts line for pi: $(cat "$HOSTS")"
printf '%s\n' "$OUT" | grep -Fq 'Address = 10.88.0.4/24' || fail "$NAME: config lacks Address 10.88.0.4/24: $OUT"
echo "ok: $NAME"

# --- b. a second run with the same name and key changes nothing --------------
NAME="a second run with the same name and key is idempotent"
before_conf=$(cat "$CONF")
before_hosts=$(cat "$HOSTS")
RC=0
OUT2=$(run laptop "$KEY1" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: re-run exited $RC: $OUT2"
[ "$(cat "$CONF")" = "$before_conf" ] || fail "$NAME: conf changed on re-run"
[ "$(cat "$HOSTS")" = "$before_hosts" ] || fail "$NAME: hosts changed on re-run"
printf '%s\n' "$OUT2" | grep -Fq 'Address = 10.88.0.2/24' || fail "$NAME: re-run config lacks Address 10.88.0.2/24: $OUT2"
echo "ok: $NAME"

# --- c. a bad name, a bad key and a taken name are refused -------------------
NAME="a bad name is refused unchanged"
before_conf=$(cat "$CONF")
RC=0
ERR=$(run "Not_OK" "$KEY3" 2>&1) || RC=$?
[ "$RC" != 0 ] || fail "$NAME: exited 0"
lines=$(printf '%s\n' "$ERR" | grep -c .)
[ "$lines" = 1 ] || fail "$NAME: $lines lines on stderr, wanted 1: $ERR"
[ "$(cat "$CONF")" = "$before_conf" ] || fail "$NAME: conf changed"
echo "ok: $NAME"

NAME="a bad key is refused unchanged"
before_conf=$(cat "$CONF")
RC=0
ERR=$(run newhost "not-a-key" 2>&1) || RC=$?
[ "$RC" != 0 ] || fail "$NAME: exited 0"
lines=$(printf '%s\n' "$ERR" | grep -c .)
[ "$lines" = 1 ] || fail "$NAME: $lines lines on stderr, wanted 1: $ERR"
[ "$(cat "$CONF")" = "$before_conf" ] || fail "$NAME: conf changed"
echo "ok: $NAME"

NAME="a taken name (different key) is refused unchanged"
before_conf=$(cat "$CONF")
RC=0
ERR=$(run laptop "$KEY3" 2>&1) || RC=$?
[ "$RC" != 0 ] || fail "$NAME: exited 0"
lines=$(printf '%s\n' "$ERR" | grep -c .)
[ "$lines" = 1 ] || fail "$NAME: $lines lines on stderr, wanted 1: $ERR"
[ "$(cat "$CONF")" = "$before_conf" ] || fail "$NAME: conf changed"
echo "ok: $NAME"

# --- d. --remove deletes the peer block and its hosts line -------------------
NAME="--remove deletes the peer block and its hosts line"
RC=0
OUT=$(run --remove pi 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT"
grep -Fq 'pi (enrolled' "$CONF" && fail "$NAME: peer comment still in conf: $(cat "$CONF")"
grep -Fq 'AllowedIPs = 10.88.0.4/32' "$CONF" && fail "$NAME: peer block still in conf: $(cat "$CONF")"
grep -Fxq '10.88.0.4 pi.mw # mw-wg' "$HOSTS" && fail "$NAME: hosts line still present: $(cat "$HOSTS")"
grep -Fq 'AllowedIPs = 10.88.0.2/32' "$CONF" || fail "$NAME: unrelated laptop block was removed too"
echo "ok: $NAME"

RC=0
ERR=$(run --remove pi 2>&1) || RC=$?
[ "$RC" != 0 ] || fail "$NAME: removing an absent peer exited 0"
echo "ok: removing an already-removed peer is refused"

# --- e. wg syncconf runs after every change, unless WG_SYNC=0 ----------------
NAME="wg syncconf wg0 <(wg-quick strip wg0) runs after every change"
new_files e
FAKE_CALLS=$T/calls
export FAKE_CALLS
: >"$FAKE_CALLS"
mkdir -p "$T/bin"
cat >"$T/bin/wg" <<'EOF'
#!/bin/sh
echo "wg $*" >>"$FAKE_CALLS"
EOF
cat >"$T/bin/wg-quick" <<'EOF'
#!/bin/sh
echo "wg-quick $*" >>"$FAKE_CALLS"
EOF
chmod +x "$T/bin/wg" "$T/bin/wg-quick"
RC=0
OUT=$(PATH="$T/bin:$PATH" WG_CONF="$CONF" WG_PUB="$PUB" WG_HOSTS="$HOSTS" WG_ENDPOINT="203.0.113.9:51820" "$SCRIPT" laptop "$KEY1" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT"
grep -Fq 'wg syncconf wg0' "$FAKE_CALLS" || fail "$NAME: wg syncconf was not called: $(cat "$FAKE_CALLS")"
grep -Fq 'wg-quick strip wg0' "$FAKE_CALLS" || fail "$NAME: wg-quick strip was not called: $(cat "$FAKE_CALLS")"
echo "ok: $NAME"

echo "OK: contrib/wg-enrol assigns addresses, writes peer blocks and hosts lines, prints client config, is idempotent, refuses bad input, removes peers and syncconfs the running tunnel"
