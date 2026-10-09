#!/bin/sh
# Run contrib/boost-bootstrap.sh against stand-in commands and a fake root, and
# hold it to what its header promises. Reads only this repository and a
# temporary directory it makes and removes; the script runs with a PATH holding
# only stand-ins and a short list of real coreutils (so the real apt, systemctl,
# wg, npm, claude and git clone cannot be reached), and a HOME, a root prefix
# (BOOST_ROOT: /etc and /var of the fake machine) and a rig all inside the
# temporary directory. The network is a stand-in curl that serves one small
# Node archive it made itself. The stand-ins keep their state in files, so what
# a first run did is what a second run finds.
#
# The scenarios are the story's acceptance criteria:
#   a. --dry-run with the sample env prints every step in order, exits 0 and
#      changes nothing (no file, no stand-in called to change anything)
#   b. a real run does every step; a second run skips every one and changes
#      nothing (the same files, the same bytes, no changing call)
#   c. no token is written anywhere but files of mode 600, none is put in a
#      command line or in git's config, and the files hold them
#   d. a missing input, a loose env file, a foreign OS and a malformed value
#      are each refused in one line, nothing changed
#   e. the checkout mode (the script run from inside a rig) skips the clone
# The shell linter is run over the script when it is installed, and skipped when not.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=$REPO_ROOT/contrib/boost-bootstrap.sh
EXAMPLE=$REPO_ROOT/contrib/boost-bootstrap.env.example
DOC=$REPO_ROOT/docs/boost-bootstrap.md

cd "$REPO_ROOT"

fail() {
	echo "check-boost-bootstrap: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
[ -f "$EXAMPLE" ] || fail "$EXAMPLE does not exist"
[ -f "$DOC" ] || fail "$DOC does not exist"
bash -n "$SCRIPT" || fail "$SCRIPT does not parse"
bash -n "$0" 2>/dev/null || sh -n "$0" || fail "$0 does not parse"

if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT" || fail "shellcheck found something in $SCRIPT"
	shellcheck "$0" || fail "shellcheck found something in $0"
else
	echo "skipped: shellcheck is not installed"
fi

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(BOOST_[A-Z0-9_]*\|MW_[A-Z0-9_]*\|GH_TOKEN\|CLAUDE_CODE_OAUTH_TOKEN\)=.*/\1/p'); do unset "$v"; done

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM

HOME_DIR=$T/home
BOOT=$T/root
STATE=$T/state
BIN=$T/bin
CALLS=$T/calls.log
TEMPLATE=$T/rig-template
mkdir -p "$HOME_DIR" "$BOOT/etc" "$STATE" "$BIN" "$TEMPLATE"
: >"$CALLS"

# Fake tokens: distinctive, so a grep finds every place they landed.
GH_TOKEN_VALUE="ghp_FAKEgithubTOKEN0123456789abcdef"
CLAUDE_TOKEN_VALUE="sk-ant-oat01-FAKEclaudeTOKEN0123456789"
BEADS_PW_VALUE="FAKEbeadsPASSWORD0123456789"
WG_PRIV_VALUE="PPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP="
HUB_PUB="HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH="

# --- the real tools the script may use ----------------------------------------
REAL=$T/real
mkdir -p "$REAL"
for tool in sh bash env cat cp mv rm mkdir ln chmod stat cmp grep sed awk head tail tr cut sort wc date dirname basename \
	mktemp find ls tar sha256sum xz printf uname readlink touch install sleep true false expr uniq tee diff test cksum; do
	p=$(command -v "$tool" 2>/dev/null) || continue
	case $p in /*) ln -sf "$p" "$REAL/$tool" ;; esac
done

# --- stand-ins ----------------------------------------------------------------
stub() { # <name>: the body on stdin
	{
		echo '#!/bin/sh'
		cat
	} >"$BIN/$1"
	chmod +x "$BIN/$1"
}

stub id <<'EOF'
case "$1" in
-u) echo 0 ;;
-un) echo tester ;;
*) echo "uid=0(root)" ;;
esac
EOF

stub dpkg-query <<'EOF'
# dpkg-query -W -f='${Status}' <pkg>
for last; do :; done
if [ -e "$STATE/pkg/$last" ]; then echo "install ok installed"; exit 0; fi
exit 1
EOF

stub apt-get <<'EOF'
echo "apt-get $*" >>"$CALLS"
[ "$1" = install ] || exit 0
shift
mkdir -p "$STATE/pkg"
for a; do case $a in -*) ;; *) touch "$STATE/pkg/$a" ;; esac; done
EOF

stub curl <<'EOF'
echo "curl $*" >>"$CALLS"
out="" url=""
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	-*) ;;
	*) url=$1 ;;
	esac
	shift
done
case $url in
*SHASUMS256.txt) src=$FIXTURE/SHASUMS256.txt ;;
*.tar.xz) src=$FIXTURE/node.tar.xz ;;
*) echo "curl stand-in: nothing served at $url" >&2; exit 22 ;;
esac
if [ -n "$out" ]; then cp "$src" "$out"; else cat "$src"; fi
EOF

stub git <<'EOF'
if [ "$1" = clone ]; then
	echo "git $*" >>"$CALLS"
	mkdir -p "$3/.git"
	case $2 in *millwright.git) cp -R "$RIGTEMPLATE/." "$3/" ;; esac
	exit 0
fi
exec /usr/bin/git "$@"
EOF

stub systemctl <<'EOF'
scope=system
[ "$1" = --user ] && { scope=user; shift; }
echo "systemctl $scope $*" >>"$CALLS"
cmd=$1
shift
for last; do :; done
mkdir -p "$STATE/units/$scope"
case $cmd in
is-enabled) [ -e "$STATE/units/$scope/enabled-$last" ] && { echo enabled; exit 0; }; echo disabled; exit 1 ;;
is-active) [ -e "$STATE/units/$scope/active-$last" ] && { echo active; exit 0; }; echo inactive; exit 3 ;;
enable)
	touch "$STATE/units/$scope/enabled-$last"
	for a; do [ "$a" = --now ] && touch "$STATE/units/$scope/active-$last"; done
	;;
daemon-reload) ;;
esac
exit 0
EOF

stub loginctl <<'EOF'
echo "loginctl $*" >>"$CALLS"
if [ "$1" = enable-linger ]; then
	mkdir -p "$BOOST_ROOT/var/lib/systemd/linger"
	touch "$BOOST_ROOT/var/lib/systemd/linger/${2:-tester}"
fi
EOF

stub wg <<'EOF'
case "$1" in
genkey) echo "GGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG=" ;;
pubkey) sed 's/^\(.\{6\}\).*/pub-\1=/' ;;
esac
EOF

# The rig a clone produces: only what the script and the stand-in installers read.
mkdir -p "$TEMPLATE/cmd/mw" "$TEMPLATE/scripts" "$TEMPLATE/contrib"
echo 'module github.com/Jonathan-A-White/millwright' >"$TEMPLATE/go.mod"
cat >"$TEMPLATE/scripts/install.sh" <<'EOF'
#!/bin/sh
echo "install.sh $*" >>"$CALLS"
if [ "$1" = --dry-run ]; then
	[ -e "$STATE/mw-built" ] || echo "    would: make build"
	exit 0
fi
touch "$STATE/mw-built"
EOF
cat >"$TEMPLATE/scripts/install-units.sh" <<'EOF'
#!/bin/sh
echo "install-units.sh $*" >>"$CALLS"
systemctl --user daemon-reload
systemctl --user enable --now mw-dispatch.timer
EOF

# The Node archive the stand-in curl serves, and the checksums file beside it.
FIXTURE=$T/fixture
mkdir -p "$FIXTURE/x/node-v24.21.0-linux-x64/bin"
for b in node npx; do
	# shellcheck disable=SC2016
	printf '#!/bin/sh\ncase "$1" in --version) echo v24.21.0 ;; esac\n' >"$FIXTURE/x/node-v24.21.0-linux-x64/bin/$b"
done
cat >"$FIXTURE/x/node-v24.21.0-linux-x64/bin/npm" <<'EOF'
#!/bin/sh
echo "npm $*" >>"$CALLS"
# npm install -g --prefix <dir> <package>
prefix=""
while [ $# -gt 0 ]; do
	[ "$1" = --prefix ] && prefix=$2
	shift
done
mkdir -p "$prefix/bin"
printf '#!/bin/sh\necho claude stand-in\n' >"$prefix/bin/claude"
chmod +x "$prefix/bin/claude"
EOF
chmod +x "$FIXTURE"/x/node-v24.21.0-linux-x64/bin/*
tar -C "$FIXTURE/x" -cJf "$FIXTURE/node.tar.xz" node-v24.21.0-linux-x64
printf '%s  node-v24.21.0-linux-x64.tar.xz\n' "$(sha256sum "$FIXTURE/node.tar.xz" | cut -d ' ' -f 1)" >"$FIXTURE/SHASUMS256.txt"
printf '%s  node-v24.21.0-linux-arm64.tar.xz\n' "$(sha256sum "$FIXTURE/node.tar.xz" | cut -d ' ' -f 1)" >>"$FIXTURE/SHASUMS256.txt"

# A supported system, and one that is not.
printf 'ID=ubuntu\nVERSION_ID="24.04"\n' >"$T/os-release"
printf 'ID=fedora\nVERSION_ID="40"\n' >"$T/os-release-fedora"

# The inputs: the sample env file with its blanks filled in, mode 600.
ENVFILE=$T/boost.env
cat >"$ENVFILE" <<EOF
BOOST_HOST=cloud1
BOOST_WG_ADDRESS=10.88.0.4
BOOST_WG_HUB_PUBKEY=$HUB_PUB
BOOST_WG_PRIVATE_KEY=$WG_PRIV_VALUE
BOOST_BEADS_HOST=10.88.0.2
BOOST_BEADS_PASSWORD=$BEADS_PW_VALUE
BOOST_RIGS="argus=https://github.com/Jonathan-A-White/argus.git postern=https://github.com/Jonathan-A-White/postern.git"
BOOST_GITHUB_TOKEN=$GH_TOKEN_VALUE
BOOST_CLAUDE_TOKEN=$CLAUDE_TOKEN_VALUE
BOOST_GIT_NAME="Jonathan White"
BOOST_GIT_EMAIL=jonathan.jawhite@gmail.com
EOF
chmod 600 "$ENVFILE"

# boost <args...>: the script in clone mode (a loose copy beside no rig).
LOOSE=$T/loose
mkdir -p "$LOOSE"
cp "$SCRIPT" "$LOOSE/boost-bootstrap.sh"
boost() {
	env -i PATH="$BIN:$REAL" HOME="$HOME_DIR" BOOST_ROOT="$BOOT" BOOST_OS_RELEASE="$T/os-release" \
		MW_HOME="$HOME_DIR/millwright" \
		STATE="$STATE" CALLS="$CALLS" FIXTURE="$FIXTURE" RIGTEMPLATE="$TEMPLATE" \
		bash "$LOOSE/boost-bootstrap.sh" "$@"
}

# snapshot: every file under the fake machine with its mode and checksum, and
# every link with its target; the call log and the stand-ins' state excluded.
snapshot() {
	(
		cd "$T"
		find home root -type f -exec sh -c 'for f; do printf "%s %s %s\n" "$f" "$(stat -c %a "$f")" "$(cksum <"$f")"; done' sh {} +
		find home root -type l -exec sh -c 'for f; do printf "%s -> %s\n" "$f" "$(readlink "$f")"; done' sh {} +
		find home root -type d
	) | sort
}

# changing: the calls in the log that change the machine.
changing() {
	grep -E '^(apt-get install|git clone|npm install|curl .*(\.tar\.xz)|loginctl|systemctl (user|system) (enable|daemon-reload)|install\.sh [^-]|install\.sh$|install-units\.sh)' "$1" || true
}

STEPS="packages node git wireguard rig claude vault rigs config beads profile dispatch"

# --- a. --dry-run prints every step and changes nothing ----------------------
NAME="--dry-run prints every step in order and changes nothing"
before=$(snapshot)
RC=0
OUT=$(boost --dry-run --env-file "$ENVFILE" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT"
n=0
total=$(echo "$STEPS" | wc -w | tr -d ' ')
for s in $STEPS; do
	n=$((n + 1))
	printf '%s\n' "$OUT" | grep -Fq "==> [$n/$total] $s:" || fail "$NAME: step $n ($s) is not printed as [$n/$total]: $OUT"
done
last=0
for s in $STEPS; do
	ln=$(printf '%s\n' "$OUT" | grep -n "^==> \[[0-9]*/$total\] $s:" | head -1 | cut -d: -f1)
	if [ -z "$ln" ] || [ "$ln" -le "$last" ]; then fail "$NAME: step $s is out of order: $OUT"; fi
	last=$ln
done
printf '%s\n' "$OUT" | grep -Fq "would:" || fail "$NAME: no step says what it would do: $OUT"
[ "$(snapshot)" = "$before" ] || fail "$NAME: the machine changed"
[ -z "$(changing "$CALLS")" ] || fail "$NAME: a changing command ran: $(changing "$CALLS")"
for secret in "$GH_TOKEN_VALUE" "$CLAUDE_TOKEN_VALUE" "$BEADS_PW_VALUE" "$WG_PRIV_VALUE"; do
	printf '%s\n' "$OUT" | grep -Fq "$secret" && fail "$NAME: a secret was printed"
done
echo "ok: $NAME"

# --- b. a real run, then a second run that changes nothing -------------------
NAME="a real run does every step"
: >"$CALLS"
RC=0
OUT=$(boost --env-file "$ENVFILE" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT"
n=0
for s in $STEPS; do
	n=$((n + 1))
	printf '%s\n' "$OUT" | grep -Fq "==> [$n/$total] $s:" || fail "$NAME: step $n ($s) is not printed: $OUT"
done
[ -x "$HOME_DIR/.local/bin/node" ] || fail "$NAME: no node in ~/.local/bin"
[ -x "$HOME_DIR/.local/bin/claude" ] || fail "$NAME: no claude in ~/.local/bin"
[ -d "$HOME_DIR/millwright/.git" ] || fail "$NAME: the rig was not cloned"
[ -d "$HOME_DIR/argus/.git" ] || fail "$NAME: argus was not cloned"
[ -d "$HOME_DIR/postern/.git" ] || fail "$NAME: postern was not cloned"
[ -d "$HOME_DIR/millwright-vault/.git" ] || fail "$NAME: the vault was not cloned"
CONF=$HOME_DIR/.config/mw/config.toml
grep -Fxq 'host = "cloud1"' "$CONF" || fail "$NAME: config.toml has no host: $(cat "$CONF")"
grep -Fq 'beads_server_host = "10.88.0.2"' "$CONF" || fail "$NAME: config.toml does not name the beads host: $(cat "$CONF")"
grep -Fq 'beads_sync = "auto"' "$CONF" || fail "$NAME: config.toml has no beads_sync: $(cat "$CONF")"
grep -Fq 'argus = "'"$HOME_DIR"'/argus"' "$CONF" || fail "$NAME: config.toml does not list the argus rig: $(cat "$CONF")"
grep -Fq 'millwright = "'"$HOME_DIR"'/millwright"' "$CONF" || fail "$NAME: config.toml does not list the millwright rig: $(cat "$CONF")"
BEADS=$HOME_DIR/.config/mw/beads.env
grep -Fxq 'BEADS_DOLT_SERVER_HOST=10.88.0.2' "$BEADS" || fail "$NAME: beads.env has no server host: $(sed 's/PASSWORD=.*/PASSWORD=.../' "$BEADS")"
grep -Fxq 'BEADS_DOLT_SERVER_PORT=3307' "$BEADS" || fail "$NAME: beads.env has no server port"
WGC=$BOOT/etc/wireguard/wg0.conf
grep -Fxq 'Address = 10.88.0.4/24' "$WGC" || fail "$NAME: wg0.conf has no address"
grep -Fxq "PublicKey = $HUB_PUB" "$WGC" || fail "$NAME: wg0.conf has no hub key"
grep -Fq 'AllowedIPs = 10.88.0.0/24' "$WGC" || fail "$NAME: wg0.conf has no AllowedIPs"
[ -e "$STATE/units/system/active-wg-quick@wg0" ] || fail "$NAME: wg-quick@wg0 was not started"
[ -e "$STATE/units/user/enabled-mw-dispatch.timer" ] || fail "$NAME: mw-dispatch.timer was not enabled"
[ -e "$BOOT/var/lib/systemd/linger/tester" ] || fail "$NAME: linger was not enabled"
grep -Fq 'boost-bootstrap' "$HOME_DIR/.profile" || fail "$NAME: ~/.profile has no block"
[ -f "$HOME_DIR/.config/mw/dispatch.env" ] || fail "$NAME: no dispatch.env"
grep -Fq 'PATH=' "$HOME_DIR/.config/mw/dispatch.env" || fail "$NAME: dispatch.env has no PATH"
DROPIN=$HOME_DIR/.config/systemd/user/mw-dispatch.service.d
grep -Fq 'EnvironmentFile=-%h/.config/mw/github.env' "$DROPIN"/*.conf || fail "$NAME: the dispatch drop-in does not read github.env"
[ "$(HOME="$HOME_DIR" /usr/bin/git config --global user.name)" = "Jonathan White" ] || fail "$NAME: git user.name not set"
echo "ok: $NAME"

NAME="a second run skips every step and changes nothing"
before=$(snapshot)
cp "$CALLS" "$T/calls-first.log"
: >"$CALLS"
RC=0
OUT2=$(boost --env-file "$ENVFILE" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT2"
after=$(snapshot)
[ "$after" = "$before" ] || fail "$NAME: the machine changed"
[ -z "$(changing "$CALLS")" ] || fail "$NAME: a changing command ran: $(changing "$CALLS")"
for s in $STEPS; do
	step_out=$(printf '%s\n' "$OUT2" | awk -v s="$s" '/^==> \[/ { on = ($0 ~ ("\\] " s ":")) } on')
	[ -n "$step_out" ] || fail "$NAME: step $s is not printed"
	printf '%s\n' "$step_out" | grep -Eq '^    (run|would):' && fail "$NAME: step $s acts on a second run: $step_out"
	printf '%s\n' "$step_out" | grep -Fq '    skip:' || fail "$NAME: step $s does not say it skipped: $step_out"
done
echo "ok: $NAME"

# --- c. no token anywhere but a file of mode 600 ------------------------------
NAME="no token is written anywhere but files of mode 600"
for secret in "$GH_TOKEN_VALUE" "$CLAUDE_TOKEN_VALUE" "$BEADS_PW_VALUE" "$WG_PRIV_VALUE"; do
	hits=$(grep -rlF -- "$secret" "$HOME_DIR" "$BOOT" "$STATE" 2>/dev/null || true)
	[ -n "$hits" ] || fail "$NAME: a secret reached no file at all"
	for f in $hits; do
		[ "$(stat -c %a "$f")" = 600 ] || fail "$NAME: $f holds a secret and is mode $(stat -c %a "$f")"
	done
	cat "$T/calls-first.log" "$CALLS" >"$T/calls-all.log"
	grep -qF -- "$secret" "$T/calls-all.log" && fail "$NAME: a secret is in a command line"
	printf '%s\n' "$OUT" "$OUT2" | grep -qF -- "$secret" && fail "$NAME: a secret was printed"
done
grep -rlF -- "$GH_TOKEN_VALUE" "$HOME_DIR/.gitconfig" 2>/dev/null && fail "$NAME: the GitHub token is in ~/.gitconfig"
[ "$(stat -c %a "$HOME_DIR/.config/mw/github.env")" = 600 ] || fail "$NAME: github.env is not mode 600"
[ "$(stat -c %a "$HOME_DIR/.config/mw/claude.env")" = 600 ] || fail "$NAME: claude.env is not mode 600"
[ "$(stat -c %a "$BEADS")" = 600 ] || fail "$NAME: beads.env is not mode 600"
[ "$(stat -c %a "$WGC")" = 600 ] || fail "$NAME: wg0.conf is not mode 600"
[ "$(stat -c %a "$HOME_DIR/.config/mw")" = 700 ] || fail "$NAME: ~/.config/mw is not mode 700"
grep -Fxq "GH_TOKEN=$GH_TOKEN_VALUE" "$HOME_DIR/.config/mw/github.env" || fail "$NAME: github.env does not hold the token"
grep -Fxq "CLAUDE_CODE_OAUTH_TOKEN=$CLAUDE_TOKEN_VALUE" "$HOME_DIR/.config/mw/claude.env" || fail "$NAME: claude.env does not hold the token"
echo "ok: $NAME"

# The sample env file holds no value that looks like a secret.
NAME="the sample env file holds no secret"
if grep -E '^(BOOST_GITHUB_TOKEN|BOOST_CLAUDE_TOKEN|BOOST_BEADS_PASSWORD|BOOST_WG_PRIVATE_KEY)=.+' "$EXAMPLE" >/dev/null; then
	fail "$NAME: $EXAMPLE gives a secret a value"
fi
echo "ok: $NAME"

# --- d. refusals --------------------------------------------------------------
refused() { # <name> <args...>: the run must exit non-zero, say it in one line, and change nothing
	rname=$1
	shift
	before=$(snapshot)
	RC=0
	ERR=$("$@" 2>&1 >/dev/null) || RC=$?
	[ "$RC" != 0 ] || fail "$rname: exited 0"
	lines=$(printf '%s\n' "$ERR" | grep -c .)
	[ "$lines" -le 3 ] || fail "$rname: $lines lines on stderr, wanted a few: $ERR"
	[ "$(snapshot)" = "$before" ] || fail "$rname: the machine changed"
}

grep -v '^BOOST_BEADS_HOST=' "$ENVFILE" >"$T/missing.env"
chmod 600 "$T/missing.env"
refused "a missing input is refused" boost --env-file "$T/missing.env"
echo "ok: a missing input is refused"

cp "$ENVFILE" "$T/loose.env"
chmod 644 "$T/loose.env"
refused "an env file others can read is refused" boost --env-file "$T/loose.env"
echo "ok: an env file others can read is refused"

sed 's/^BOOST_WG_ADDRESS=.*/BOOST_WG_ADDRESS=not-an-address/' "$ENVFILE" >"$T/badaddr.env"
chmod 600 "$T/badaddr.env"
refused "a malformed address is refused" boost --env-file "$T/badaddr.env"
echo "ok: a malformed address is refused"

sed 's/^BOOST_GITHUB_TOKEN=.*/BOOST_GITHUB_TOKEN=has a space/' "$ENVFILE" >"$T/badtoken.env"
chmod 600 "$T/badtoken.env"
refused "a token with a space is refused" boost --env-file "$T/badtoken.env"
echo "ok: a token with a space is refused"

refused "a system that is not Ubuntu is refused" env BOOST_OS_RELEASE="$T/os-release-fedora" \
	env -i PATH="$BIN:$REAL" HOME="$HOME_DIR" BOOST_ROOT="$BOOT" BOOST_OS_RELEASE="$T/os-release-fedora" \
	STATE="$STATE" CALLS="$CALLS" FIXTURE="$FIXTURE" RIGTEMPLATE="$TEMPLATE" \
	bash "$LOOSE/boost-bootstrap.sh" --env-file "$ENVFILE"
echo "ok: a system that is not Ubuntu is refused"

RC=0
ERR=$(boost --no-such-flag 2>&1) || RC=$?
[ "$RC" != 0 ] || fail "an unknown flag exited 0"
echo "ok: an unknown flag is refused"

# --- e. inside a rig checkout the clone is skipped -----------------------------
NAME="run from inside a rig it uses that rig and clones nothing"
rm -rf "$HOME_DIR" "$STATE" "$BOOT"
mkdir -p "$HOME_DIR" "$STATE" "$BOOT/etc"
INRIG=$T/inrig
mkdir -p "$INRIG"
cp -R "$TEMPLATE/." "$INRIG/"
mkdir -p "$INRIG/contrib"
cp "$SCRIPT" "$INRIG/contrib/boost-bootstrap.sh"
: >"$CALLS"
RC=0
OUT=$(env -i PATH="$BIN:$REAL" HOME="$HOME_DIR" BOOST_ROOT="$BOOT" BOOST_OS_RELEASE="$T/os-release" \
	STATE="$STATE" CALLS="$CALLS" FIXTURE="$FIXTURE" RIGTEMPLATE="$TEMPLATE" \
	bash "$INRIG/contrib/boost-bootstrap.sh" --env-file "$ENVFILE" 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exited $RC: $OUT"
grep -F 'git clone' "$CALLS" | grep -Fq 'millwright.git' && fail "$NAME: it cloned the rig it is in"
grep -Fq 'millwright = "'"$INRIG"'"' "$HOME_DIR/.config/mw/config.toml" || fail "$NAME: config.toml does not name $INRIG: $(cat "$HOME_DIR/.config/mw/config.toml")"
echo "ok: $NAME"

echo "OK: contrib/boost-bootstrap.sh prints and runs every step in order, checks before it acts so a second run changes nothing, writes tokens only to mode 600 files, and refuses bad input"
