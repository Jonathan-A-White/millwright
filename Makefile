# millwright — build, test and lint.
#
# The VPS is small (1 vCPU, ~1 GB RAM), so every go command here is pinned to
# one process and one CPU. Run one target at a time; do not use make -j.

GO ?= go
BIN ?= bin/mw
PKG ?= ./...

export GOFLAGS := -p=1
export GOMAXPROCS := 1

.PHONY: all build test lint clean check-formulas

all: build test lint

build:
	$(GO) build -o $(BIN) ./cmd/mw

test:
	$(GO) test $(PKG)

lint:
	$(GO) vet $(PKG)

clean:
	rm -rf bin

# Checks formulas/*.formula.json against a throwaway beads database.
# Not part of `make test`: needs bd on PATH and takes real wall-clock time.
check-formulas:
	scripts/check-formulas.sh
