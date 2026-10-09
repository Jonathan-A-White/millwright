# millwright — build, test and lint.
#
# Every go command here runs as many processes as JOBS, which defaults to the
# host's cores (the gate was once pinned to one for the 1 vCPU, ~1 GB VPS; on a
# small box run `make test JOBS=1`). Run one target at a time; do not use make -j.

GO ?= go
BIN ?= bin/mw
HANDS_ROOT_BIN ?= bin/mw-hands-root
PKG ?= ./...
# The build tags make test and make lint compile in. beads_integration is the
# real-bd cases of infrastructure/beads, most of the suite's clock, which a
# plain `go test` therefore skips.
TAGS ?= beads_integration

JOBS ?= $(shell nproc)

export GOFLAGS := -p=$(JOBS)
export GOMAXPROCS := $(JOBS)

.PHONY: all build test lint clean check-formulas check-bootstrap

all: build test lint

# mw-hands-root runs as root through sudo: built static, so no loader or
# library outside the binary has any say in what it does.
build:
	$(GO) build -o $(BIN) ./cmd/mw
	CGO_ENABLED=0 $(GO) build -o $(HANDS_ROOT_BIN) ./cmd/mw-hands-root

test:
	$(GO) test -tags $(TAGS) $(PKG)

# go vet, then the code map check: docs/codemap.md must be under its size
# limit, name only paths that exist, and leave out no port, use case or
# cmd/mw command. Then the timer units: systemd-analyze must accept them where
# it exists. Then the seat's tmux server unit against a real, throwaway
# --user systemd unit on its own tmux socket (never the host's real session
# "0"): killing its server must bring a new one up within RestartSec; it
# skips this live proof, rather than failing, where no --user systemd or no
# tmux is reachable; then scripts/check-seat-tmux-leak.sh kills a run of that
# check with SIGKILL and proves it leaves only bounded units that cannot reach
# the real socket. Then the health script, run against stand-in commands
# for every reading it takes. Then template/, which must hold no personal or
# host-bound detail. Then the installer, run against stand-in commands, and
# its pins. Then the unit installer, run against a stand-in systemctl and a
# throwaway HOME. Then mw-heavy, against a stand-in systemd-run and a real
# flock on a throwaway lock file. Then wg-enrol, against temp files with
# WG_SYNC=0 (and, for the syncconf scenario, stand-in wg and wg-quick). Then
# contrib/install-sops-age.sh, against a stand-in curl serving made-up releases
# and a throwaway MW_BIN: its pins, its checksum refusals, and that a second run
# changes nothing. Then the
# gate's parallelism: GOFLAGS and GOMAXPROCS must follow JOBS. Every
# other check here reads only this repository (and a temporary directory)
# and starts nothing.
lint:
	$(GO) vet -tags $(TAGS) $(PKG)
	scripts/check-codemap.sh
	scripts/check-timer-units.sh
	scripts/check-seat-tmux-respawn.sh
	scripts/check-seat-tmux-leak.sh
	scripts/check-health.sh
	scripts/check-template.sh
	scripts/check-install.sh
	scripts/check-install-units.sh
	scripts/check-heavy.sh
	scripts/check-wg-enrol.sh
	scripts/check-install-sops-age.sh
	scripts/check-gate-jobs.sh
	scripts/check-chain-stamps-doc.sh

clean:
	rm -rf bin

# Checks formulas/*.formula.json against a throwaway beads database.
# Not part of `make test`: needs bd on PATH and takes real wall-clock time.
check-formulas:
	scripts/check-formulas.sh

# Proves scripts/install.sh works from nothing in a clean container. Not part
# of build, test or lint: it needs docker and the network. Skips itself with
# exit 0 and prints "skipped: no docker" on a host without docker (the VPS
# must never run this).
check-bootstrap:
	scripts/check-bootstrap.sh
