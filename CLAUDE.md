# millwright — notes for sessions working this rig

Read `CONTEXT.md` first. Its glossary is law: use Seat, Session, Story, Path,
Formula, Rig, Host, Fuel exactly as defined there, and avoid the words it
lists under *Avoid* — in code, comments, commits and tests alike.

## Build and test

```sh
export PATH=$PATH:/usr/local/go/bin   # Go is not on PATH in a non-login shell
make build   # -> bin/mw
make test    # go test -tags beads_integration ./... , including the godog features
make lint    # go vet -tags beads_integration ./...
```

A plain `go test ./...` skips the real-`bd` cases of `infrastructure/beads`, most
of the suite's clock; `make test` adds `-tags beads_integration` to run them.

One `go` command at a time on the VPS (1 vCPU, ~1 GB RAM); the Makefile pins
`GOFLAGS=-p=1` and `GOMAXPROCS=1`. The first build after adding a dependency
takes minutes — wait it out rather than retrying.

## Layout

`docs/codemap.md` is the one page that says where everything is — the layers,
every port with its adapter and fake, every use case with its command and
feature. Read it before hunting. To add a command, read
`docs/adding-a-command.md` first.

`domain/` pure value types, standard library only · `application/` use cases
and ports · `infrastructure/` adapters · `cmd/mw` the cobra command line ·
`features/` Gherkin, run by godog from `features/features_test.go`.
