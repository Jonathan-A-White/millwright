mw-gq6.404 (REPLAY, scratch worktree, detached HEAD; commit d2ab622 on top of 65328b1)

What was done
- scripts/check-gate-jobs.sh: its var() helper ran `make -pn` with the caller's environment, so an ambient JOBS (JOBS=4 from a host's builder_jobs cap) beat the Makefile's `JOBS ?= $(shell nproc)` default. The check then expected -p=<nproc> and failed. var() now runs `env -u JOBS make -pn ...`, so only the JOBS=n the check passes itself sets it. One line of code plus a comment. Nothing else changed.

Verified and how
- Before the fix, reproduced: `JOBS=4 sh scripts/check-gate-jobs.sh` exited 1 ("GOFLAGS is '-p=4', want -p=20 (nproc)" on this 20-core box).
- After the fix on this 20-core box: `JOBS=4`, no JOBS, `JOBS=1`, and `JOBS=20` all exit 0. `sh -n` on the script passes.
- Criterion 1 names a 16-core host, which this box is not. I simulated one with a stub `nproc` printing 16, on a PATH entry outside the repo (/tmp/haiku-trial/fakenproc16). Before the fix: `JOBS=4` exit 1 ("want -p=16"). After: `JOBS=4` exit 0, and no JOBS exit 0. The stub also feeds the Makefile's `$(shell nproc)`, so the simulation is the real path, not just the script's own call. It is still a simulation, not a real 16-core machine.
- `make build`: exit 0.
- `make test` (full suite, `-tags beads_integration`): exit 0, 47 packages `ok`, no FAIL or panic lines. Log at /tmp/haiku-trial/story-mw-gq6.404-haiku-high.test.log.
- `JOBS=4 make lint`: exit 0, including "check-gate-jobs: ok". Log at /tmp/haiku-trial/story-mw-gq6.404-haiku-high.lint.log.
- No Go code changed, so gofmt does not apply.

Left undone / not checked
- `env -u` is not POSIX sh. It works with GNU env here; BSD/macOS env also takes -u, but I did not run the script there.
- No bd, mw check, or bead comment, per the replay rules.

Internal: nothing for the Governor to look at.

What's new: a check that failed on any host with a JOBS cap in its environment now reads the Makefile's own defaults, so `JOBS=4 make lint` passes.

For the rig memory:
- A host's builder_jobs cap reaches a Builder as JOBS in the environment; any scripts/ check that runs `make -pn` must unset it (`env -u JOBS`), as check-gate-jobs.sh now does.
- To test a different core count, put a stub `nproc` on PATH in a directory outside the repo; both the script and the Makefile's `$(shell nproc)` see it.
