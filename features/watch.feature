Feature: mw watch
  mw watch runs on a host that can lose its own network, and tells a fault of
  this host's own from a fault of the host it watches. It applies one rule and
  prints ONE line naming what it found:

    1. Neither of two outside places answers: local-fault. This host's own
       network is down, so nothing is known of the watched host. Nobody is woken,
       and what mw watch remembers is left as it was.
    2. An outside place answers and ssh to the watched host answers: the host's
       health line (~/.mw-health there) is read. Its text is opaque except its
       leading UTC timestamp and its trailing verdict=ok or verdict=unwell:<why>.
       A line older than 40 minutes is stale: ok, unwell <reasons> or stale.
    3. An outside place answers but ssh does not: look for signs of life, the
       blog over HTTPS and the host's last sync note in the local beads fresher
       than 30 minutes. The host is down only after two failed checks at least 3
       minutes apart, remembered on this host: unreachable-once signs=<...>, then
       down signs=<...>. Any ssh that answers, whatever the line says, forgets it.

  The line, dated, is also appended to a log on this host: one line a run. mw
  watch leaves with 0 when nobody needs waking and 6 when a wake is called for:
  unwell, stale, down. With no [watch] table it says "nothing to watch" and
  leaves with 0. It only reads, and it is only ever run here with fakes.

  Background:
    Given a watch of the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example" and the blog "https://blog.example"
    And the watch clock reads "2026-09-19T12:00:00Z"

  Scenario: Neither outside place answers, so the fault is local
    Given neither outside place answers
    When mw watch runs
    Then mw watch says "local-fault"
    And mw watch leaves with the status 0
    And ssh was not tried
    And the watch memory was never touched
    And the watch log holds exactly 1 line
    And the last watch log line is "2026-09-19T12:00:00Z local-fault"

  Scenario: One outside place answering is enough to know this host's network is up
    Given only "https://two.example" answers of the outside places
    And the health line of the watched host is 5 minutes old and ends "verdict=ok"
    When mw watch runs
    Then mw watch says "ok"
    And mw watch leaves with the status 0

  Scenario: A local fault leaves a failure already remembered as it was
    Given the outside places answer
    And the ssh read fails
    And mw watch runs
    And the outside places stop answering
    And 10 minutes pass
    When mw watch runs
    Then mw watch says "local-fault"
    And mw watch leaves with the status 0
    And the watch memory holds the first failure at "2026-09-19T12:00:00Z"
    And the watch log holds exactly 2 lines

  Scenario: The watched host answers and says it is ok
    Given the outside places answer
    And the health line of the watched host is 5 minutes old and ends "verdict=ok"
    When mw watch runs
    Then mw watch says "ok"
    And mw watch leaves with the status 0
    And ssh was asked to read the health line of "vps-ssh" once
    And the watch log holds exactly 1 line
    And the last watch log line is "2026-09-19T12:00:00Z ok"

  Scenario: The watched host answers and says it is unwell
    Given the outside places answer
    And the health line of the watched host is 5 minutes old and ends "verdict=unwell:load1,mayor_gone"
    When mw watch runs
    Then mw watch says "unwell load1,mayor_gone"
    And mw watch leaves with the status 6
    And the watch log holds exactly 1 line
    And the last watch log line is "2026-09-19T12:00:00Z unwell load1,mayor_gone"

  Scenario: The watched host answers with a health line older than 40 minutes
    Given the outside places answer
    And the health line of the watched host is 41 minutes old and ends "verdict=ok"
    When mw watch runs
    Then mw watch says "stale"
    And mw watch leaves with the status 6
    And the watch log holds exactly 1 line

  Scenario: A health line 40 minutes old is not yet stale
    Given the outside places answer
    And the health line of the watched host is 40 minutes old and ends "verdict=ok"
    When mw watch runs
    Then mw watch says "ok"
    And mw watch leaves with the status 0

  Scenario: An old health line that says unwell is stale, not believed
    Given the outside places answer
    And the health line of the watched host is 90 minutes old and ends "verdict=unwell:disk_pct"
    When mw watch runs
    Then mw watch says "stale"
    And mw watch leaves with the status 6

  Scenario: A health line mw watch cannot read the time of is stale
    Given the outside places answer
    And the health line of the watched host is "yesterday all is well verdict=ok"
    When mw watch runs
    Then mw watch says "stale"
    And mw watch leaves with the status 6

  Scenario: An ssh that fails for the first time is unreachable-once, with no signs of life
    Given the outside places answer
    And the ssh read fails
    And the blog does not answer
    And the watched host last synced 90 minutes ago
    When mw watch runs
    Then mw watch says "unreachable-once signs=none"
    And mw watch leaves with the status 0
    And the watch memory holds the first failure at "2026-09-19T12:00:00Z"
    And the watch log holds exactly 1 line
    And the last watch log line is "2026-09-19T12:00:00Z unreachable-once signs=none"

  Scenario: The blog answering is a sign of life
    Given the outside places answer
    And the ssh read fails
    And the blog answers
    When mw watch runs
    Then mw watch says "unreachable-once signs=blog"
    And mw watch leaves with the status 0

  Scenario: A sync note fresher than 30 minutes is a sign of life
    Given the outside places answer
    And the ssh read fails
    And the blog does not answer
    And the watched host last synced 29 minutes ago
    When mw watch runs
    Then mw watch says "unreachable-once signs=sync"
    And mw watch leaves with the status 0

  Scenario: A sync note 30 minutes old is no sign of life
    Given the outside places answer
    And the ssh read fails
    And the blog does not answer
    And the watched host last synced 30 minutes ago
    When mw watch runs
    Then mw watch says "unreachable-once signs=none"

  Scenario: Both signs of life are named
    Given the outside places answer
    And the ssh read fails
    And the blog answers
    And the watched host last synced 5 minutes ago
    When mw watch runs
    Then mw watch says "unreachable-once signs=blog,sync"
    And mw watch leaves with the status 0

  Scenario: Failing again less than 3 minutes later is still unreachable-once
    Given the outside places answer
    And the ssh read fails
    And the blog does not answer
    And mw watch runs
    And 2 minutes pass
    When mw watch runs
    Then mw watch says "unreachable-once signs=none"
    And mw watch leaves with the status 0
    And the watch memory holds the first failure at "2026-09-19T12:00:00Z"
    And the watch log holds exactly 2 lines

  Scenario: Failing again 3 minutes or more later is down
    Given the outside places answer
    And the ssh read fails
    And the blog does not answer
    And mw watch runs
    And 3 minutes pass
    When mw watch runs
    Then mw watch says "down signs=none"
    And mw watch leaves with the status 6
    And the watch log holds exactly 2 lines
    And the last watch log line is "2026-09-19T12:03:00Z down signs=none"

  Scenario: A host that is down names the signs of life it still shows
    Given the outside places answer
    And the ssh read fails
    And the blog answers
    And mw watch runs
    And 10 minutes pass
    When mw watch runs
    Then mw watch says "down signs=blog"
    And mw watch leaves with the status 6

  Scenario: A host that stays down is still down
    Given the outside places answer
    And the ssh read fails
    And mw watch runs
    And 3 minutes pass
    And mw watch runs
    And 15 minutes pass
    When mw watch runs
    Then mw watch says "down signs=none"
    And mw watch leaves with the status 6

  Scenario: An ok check in between clears the count
    Given the outside places answer
    And the ssh read fails
    And mw watch runs
    And 2 minutes pass
    And the health line of the watched host is 1 minute old and ends "verdict=ok"
    And mw watch runs
    And the ssh read fails
    And 2 minutes pass
    When mw watch runs
    Then mw watch says "unreachable-once signs=none"
    And mw watch leaves with the status 0
    And the watch memory holds the first failure at "2026-09-19T12:04:00Z"

  Scenario: An ssh that answers even with an unwell line clears the count
    Given the outside places answer
    And the ssh read fails
    And mw watch runs
    And 4 minutes pass
    And the health line of the watched host is 1 minute old and ends "verdict=unwell:disk_pct"
    And mw watch runs
    Then mw watch says "unwell disk_pct"
    And the watch memory holds no failure

  Scenario: With no watch table there is nothing to watch
    Given the config has no watch table
    When mw watch runs
    Then mw watch says "nothing to watch"
    And mw watch leaves with the status 0
    And nothing outside was tried and ssh was not tried
    And the watch memory was never touched
    And the watch log holds exactly 1 line
    And the last watch log line is "2026-09-19T12:00:00Z nothing to watch"
