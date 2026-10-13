Feature: The follower files the monthly contribute-back review on the home
  On the first of each month (or the day the config says), the home's follower
  files one research bead under mw-gq6, "Contribute-back review <YYYY-MM>",
  labelled wayfinder:research, asking which pieces built for a gap in an
  open-source project would make a welcome upstream pull request. The review's
  picks go to the Governor as decision cards; nothing is sent upstream without
  his word. A month already filed is not filed again.

  Background:
    Given the contribute-back follower is running on "laptop" and the home is "laptop"
    And the contribute-back review is due on day 1

  Scenario: It files one bead on the 1st, and none on a second run that month
    Given the contribute-back clock reads "2026-11-01 08:00" UTC
    When the contribute-back follower runs a cycle
    Then 1 review bead titled "Contribute-back review 2026-11" is filed
    And that bead is labelled "wayfinder:research" and is under "mw-gq6"
    And that bead asks which code we wrote to fill a gap would be welcome upstream
    When the contribute-back clock reads "2026-11-01 20:00" UTC
    And the contribute-back follower runs a cycle
    Then 1 review bead titled "Contribute-back review 2026-11" is filed

  Scenario: The config key sets the day
    Given the contribute-back review is due on day 15
    And the contribute-back clock reads "2026-11-14 08:00" UTC
    When the contribute-back follower runs a cycle
    Then 0 review beads are filed
    When the contribute-back clock reads "2026-11-15 08:00" UTC
    And the contribute-back follower runs a cycle
    Then 1 review bead titled "Contribute-back review 2026-11" is filed

  Scenario: A follower that was down on the day files the month's review when it is back
    Given the contribute-back clock reads "2026-11-09 08:00" UTC
    When the contribute-back follower runs a cycle
    Then 1 review bead titled "Contribute-back review 2026-11" is filed

  Scenario: The next month is filed again
    Given the contribute-back clock reads "2026-11-01 08:00" UTC
    When the contribute-back follower runs a cycle
    And the contribute-back clock reads "2026-12-01 08:00" UTC
    And the contribute-back follower runs a cycle
    Then 1 review bead titled "Contribute-back review 2026-11" is filed
    And 1 review bead titled "Contribute-back review 2026-12" is filed

  Scenario: Only the home files it
    Given the contribute-back follower is running on "desktop" and the home is "laptop"
    And the contribute-back clock reads "2026-11-01 08:00" UTC
    When the contribute-back follower runs a cycle
    Then 0 review beads are filed

  Scenario: Its pass is told in the log as the job contribute-back@<host>
    Given the contribute-back clock reads "2026-11-01 08:00" UTC
    When the contribute-back follower runs a cycle
    Then the contribute-back log tells the job "contribute-back@laptop" as scheduled, running, done
