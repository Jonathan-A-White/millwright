#!/bin/sh
# Runs as root inside the bootstrap-test container (see
# scripts/check-bootstrap.sh, which builds the image and copies this in).
# Proves scripts/install.sh works from nothing, the way the README's Quick
# start says: as a non-root user, with the one apt-get command it prints run
# as root, same as a real host without those packages yet would need. set -e:
# the first failure stops it and check-bootstrap.sh sees the exec fail.

set -eu

REPO_RO=/repo-ro
REPO=/repo
WANT_BD=$(sed -n 's/^BD_VERSION=//p' "$REPO_RO/scripts/pins.env")
LEAKS='jonathan|jawhite|allmymind|/root/|/home/|vultr|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'

say() { echo "== $*"; }

say "copying the read-only checkout to a writable one"
cp -r "$REPO_RO" "$REPO"
chown -R tester:tester "$REPO"

say "first run, as tester, with no packages yet: must refuse and name what needs root"
if su - tester -c "cd $REPO && sh scripts/install.sh" >/tmp/first.log 2>&1; then
	echo "FAIL: the first run should have refused with no packages installed" >&2
	cat /tmp/first.log >&2
	exit 1
fi
grep -q 'needs root' /tmp/first.log || {
	echo "FAIL: the first run did not say it needs root" >&2
	cat /tmp/first.log >&2
	exit 1
}

say "installing the apt packages as root, the hand step the first run named"
apt-get update >/tmp/apt.log 2>&1
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	git tmux jq ripgrep python3 curl ca-certificates gh make >>/tmp/apt.log 2>&1 || {
	echo "FAIL: installing the apt packages as root" >&2
	cat /tmp/apt.log >&2
	exit 1
}

say "second run, as tester: installs Go and bd, builds mw, links it"
su - tester -c "cd $REPO && sh scripts/install.sh" >/tmp/second.log 2>&1 || {
	echo "FAIL: install.sh failed once the apt packages were there" >&2
	cat /tmp/second.log >&2
	exit 1
}
for h in '[claude]' '[github]' '[git-identity]' '[mw-init]'; do
	grep -qF "$h" /tmp/second.log || {
		echo "FAIL: the hand steps did not name $h" >&2
		cat /tmp/second.log >&2
		exit 1
	}
done
say "the named hand steps were printed: [claude] [github] [git-identity] [mw-init]"

say "mw version"
su - tester -c 'export PATH=$HOME/.local/bin:$HOME/.local/go/bin:$PATH; mw version' || {
	echo "FAIL: mw version" >&2
	exit 1
}

say "bd version prints the pinned BD_VERSION ($WANT_BD)"
got_bd=$(su - tester -c 'export PATH=$HOME/.local/bin:$PATH; bd version')
case $got_bd in
*"$WANT_BD"*) ;;
*)
	echo "FAIL: bd version ($got_bd) does not name the pinned BD_VERSION ($WANT_BD)" >&2
	exit 1
	;;
esac

say "mw init --vault /tmp/v --prefix tst"
su - tester -c 'export PATH=$HOME/.local/bin:$HOME/.local/go/bin:$PATH; mw init --vault /tmp/v --prefix tst' || {
	echo "FAIL: mw init" >&2
	exit 1
}
su - tester -c 'export PATH=$HOME/.local/bin:$PATH; cd /tmp/v && bd list >/dev/null' || {
	echo "FAIL: bd list in the fresh vault" >&2
	exit 1
}
if su - tester -c "cd /tmp/v && grep -rn -i -E '$LEAKS' --exclude-dir=.git ."; then
	echo "FAIL: the fresh vault holds something scripts/check-template.sh forbids" >&2
	exit 1
fi

say "mw init --join against a bare copy of /tmp/v"
su - tester -c 'git clone --bare /tmp/v /tmp/v-remote.git' >/tmp/clone.log 2>&1 || {
	echo "FAIL: cloning a bare copy of the vault" >&2
	cat /tmp/clone.log >&2
	exit 1
}
su - tester -c 'export PATH=$HOME/.local/bin:$HOME/.local/go/bin:$PATH; mw init --join /tmp/v-remote.git --vault /tmp/v2' || {
	echo "FAIL: mw init --join" >&2
	exit 1
}

say "a second run of install.sh changes nothing"
su - tester -c "cd $REPO && find . -path ./.git -prune -o -type f -print | sort" >/tmp/before-tree.txt
su - tester -c "cd $REPO && find . -path ./.git -prune -o -type f -exec cksum {} + | sort" >/tmp/before-sums.txt
su - tester -c "cd $REPO && sh scripts/install.sh" >/tmp/third.log 2>&1 || {
	echo "FAIL: the third run of install.sh" >&2
	cat /tmp/third.log >&2
	exit 1
}
skips=$(grep -c '^    skip:' /tmp/third.log || true)
[ "$skips" = 6 ] || {
	echo "FAIL: the third run skipped $skips of 6 steps, wanted 6 (it should have changed nothing)" >&2
	cat /tmp/third.log >&2
	exit 1
}
su - tester -c "cd $REPO && find . -path ./.git -prune -o -type f -print | sort" >/tmp/after-tree.txt
su - tester -c "cd $REPO && find . -path ./.git -prune -o -type f -exec cksum {} + | sort" >/tmp/after-sums.txt
diff /tmp/before-tree.txt /tmp/after-tree.txt >/dev/null || {
	echo "FAIL: a second install.sh changed which files exist in the checkout" >&2
	exit 1
}
diff /tmp/before-sums.txt /tmp/after-sums.txt >/dev/null || {
	echo "FAIL: a second install.sh changed a file in the checkout" >&2
	exit 1
}

echo "run.sh: ok"
