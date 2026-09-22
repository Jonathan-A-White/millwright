#!/bin/sh
# Proves scripts/install.sh works from nothing, in a clean container. Not run
# by make test or make lint: it needs docker and the network, which those must
# not depend on. `make check-bootstrap` is its own target, for a host that has
# docker (never the VPS, which must not run it).
#
# Builds contrib/bootstrap-test/Dockerfile (a bare Debian base with nothing
# preinstalled but a non-root user), mounts this checkout into the container
# read-only and copies it to a writable path, then runs
# contrib/bootstrap-test/run.sh inside it, which drives scripts/install.sh
# exactly as the README's Quick start says (as that non-root user, with the
# one apt-get command install.sh prints run as root, same as a real host
# without those packages yet) and then checks: mw version and bd version at
# the pinned BD_VERSION both run, the named hand steps were printed, mw init
# makes a vault where bd list works and holds nothing
# scripts/check-template.sh forbids, mw init --join works against a bare copy
# of that vault, and a second run of install.sh changes nothing.
#
# It never logs in to anything, needs no secret, and never pushes anywhere but
# a bare git repository it made itself inside the container.
#
# Exits 0 and prints "skipped: no docker" when docker is not on PATH or not
# reachable, so a host without it is never asked to run this. Otherwise it
# ends with "bootstrap: ok" on success and reports the wall time and the image
# size.
#
#   make check-bootstrap
#   sh scripts/check-bootstrap.sh

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DOCKERFILE_DIR=contrib/bootstrap-test
IMAGE=mw-bootstrap-test
CONTAINER=mw-bootstrap-test-$$

cd "$REPO_ROOT"

if ! command -v docker >/dev/null 2>&1 || ! docker version >/dev/null 2>&1; then
	echo "skipped: no docker"
	exit 0
fi

fail() {
	echo "check-bootstrap: $*" >&2
	exit 1
}

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

[ -f "$DOCKERFILE_DIR/Dockerfile" ] || fail "$DOCKERFILE_DIR/Dockerfile does not exist"
[ -f "$DOCKERFILE_DIR/run.sh" ] || fail "$DOCKERFILE_DIR/run.sh does not exist"
sh -n "$DOCKERFILE_DIR/run.sh" || fail "$DOCKERFILE_DIR/run.sh does not parse"
sh -n "$0" || fail "$0 does not parse"
if command -v shellcheck >/dev/null 2>&1; then
	shellcheck -s sh "$DOCKERFILE_DIR/run.sh" "$0" || fail "shellcheck found something"
fi

START=$(date +%s)

docker build -q -t "$IMAGE" "$DOCKERFILE_DIR" >/dev/null || fail "docker build failed"

docker run -d --name "$CONTAINER" -v "$REPO_ROOT:/repo-ro:ro" "$IMAGE" sleep 3600 >/dev/null ||
	fail "docker run failed"

docker cp "$DOCKERFILE_DIR/run.sh" "$CONTAINER:/run.sh" || fail "could not copy the driver into the container"

if ! docker exec "$CONTAINER" sh /run.sh; then
	fail "the container did not prove the bootstrap (see the output above)"
fi

END=$(date +%s)
SIZE=$(docker image inspect "$IMAGE" --format '{{.Size}}' 2>/dev/null || echo 0)

echo "check-bootstrap: wall time $((END - START))s, image size $((SIZE / 1024 / 1024)) MB"
echo "bootstrap: ok"
