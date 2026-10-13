Feature: builder_jobs caps the test workers a Builder and the gate may start on one host
  A host that is also someone's PC (the desktop, where Windows has to keep cores free
  for the browser) says how many test workers a Builder's tests and mw next's gate may
  start at once, with `builder_jobs = <n>` in ~/.config/mw/config.toml. The command line
  of the rig's tests then sees JOBS, GOMAXPROCS and VITEST_MAX_WORKERS as that number.
  A host that does not say it is left exactly as it was.

  Scenario: a host that says builder_jobs hands the number to the rig's tests
    Given a config file on this host holding:
      """
      builder_jobs = 3
      """
    When the rig's tests are run on this host with a command that prints its worker limits
    Then the tests printed: JOBS=3 GOMAXPROCS=3 VITEST_MAX_WORKERS=3

  Scenario: a host that does not say builder_jobs leaves the rig's tests as they were
    Given a config file on this host holding:
      """
      cap = 4
      """
    When the rig's tests are run on this host with a command that prints its worker limits
    Then the tests printed: JOBS= GOMAXPROCS= VITEST_MAX_WORKERS=

  Scenario: builder_jobs below one is refused, naming the key
    Given a config file on this host holding:
      """
      builder_jobs = 0
      """
    Then reading builder_jobs is refused, saying: builder_jobs
