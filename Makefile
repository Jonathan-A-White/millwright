# millwright — build, test and lint.
#
# The VPS is small (1 vCPU, ~1 GB RAM), so every go command here is pinned to
# one process and one CPU. Run one target at a time; do not use make -j.

GO ?= go
BIN ?= bin/mw
PKG ?= ./...
# The build tags make test and make lint compile in. beads_integration is the
# real-bd cases of infrastructure/beads, most of the suite's clock, which a
# plain `go test` therefore skips.
TAGS ?= beads_integration

export GOFLAGS := -p=1
export GOMAXPROCS := 1

.PHONY: all build test lint clean check-formulas

all: build test lint

build:
	$(GO) build -o $(BIN) ./cmd/mw

test:
	$(GO) test -tags $(TAGS) $(PKG)

# go vet, then the code map check: docs/codemap.md must be under its size
# limit, name only paths that exist, and leave out no port, use case or
# cmd/mw command. Then the timer units: systemd-analyze must accept them where
# it exists. Then the health script, run against stand-in commands for every
# reading it takes. Then template/, which must hold no personal or host-bound
# detail. Then the installer, run against stand-in commands, and its pins. These
# checks read only this repository (and a temporary directory) and start nothing.
lint:
	$(GO) vet -tags $(TAGS) $(PKG)
	scripts/check-codemap.sh
	scripts/check-timer-units.sh
	scripts/check-health.sh
	scripts/check-template.sh
	scripts/check-install.sh

clean:
	rm -rf bin

# Checks formulas/*.formula.json against a throwaway beads database.
# Not part of `make test`: needs bd on PATH and takes real wall-clock time.
check-formulas:
	scripts/check-formulas.sh
