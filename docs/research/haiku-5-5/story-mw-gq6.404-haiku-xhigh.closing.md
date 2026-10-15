Replay of mw-gq6.404 in a scratch worktree, on the detached HEAD, commit 2fd5d7d. No bead was read or written, no bd was run, and mw check was not run.

What was done
- scripts/check-gate-jobs.sh: var now runs its make -pn calls with JOBS unset (env -u JOBS). A JOBS in the caller's environment no longer decides what the checks read off the Makefile.
- The check came first: JOBS=4 var GOFLAGS must equal -p=nproc. Before the fix it failed for the right reason: "an ambient JOBS=4 reaches make: GOFLAGS is '-p=4', want -p=20 (nproc)". It passes after the fix.
- Nothing else changed: one file, 6 insertions and 2 deletions.

Verified, and how
- Before the fix: env -u JOBS scripts/check-gate-jobs.sh and JOBS=4 scripts/check-gate-jobs.sh both exited 1 with the message above (this host has 20 cores).
- After the fix: JOBS=4 scripts/check-gate-jobs.sh exits 0 on this host. With a stand-in nproc that prints 16 placed first on PATH (make's $(shell nproc) reads it too), JOBS=4 exits 0 and so does no JOBS. Ambient JOBS=1 and JOBS=3 also exit 0.
- Acceptance 1: met on this host. The 16-core case was run with the stand-in, not on a real 16-core host.
- Acceptance 2: JOBS=4 make lint exits 0, and its log shows "check-gate-jobs: ok".
- Acceptance 3: make test, second run, exits 0 with 47 packages ok. The tmux package ran fresh in that run.
- make build exits 0. make lint exits 0.

Not verified, or left undone
- The first make test run exited 2. TestRunnerCarriesTheDirectoryTheEnvironmentAndWhatIsTypedIn (infrastructure/tmux) waited 30 s and failed: its pane showed "carry on" with no "typed=carry on" line, then "Pane is dead (status 3)". The test passed alone (twice) and in the second full run. I did not find the cause of the first failure. This change touches only a shell script that the tmux package does not read.
- shellcheck is not installed on this host, so make lint skipped its shellcheck step for scripts/check-gate-jobs.sh.

Internal: nothing for the Governor to look at.

What's new: scripts/check-gate-jobs.sh checks the Makefile's own JOBS default. An ambient JOBS, such as a host's builder_jobs cap, no longer fails make lint.

For the rig memory:
- To test another core count for scripts/check-gate-jobs.sh, put a stand-in nproc first on PATH: make's $(shell nproc) reads it too.
- TestRunnerCarriesTheDirectoryTheEnvironmentAndWhatIsTypedIn (infrastructure/tmux) failed once in a full make test (30 s wait, pane dead) and passed alone and on a full rerun: rerun it alone before chasing it.
