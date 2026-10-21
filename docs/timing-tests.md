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
3. A test says why it is a timing test with a `covers:` line in the comment
   above it: what the real clock proves that a fake one cannot.

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
