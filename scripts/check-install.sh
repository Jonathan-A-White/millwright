#!/bin/sh
# Run scripts/install.sh against stand-in commands and hold what it does to what
# its header promises. Reads only this repository and a temporary directory it
# makes and removes; it never touches the real host: the script runs with an
# empty environment, a PATH holding only stand-ins and a short list of real
# coreutils (so the real apt, curl, git, make, go and bd cannot be reached), and
# a HOME, a Go root, a bin directory and a rig inside the temporary directory.
# The network is a stand-in curl that serves two small archives it made itself.
#
# What it holds: scripts/pins.env is only assignments, has one BD_VERSION and a
# GO_VERSION that agrees with go.mod; install.sh names no web server, database
# or blog and handles no credential; --help and an unknown flag; a system that is
# not Debian or Ubuntu (and an architecture that is not amd64 or arm64) is
# refused with nothing changed; --dry-run prints every step and changes nothing;
# a real run does the steps in order and a second run skips every one; a
# non-root run that needs root says so and changes nothing; a bad checksum
# installs nothing; the rig is cloned when the script is not in a checkout (also
# when it is piped in, with no pins beside it); a foreign file is never
# overwritten; the hand steps are printed.
#
# shellcheck is run over the scripts when it is installed, and skipped when not.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=scripts/install.sh
PINS=scripts/pins.env

cd "$REPO_ROOT"

fail() {
	echo "check-install: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -f "$PINS" ] || fail "$PINS does not exist"
sh -n "$SCRIPT" || fail "$SCRIPT does not parse"
sh -n scripts/check-install.sh || fail "scripts/check-install.sh does not parse"

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(MW_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

# --- 1. the pins ------------------------------------------------------------
bad=$(grep -Ev '^([A-Z][A-Z_]*=[0-9][0-9.]*|#.*|)$' "$PINS" || true)
[ -z "$bad" ] || fail "$PINS holds more than assignments of a version: $bad"
[ "$(grep -c '^GO_VERSION=' "$PINS")" = 1 ] || fail "$PINS must set GO_VERSION once"
[ "$(grep -c '^BD_VERSION=' "$PINS")" = 1 ] || fail "$PINS must set BD_VERSION once"
[ "$(grep -c BD_VERSION "$PINS")" = 1 ] || fail "$PINS names BD_VERSION more than once (comments too)"
GO_VERSION=
BD_VERSION=
# shellcheck disable=SC1090
. "./$PINS"
want_go=$(awk '$1 == "go" { print $2; exit }' go.mod)
[ "$GO_VERSION" = "$want_go" ] || fail "$PINS says GO_VERSION=$GO_VERSION but go.mod says go $want_go"
[ -n "$BD_VERSION" ] || fail "$PINS does not set BD_VERSION"

# --- 2. what the script may name ---------------------------------------------
if grep -Eiq 'nginx|mysql|ghost' "$SCRIPT" "$PINS"; then
	fail "$SCRIPT or $PINS names a web server, a database or a blog: the factory only"
fi
if grep -Eiq 'auth token|GH_TOKEN|GITHUB_TOKEN|ANTHROPIC_API_KEY|ANTHROPIC_AUTH|password|\.credentials|\.ssh|\.claude\.json' "$SCRIPT"; then
	fail "$SCRIPT reads or names a credential: secrets are hand steps"
fi

# --- 3. the sandbox ----------------------------------------------------------
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
SH=$(command -v sh)
STAND=$T/stand
STOCK=$T/stock
REAL=$T/real
W=$T/w
mkdir -p "$STAND" "$STOCK" "$REAL"

# The only real programs the script may reach: coreutils and text tools.
for c in awk basename cat chmod cp cut dirname find grep gzip head ln ls mkdir mktemp mv readlink rm sed sha256sum sort tail tar touch tr wc; do
	for d in /usr/bin /bin; do
		if [ -x "$d/$c" ]; then ln -s "$d/$c" "$REAL/$c"; break; fi
	done
done

standin() { # <dir> <name>, the body on stdin
	cat >"$1/$2"
	chmod +x "$1/$2"
}

# Always there: who is running, what the system is, what is installed.
standin "$STAND" id <<'EOF'
#!/bin/sh
echo "id $*" >>"$FAKE_CALLS"
[ "$1" = -u ] || exit 99
echo "$FAKE_UID"
EOF
standin "$STAND" uname <<'EOF'
#!/bin/sh
echo "uname $*" >>"$FAKE_CALLS"
case $1 in
-s) echo "${FAKE_KERNEL:-Linux}" ;;
-m) echo "${FAKE_ARCH:-x86_64}" ;;
*) exit 99 ;;
esac
EOF
standin "$STAND" dpkg <<'EOF'
#!/bin/sh
echo "dpkg $*" >>"$FAKE_CALLS"
[ "$1" = -s ] || exit 99
[ -e "$FAKE_WORLD/pkgs/$2" ]
EOF
# apt-get: root only, and installing a package puts its command on PATH.
standin "$STAND" apt-get <<'EOF'
#!/bin/sh
echo "apt-get $*" >>"$FAKE_CALLS"
[ "$(id -u)" = 0 ] || { echo "E: Could not open lock file - are you root?" >&2; exit 100; }
case $1 in
update) exit 0 ;;
install) ;;
*) exit 99 ;;
esac
shift
for p in "$@"; do
	case $p in
	-*) continue ;;
	ripgrep) cmd=rg ;;
	ca-certificates) cmd="" ;;
	*) cmd=$p ;;
	esac
	touch "$FAKE_WORLD/pkgs/$p"
	[ -z "$cmd" ] || cp "$FAKE_STOCK/$cmd" "$FAKE_BIN/$cmd"
done
EOF

# What apt puts on PATH. Each logs its call.
for c in tmux jq rg python3 gh; do
	standin "$STOCK" "$c" <<EOF
#!/bin/sh
echo "$c \$*" >>"\$FAKE_CALLS"
exit 99
EOF
done
standin "$STOCK" git <<'EOF'
#!/bin/sh
echo "git $*" >>"$FAKE_CALLS"
case $1 in
clone)
	[ "$#" -eq 3 ] || exit 99
	mkdir -p "$3/.git" "$3/cmd/mw" "$3/scripts"
	printf 'module github.com/Jonathan-A-White/millwright\n\ngo 1.27.1\n' >"$3/go.mod"
	cp "$FAKE_PINS" "$3/scripts/pins.env"
	;;
config)
	[ "$2" = --global ] || exit 99
	v=$(sed -n "s/^$3=//p" "$FAKE_WORLD/gitconfig" 2>/dev/null | head -n 1)
	[ -n "$v" ] || exit 1
	echo "$v"
	;;
*) exit 99 ;;
esac
EOF
standin "$STOCK" make <<'EOF'
#!/bin/sh
echo "make $* (in $PWD, go=$(command -v go || echo none))" >>"$FAKE_CALLS"
[ "$1" = build ] || exit 99
mkdir -p bin
echo built >bin/mw
chmod +x bin/mw
EOF
# The network: two archives it made, served by URL; nothing else is reachable.
standin "$STOCK" curl <<'EOF'
#!/bin/sh
echo "curl $*" >>"$FAKE_CALLS"
out=""
url=""
while [ "$#" -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	-*) ;;
	*) url=$1 ;;
	esac
	shift
done
give() { if [ -n "$out" ]; then cat >"$out"; else cat; fi; }
case $url in
https://dl.google.com/go/go*.tar.gz.sha256)
	if [ "$FAKE_BAD_SUM" = go ]; then echo 0000000000000000000000000000000000000000000000000000000000000000; else cat "$FAKE_STOCK/go.sum"; fi | give ;;
https://dl.google.com/go/go*.tar.gz) give <"$FAKE_STOCK/go.tgz" ;;
https://github.com/gastownhall/beads/releases/download/v*/checksums.txt)
	if [ "$FAKE_BAD_SUM" = bd ]; then sum=0000000000000000000000000000000000000000000000000000000000000000; else sum=$(cat "$FAKE_STOCK/bd.sum"); fi
	v=${url#*/download/v}
	v=${v%%/*}
	{
		echo "1111111111111111111111111111111111111111111111111111111111111111  beads_${v}_darwin_arm64.tar.gz"
		echo "$sum  beads_${v}_linux_amd64.tar.gz"
		echo "$sum  beads_${v}_linux_arm64.tar.gz"
	} | give ;;
https://github.com/gastownhall/beads/releases/download/v*/beads_*_linux_*.tar.gz) give <"$FAKE_STOCK/bd.tgz" ;;
*) echo "curl: no stand-in for $url" >&2; exit 22 ;;
esac
EOF

# The two archives: a Go root with bin/go, and a bd release with bd.
mkdir -p "$T/gosrc/go/bin" "$T/bdsrc"
printf '#!/bin/sh\necho "go version go%s linux/amd64"\n' "$GO_VERSION" >"$T/gosrc/go/bin/go"
printf '#!/bin/sh\necho "bd version %s (stand-in)"\n' "$BD_VERSION" >"$T/bdsrc/bd"
echo "stand-in licence" >"$T/bdsrc/LICENSE"
chmod +x "$T/gosrc/go/bin/go" "$T/bdsrc/bd"
tar -czf "$STOCK/go.tgz" -C "$T/gosrc" go
tar -czf "$STOCK/bd.tgz" -C "$T/bdsrc" bd LICENSE
sha256sum "$STOCK/go.tgz" | cut -d ' ' -f 1 >"$STOCK/go.sum"
sha256sum "$STOCK/bd.tgz" | cut -d ' ' -f 1 >"$STOCK/bd.sum"

# --- 4. a world to run in, and ways to look at what happened ----------------
# world <uid>: a fresh host: only the always-there stand-ins on PATH, no
# packages, an empty HOME, a checkout of the rig and a lone copy of the script.
world() {
	rm -rf "$W"
	mkdir -p "$W/bin" "$W/pkgs" "$W/home" "$W/tmp" "$W/cwd" "$W/lone"
	for c in "$STAND"/*; do ln -s "$c" "$W/bin/$(basename "$c")"; done
	: >"$W/calls"
	printf 'ID=ubuntu\nID_LIKE=debian\nNAME="Ubuntu"\n' >"$W/os-release"
	# a checkout of the rig, holding a copy of the script under test
	mkdir -p "$W/rig/scripts" "$W/rig/cmd/mw"
	printf 'module github.com/Jonathan-A-White/millwright\n\ngo 1.27.1\n' >"$W/rig/go.mod"
	cp "$SCRIPT" "$PINS" "$W/rig/scripts/"
	# the script on its own, with its pins beside it, outside any checkout
	cp "$SCRIPT" "$PINS" "$W/lone/"
	UID_NOW=${1:-0}
	ENVX=""
	SCRIPT_RUN=$W/rig/scripts/install.sh
	MW_HOME_DIR=$W/millwright
	MW_BIN_DIR=$W/home/.local/bin
	GO_ROOT=$W/goroot
	PATH_EXTRA=""
}
# preinstall: every package already there, as on a host that has the tools.
preinstall() {
	FAKE_UID=0 FAKE_CALLS=$W/calls FAKE_WORLD=$W FAKE_STOCK=$STOCK FAKE_BIN=$W/bin PATH=$W/bin:$REAL \
		apt-get install -y git tmux jq ripgrep python3 curl ca-certificates gh make >/dev/null
	: >"$W/calls"
}
# snap: everything under the world but the call log and the scratch directory.
snap() {
	(
		cd "$W"
		find . ! -path ./calls ! -path ./tmp ! -path './tmp/*' | sort
		find . -type f ! -path ./calls ! -path './tmp/*' -exec cksum {} + | sort
		find . -type l ! -path './tmp/*' -exec sh -c 'for l; do echo "$l -> $(readlink "$l")"; done' sh {} + | sort
	)
}
# run <arg>...: run $SCRIPT_RUN with those arguments; OUT is what it printed, RC its status.
run() {
	: >"$W/calls"
	RC=0
	# shellcheck disable=SC2086
	OUT=$(cd "$W/cwd" && env -i PATH="$W/bin:$REAL$PATH_EXTRA" HOME="$W/home" TMPDIR="$W/tmp" \
		FAKE_UID="$UID_NOW" FAKE_CALLS="$W/calls" FAKE_WORLD="$W" FAKE_STOCK="$STOCK" FAKE_BIN="$W/bin" \
		FAKE_PINS="$REPO_ROOT/$PINS" \
		MW_INSTALL_OS_RELEASE="$W/os-release" MW_HOME="$MW_HOME_DIR" MW_GO_ROOT="$GO_ROOT" \
		$ENVX "$SH" "$SCRIPT_RUN" "$@" 2>&1) || RC=$?
}
# runpiped: the same, with the script fed to sh on stdin, as curl | sh would.
runpiped() {
	: >"$W/calls"
	RC=0
	# shellcheck disable=SC2086
	OUT=$(cd "$W/cwd" && env -i PATH="$W/bin:$REAL$PATH_EXTRA" HOME="$W/home" TMPDIR="$W/tmp" \
		FAKE_UID="$UID_NOW" FAKE_CALLS="$W/calls" FAKE_WORLD="$W" FAKE_STOCK="$STOCK" FAKE_BIN="$W/bin" \
		FAKE_PINS="$REPO_ROOT/$PINS" \
		MW_INSTALL_OS_RELEASE="$W/os-release" MW_HOME="$MW_HOME_DIR" MW_GO_ROOT="$GO_ROOT" \
		$ENVX "$SH" -s -- "$@" <"$REPO_ROOT/$SCRIPT" 2>&1) || RC=$?
}

# --- assertions -------------------------------------------------------------
NAME=""
ok() { echo "ok: $NAME"; }
has() { printf '%s\n' "$OUT" | grep -Fq -- "$1" || fail "$NAME: the output lacks: $1
$OUT"; }
lacks() { if printf '%s\n' "$OUT" | grep -Fq -- "$1"; then fail "$NAME: the output has: $1
$OUT"; fi; }
called() { grep -Eq -- "$1" "$W/calls" || fail "$NAME: no call matched: $1
$(cat "$W/calls")"; }
notcalled() { if grep -Eq -- "$1" "$W/calls"; then fail "$NAME: a call matched: $1
$(cat "$W/calls")"; fi; }
rc_is() { [ "$RC" = "$1" ] || fail "$NAME: exit status $RC, wanted $1
$OUT"; }
rc_not0() { [ "$RC" != 0 ] || fail "$NAME: exit status 0, wanted a refusal
$OUT"; }
# The calls that would change something. Everything else the stand-ins see is a read.
MUTATING='^(apt-get |curl |git clone|make )'
only_reads() { notcalled "$MUTATING"; }
steps_are() {
	got=$(printf '%s\n' "$OUT" | sed -n 's/^==> \[\([0-9]*\)\/[0-9]*\] \([a-z]*\):.*/\1 \2/p' | tr '\n' ' ')
	[ "$got" = "$1 " ] || fail "$NAME: steps ran as: $got; wanted: $1"
}
# in_order <pattern>...: each pattern's first matching call comes after the one before it.
in_order() {
	prev=0
	for p in "$@"; do
		n=$(grep -En -- "$p" "$W/calls" | head -n 1 | cut -d: -f1) || true
		[ -n "$n" ] || fail "$NAME: no call matched: $p
$(cat "$W/calls")"
		[ "$n" -gt "$prev" ] || fail "$NAME: '$p' came at call $n, not after call $prev
$(cat "$W/calls")"
		prev=$n
	done
}
skips() { # <n>: exactly n steps were skipped
	got=$(printf '%s\n' "$OUT" | grep -c '^    skip:' || true)
	[ "$got" = "$1" ] || fail "$NAME: $got steps skipped, wanted $1
$OUT"
}
hand_steps() {
	for h in '[claude]' '[github]' '[git-identity]' '[mw-init]'; do has "$h"; done
	has 'claude auth login'
	has 'gh auth login'
	has 'git config --global user.name'
	has 'git config --global user.email'
	has 'mw init'
	n=$(printf '%s\n' "$OUT" | grep -c '^      check: ' || true)
	[ "$n" = 4 ] || fail "$NAME: $n hand steps carry a check, wanted 4
$OUT"
}
unchanged() { # <before>: the world is as snapshotted
	[ "$(snap)" = "$1" ] || fail "$NAME: the run changed the world:
$(snap | diff - "$T/before" || true)"
	[ -z "$(ls -A "$W/tmp")" ] || fail "$NAME: the script left its scratch files behind: $(ls -A "$W/tmp")"
}
NOAPT_PKGS='git tmux jq ripgrep python3 curl ca-certificates gh make'
GO_URL=https://dl.google.com/go/go$GO_VERSION.linux-amd64.tar.gz
BD_URL=https://github.com/gastownhall/beads/releases/download/v$BD_VERSION/beads_${BD_VERSION}_linux_amd64.tar.gz

# --- 5. --help and a flag it does not know ------------------------------------
NAME="--help"
world 1000
snap >"$T/before"
run --help
rc_is 0
has '--dry-run'
has '--help'
[ ! -s "$W/calls" ] || fail "$NAME: --help ran a command: $(cat "$W/calls")"
unchanged "$(cat "$T/before")"
ok

NAME="an unknown flag"
world 1000
run --frobnicate
rc_is 2
has 'unknown'
has 'Usage'
only_reads
ok

# --- 6. a system that is not Debian or Ubuntu: refused, nothing changed ---------
for mode in real dry; do
	NAME="a Fedora host is refused ($mode)"
	world 0
	printf 'ID=fedora\nID_LIKE="rhel fedora"\n' >"$W/os-release"
	snap >"$T/before"
	if [ "$mode" = dry ]; then run --dry-run; else run; fi
	rc_not0
	has 'Debian or Ubuntu'
	has 'fedora'
	has 'apt'
	for p in git tmux jq ripgrep python3 curl ca-certificates gh; do has "$p"; done
	has "$GO_VERSION"
	has "$BD_VERSION"
	only_reads
	unchanged "$(cat "$T/before")"
	ok
done

NAME="a Mac is refused"
world 0
printf 'ID=ubuntu\n' >"$W/os-release"
ENVX="FAKE_KERNEL=Darwin"
snap >"$T/before"
run
rc_not0
has 'Debian or Ubuntu'
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="a 32-bit host is refused"
world 0
ENVX="FAKE_ARCH=armv7l"
snap >"$T/before"
run
rc_not0
has 'armv7l'
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="a Debian derivative is accepted (ID_LIKE)"
world 0
printf 'ID=linuxmint\nID_LIKE="ubuntu debian"\n' >"$W/os-release"
run --dry-run
rc_is 0
ok

# --- 7. --dry-run: every step printed, nothing changed --------------------------
NAME="--dry-run in a checkout, as root"
world 0
snap >"$T/before"
run --dry-run
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
has "apt-get install -y --no-install-recommends $NOAPT_PKGS"
has "$GO_ROOT"
has "$GO_VERSION"
has "$BD_VERSION"
has 'make build'
has "$MW_BIN_DIR/mw"
has 'Dry run'
hand_steps
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="--dry-run as a user without root says what needs root and still exits 0"
world 1000
snap >"$T/before"
run --dry-run
rc_is 0
has 'needs root'
has 'apt-get'
hand_steps
only_reads
unchanged "$(cat "$T/before")"
ok

NAME="--dry-run for a script that is not in a checkout and has no pins beside it"
world 0
snap >"$T/before"
runpiped --dry-run
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
has 'git clone https://github.com/Jonathan-A-White/millwright.git'
has "$MW_HOME_DIR"
has 'scripts/pins.env'
hand_steps
only_reads
unchanged "$(cat "$T/before")"
ok

# --- 8. a real run in a checkout, then again --------------------------------------
NAME="a real run in a checkout, as root"
world 0
run
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
in_order '^apt-get update' "^apt-get install -y --no-install-recommends $NOAPT_PKGS\$" "^curl .*$GO_URL" "^curl .*$BD_URL" '^make build'
notcalled '^git clone'
called "^make build \(in $W/rig, go=$GO_ROOT/bin/go\)"
"$GO_ROOT/bin/go" version | grep -Fq "go$GO_VERSION" || fail "$NAME: no Go $GO_VERSION under $GO_ROOT"
"$MW_BIN_DIR/bd" version | grep -Fq "bd version $BD_VERSION" || fail "$NAME: no bd $BD_VERSION under $MW_BIN_DIR"
[ "$(readlink "$MW_BIN_DIR/mw")" = "$W/rig/bin/mw" ] || fail "$NAME: $MW_BIN_DIR/mw does not link to the rig's bin/mw"
[ -x "$W/rig/bin/mw" ] || fail "$NAME: no bin/mw was built"
has "$MW_BIN_DIR is not on your PATH"
has "$GO_ROOT/bin is not on your PATH"
hand_steps
[ -z "$(ls -A "$W/tmp")" ] || fail "$NAME: the script left its scratch files behind"
ok

NAME="a second run skips every step"
snap >"$T/before"
run
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
skips 6
only_reads
hand_steps
unchanged "$(cat "$T/before")"
ok

NAME="a run when a source is newer than bin/mw builds again"
sleep 1
touch "$W/rig/cmd/mw/main.go"
run
rc_is 0
called '^make build'
notcalled '^(apt-get|curl|git clone)'
ok

NAME="PATH already holding the bin directory and the Go root is not nagged about"
PATH_EXTRA=":$MW_BIN_DIR:$GO_ROOT/bin"
run
rc_is 0
lacks 'is not on your PATH'
ok

# --- 9. not root ------------------------------------------------------------------
NAME="a real run without root that needs root stops before changing anything"
world 1000
snap >"$T/before"
run
rc_not0
has 'needs root'
has "apt-get install -y --no-install-recommends $NOAPT_PKGS"
notcalled "$MUTATING"
unchanged "$(cat "$T/before")"
ok

NAME="a real run without root when the packages are there puts Go under HOME"
world 1000
preinstall
GO_ROOT=""
run
rc_is 0
[ -x "$W/home/.local/go/bin/go" ] || fail "$NAME: Go is not under \$HOME/.local/go"
notcalled '^apt-get'
[ "$(readlink "$MW_BIN_DIR/mw")" = "$W/rig/bin/mw" ] || fail "$NAME: mw is not linked"
ok

# --- 10. checksums ------------------------------------------------------------------
NAME="a Go archive that does not match its checksum installs nothing"
world 0
preinstall
ENVX="FAKE_BAD_SUM=go"
run
rc_not0
has 'checksum'
[ ! -e "$GO_ROOT" ] || fail "$NAME: $GO_ROOT exists"
notcalled '^make'
ok

NAME="a bd archive that does not match its checksum installs nothing"
world 0
preinstall
ENVX="FAKE_BAD_SUM=bd"
run
rc_not0
has 'checksum'
[ ! -e "$MW_BIN_DIR/bd" ] || fail "$NAME: bd was installed"
notcalled '^make'
ok

# --- 11. the arm64 archives ----------------------------------------------------------
NAME="an arm64 host fetches the arm64 archives"
world 0
preinstall
ENVX="FAKE_ARCH=aarch64"
run
rc_is 0
called "^curl .*go$GO_VERSION.linux-arm64.tar.gz"
called "^curl .*beads_${BD_VERSION}_linux_arm64.tar.gz"
ok

# --- 12. the rig is cloned when the script is not in a checkout --------------------------
NAME="a script beside its pins but outside a checkout clones the rig"
world 0
SCRIPT_RUN=$W/lone/install.sh
run
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
in_order "^apt-get install" "^git clone https://github.com/Jonathan-A-White/millwright.git $MW_HOME_DIR\$" "^curl .*$GO_URL" '^make build'
called "^make build \(in $MW_HOME_DIR,"
[ "$(readlink "$MW_BIN_DIR/mw")" = "$MW_HOME_DIR/bin/mw" ] || fail "$NAME: mw is not linked to the clone"
ok

NAME="a second run does not clone again"
snap >"$T/before"
run
rc_is 0
skips 6
only_reads
has 'already cloned'
unchanged "$(cat "$T/before")"
ok

NAME="a script piped to sh reads its pins from the clone"
world 0
runpiped
rc_is 0
steps_are "1 apt 2 rig 3 go 4 bd 5 build 6 link"
in_order "^apt-get install" '^git clone' "^curl .*$GO_URL" "^curl .*$BD_URL" '^make build'
"$GO_ROOT/bin/go" version | grep -Fq "go$GO_VERSION" || fail "$NAME: no Go $GO_VERSION"
hand_steps
ok

NAME="a directory in the way of the clone is not touched"
world 0
preinstall
mkdir -p "$MW_HOME_DIR"
echo mine >"$MW_HOME_DIR/notes.txt"
SCRIPT_RUN=$W/lone/install.sh
run
rc_not0
has "$MW_HOME_DIR"
[ "$(cat "$MW_HOME_DIR/notes.txt")" = mine ] || fail "$NAME: the directory was changed"
notcalled '^git clone'
ok

# --- 13. what is already there is not overwritten ---------------------------------------------
NAME="an older Go under the Go root is replaced by the pinned one"
world 0
preinstall
mkdir -p "$GO_ROOT/bin"
printf '#!/bin/sh\necho "go version go1.20.0 linux/amd64"\n' >"$GO_ROOT/bin/go"
chmod +x "$GO_ROOT/bin/go"
run
rc_is 0
"$GO_ROOT/bin/go" version | grep -Fq "go$GO_VERSION" || fail "$NAME: still the old Go"
ok

NAME="a directory at the Go root that is not a Go root is left alone"
world 0
preinstall
mkdir -p "$GO_ROOT"
echo mine >"$GO_ROOT/notes.txt"
run
rc_not0
[ "$(cat "$GO_ROOT/notes.txt")" = mine ] || fail "$NAME: the directory was changed"
ok

NAME="a Go on PATH at the pinned version is used, not replaced"
world 0
preinstall
mkdir -p "$W/pathgo"
printf '#!/bin/sh\necho "go version go%s linux/amd64"\n' "$GO_VERSION" >"$W/pathgo/go"
chmod +x "$W/pathgo/go"
PATH_EXTRA=":$W/pathgo"
run
rc_is 0
notcalled "^curl .*$GO_URL"
[ ! -e "$GO_ROOT" ] || fail "$NAME: installed a second Go"
ok

NAME="a regular file where the mw link goes is not overwritten"
world 0
preinstall
mkdir -p "$MW_BIN_DIR"
echo mine >"$MW_BIN_DIR/mw"
run
rc_not0
[ "$(cat "$MW_BIN_DIR/mw")" = mine ] || fail "$NAME: the file was changed"
ok

NAME="a link to another mw is repointed"
world 0
preinstall
mkdir -p "$MW_BIN_DIR"
ln -s /nonexistent/mw "$MW_BIN_DIR/mw"
run
rc_is 0
[ "$(readlink "$MW_BIN_DIR/mw")" = "$W/rig/bin/mw" ] || fail "$NAME: the link was not repointed"
ok

# --- 14. hand steps that are already done are not owed ---------------------------------------------
NAME="a set git identity and an existing mw config are not owed"
world 0
preinstall
printf 'user.name=Someone\nuser.email=someone@example.org\n' >"$W/gitconfig"
mkdir -p "$W/home/.config/mw"
echo 'host = "laptop"' >"$W/home/.config/mw/config.toml"
run
rc_is 0
has '[claude]'
has '[github]'
lacks '[git-identity]'
lacks '[mw-init]'
has 'git identity: already set'
ok

# --- 15. shellcheck, where it is installed -----------------------------------------------------------
if command -v shellcheck >/dev/null 2>&1; then
	NAME="shellcheck"
	shellcheck -s sh "$SCRIPT" scripts/check-install.sh || fail "shellcheck found something"
	ok
else
	echo "skip: shellcheck is not installed"
fi

echo "OK: $SCRIPT: $PINS agrees with go.mod; the steps run in order, a second run skips them all, a foreign system is refused, and the hand steps are printed, all against stand-in commands"
