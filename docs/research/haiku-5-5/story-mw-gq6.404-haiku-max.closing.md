Replay of mw-gq6.404 in a scratch worktree, detached HEAD, commit 1e12a65. No bead, branch or remote touched.

Done
- scripts/check-gate-jobs.sh: var() now runs its make -pn read with JOBS and MAKEFLAGS unset (env -u JOBS -u MAKEFLAGS). A host's builder_jobs sets JOBS for its Builders, and a command-line JOBS=4 reaches a nested make through MAKEFLAGS, so before this the check tested those overrides instead of the Makefile's default. The check's own arguments (JOBS=1, JOBS=3) still apply.
- One guard line, written first: it runs var with JOBS=7 and MAKEFLAGS='-- JOBS=7' set and expects -p=<nproc>. On the unchanged script it failed (GOFLAGS '-p=7', want -p=20), so a regression shows even on a host where JOBS is unset.
- One commit, 1e12a65. Only that script changed.

Verified (this box has 20 cores; "16 cores" means a stand-in nproc that prints 16, put first on PATH)
- Before the fix: JOBS=4 scripts/check-gate-jobs.sh exited 1 with "GOFLAGS is '-p=4', want -p=20 (nproc)"; with the stand-in nproc 16, "want -p=16". That is the reported failure.
- After the fix: JOBS=4 exits 0 with the stand-in nproc 16 (AC1) and on this box. It also exits 0 with no JOBS, with JOBS=1, with JOBS=3 and GOFLAGS=-p=9 set, with GOMAXPROCS=4 and VITEST_MAX_WORKERS=4 alongside JOBS=4, under dash and under bash, and when run from another directory.
- Each half of the fix is needed: in a scratch copy, dropping -u MAKEFLAGS or dropping -u JOBS makes the guard fail with -p=7.
- AC2: JOBS=4 make lint exits 0 (environment form), make lint JOBS=4 exits 0 (command-line form), and plain make lint exits 0. All on 1e12a65, and the gate-jobs step reads "check-gate-jobs: ok" each time.
- AC3: make test exits 0 on 1e12a65 (47 packages ok, none failing). make build exits 0.
- sh -n ok and git diff --check clean. gofmt does not apply: no Go file changed.

Left undone or not verified
- Not run on a real 16-core host, nor on the desktop with builder_jobs = 4, which this replay cannot reach. The 16-core case is the stand-in nproc only.
- shellcheck is not installed on this box, so the lint's shellcheck steps printed "skipped". The new lines are plain sh, and no comment added begins with "# shellcheck ".

Internal: nothing for the Governor to look at

What's new: make lint on a host with JOBS set (the desktop, with builder_jobs = 4) no longer fails at the gate-jobs check. The check now reads the Makefile's own default, and a guard line proves it.

For the rig memory:
- A command-line JOBS=4 (make lint JOBS=4) reaches a nested make through MAKEFLAGS as well as the environment, so clearing JOBS alone is not enough: a check that runs make -pn must clear MAKEFLAGS too.
- To test a host's core count, put a stand-in nproc first on PATH: the Makefile's JOBS default and scripts/check-gate-jobs.sh both call it.
