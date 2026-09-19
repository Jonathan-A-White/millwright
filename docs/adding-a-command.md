# Adding a command

Read `docs/codemap.md` first for the layers, ports and use cases.

Copy `mw sweep`.

1. **Feature** — `features/sweep.feature` first. Step text is unique across
   all features; Gherkin does not unescape `\"`.
2. **Steps** — `features/steps/sweep_steps.go`: context struct, `Before` reset,
   ctx.Given/When/Then; register in `features/features_test.go`.
3. **Use case** — `application/sweep.go`: struct of ports and settings, `Run(ctx)`
   returning a report with `String()`.
4. **Port method** — if a port falls short, add it with a doc comment.
5. **Fake** — `application/apptest/faketracker.go`.
6. **Adapter** — `infrastructure/beads/beads.go`, tested in
   `infrastructure/beads/beads_integration_test.go`.
7. **Cobra** — `cmd/mw/sweep.go`: `newSweepCmd()`, a `RunE` that only reads
   config and calls the use case; add it in `cmd/mw/root.go`.
8. **README** — a section beside "Keeping two hosts level", ending with a
   pointer to the feature file.
