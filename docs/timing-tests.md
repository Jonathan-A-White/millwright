# Timing tests

A timing test is one that needs a real clock to mean anything: it measures or
waits for time itself (a debounce that fires after 300 ms, a service worker's
retry back-off, a token that expires). It is not a test that merely uses time.
Try the rig's test preset and fake timers first; a test that still cannot be
made exact with them is a timing test. Such a test is slow or flaky under load,
so a rig can keep it out of the landing gate and run it on its own schedule.

## How a rig tags one

1. The test's file matches a glob in the rig's `timing_tests` key, in the rig's
   file in the vault (`rigs/<rig>.toml`):

   ```toml
   timing_tests = ["tests/timing/**", "**/*.timing.test.ts"]
   ```

   The key is a list of strings, like `union_merge_files`; the default is none.
   A value that is not a list of strings is refused, naming the key. A landing
   still reads a file with a key this mw does not know.
2. Go tests are also named with a `Timing` suffix (`TestRetryBackoffTiming`), so
   the rig's Makefile can pass `-skip Timing` or `-run Timing`.
3. A test says which files it covers with a `covers:` line in the first ten
   lines of its file, in the comment above it: the globs of the files whose
   change makes it worth running, separated by commas or spaces
   (`// covers: src/sw/**, src/retry.ts`). A test with no such line covers the
   whole rig. Say why the test needs a real clock on another line.

## What mw does with the key

For a rig that has `timing_tests`, every run of its `[tests]` or
`[changed_specs]` command is handed two environment variables:

- `MW_TIMING=skip`: leave the timing tests out;
- `MW_TIMING_TESTS`: the globs, joined by commas.

That is the close-out gate, `mw check` (and its parts), the merged-result gate,
the batch gate of Land Rush, and `mw suite`; the Builder's own session gets the
same two, so `npm test` run by hand matches the gate. A rig without the key is
run exactly as before: neither variable is set. mw never deletes or moves a
tagged file; the rig's test setup reads the variables.

## The nightly timing run

The home's follower runs the tagged tests itself: the job `timing@<host>`. From
the suite's hour (`suite_hour`, 08:00 UTC unless the config says another), after
the night's suite of a rig that asks for one, a rig with `timing_tests` is run
when a file a tagged test covers has changed between the commit of its last
passing night and origin's main (`git diff --name-only`). A test's covered files
are the globs of its `covers:` line, or the whole rig without one, and the test
file itself always counts; a rig with no passing night yet is always due. When
nothing changed, nothing runs: the log says so, and nothing is kept. A rig with
`timing_tests` and no `suite_epic` is not run: the log says it has none.

The run waits for the host to calm by the rule `mw suite` uses, for at most two
hours. A host that never calms skips the night: the log and the job's event name
the load, nothing is kept and no bug is filed. Otherwise it takes the host's gate
lock and runs the rig's `[tests]` command in a throwaway worktree of origin's
main with `MW_TIMING=only` and `MW_TIMING_TESTS` (the rig's globs).

The result is kept in the vault as `runs/timing/<rig>/<date>.json` (and the
output as `<date>.log`): the commit, whether it passed, the failing files, the
host's `uptime` before and after, and the duration. Each failing test file is
filed as one held `[bug]` under the rig's `suite_epic`, labelled `bug,timing`,
naming the commit, the covered files that changed and the load; a file that
already has an open timing bug gets a dated comment on it and no second bug. A
failure that names no file files nothing and fails the pass, which mails the
Mayor.

## The rig's wiring

vitest and Playwright read the variables through `pwa-kit/test-preset`'s
`timingFilter`, which turns `MW_TIMING=skip` and the globs into an `exclude`.
A Makefile for Go reads `MW_TIMING` and passes `-skip Timing` (or `-run Timing`
for the run that wants only them).

## When to set the key

The Mayor sets `timing_tests` on a rig only once this mw runs on every host
that lands that rig's stories: a host with an older mw would ignore the key and
run the tagged tests in its gate, and the gate would then differ from host to
host. The key changes which tests a landing gate runs, so the Governor sees it
before it is set.
