Feature: mw millhand
  mw millhand brings up this host's Millhand: a thin command over `mw seat up
  millhand`. There are two kinds of wake, each on its own model, and a third for
  when the Governor is here. A routine wake and a wake by hand run Sonnet at high
  effort; a review wake runs Opus at high effort. Every one arms the idle reaper,
  so the window closes itself once the Millhand has handed off. The kickoff names
  the kind of wake and the reason for it.

  For a review wake, mw also passes what the review mark holds — the file
  seats/millhand/hosts/<host>/review-since — when it is there. mw only reads the
  mark: the Millhand moves it in its handoff.

  If a Millhand window is already open on this host, mw starts nothing, says so
  in one line and leaves with status 5, so a timer can tell "already up" from a
  failure.

  Background:
    Given a vault holding the "millhand" seat
    And the "millhand" seat's charter
    And the "millhand" seat keeps its handoffs on this host, and has written:
      | 2026-09-19-04 |
    And today is "2026-09-19"

  Scenario: A wake by hand is the default, on Sonnet at high effort with the idle reaper armed
    When mw millhand is run with no wake named
    Then mw millhand succeeds
    And exactly one window was opened
    And the window is named "millhand-2026-09-19-05"
    And the window's command carries:
      | --model sonnet |
      | --effort high  |
    And a reaper was armed on the window "millhand-2026-09-19-05" in when-idle mode for the "millhand" seat
    And the kickoff prompt of the window holds:
      | a wake by hand |

  Scenario: A routine wake runs Sonnet at high effort with the idle reaper armed
    When mw millhand is run for a "routine" wake
    Then mw millhand succeeds
    And exactly one window was opened
    And the window's command carries:
      | --model sonnet |
      | --effort high  |
    And a reaper was armed on the window "millhand-2026-09-19-05" in when-idle mode for the "millhand" seat
    And the kickoff prompt of the window holds:
      | a routine wake |

  Scenario: A review wake runs Opus at high effort with the idle reaper armed
    When mw millhand is run for a "review" wake
    Then mw millhand succeeds
    And exactly one window was opened
    And the window's command carries:
      | --model opus  |
      | --effort high |
    And a reaper was armed on the window "millhand-2026-09-19-05" in when-idle mode for the "millhand" seat
    And the kickoff prompt of the window holds:
      | a review wake |

  Scenario: The kind of wake and the reason reach the kickoff, with the newest handoff
    When mw millhand is run for a "routine" wake for the reason "the mail timer saw a message"
    Then mw millhand succeeds
    And the kickoff prompt of the window holds:
      | a routine wake                                              |
      | the mail timer saw a message                                |
      | seats/millhand/hosts/laptop/handoffs/2026-09-19-04.md       |
    And the kickoff prompt of the window holds none of:
      | review mark |

  Scenario: A review wake is passed the review mark when there is one
    Given the "millhand" seat's review mark says "2026-09-18T20:15:00Z"
    When mw millhand is run for a "review" wake
    Then mw millhand succeeds
    And the kickoff prompt of the window holds:
      | review mark          |
      | 2026-09-18T20:15:00Z |

  Scenario: A review wake with no review mark still starts, and passes none
    When mw millhand is run for a "review" wake
    Then mw millhand succeeds
    And exactly one window was opened
    And the kickoff prompt of the window holds none of:
      | 2026-09-18T20:15:00Z |

  Scenario: The review mark is not passed to a wake that is not a review
    Given the "millhand" seat's review mark says "2026-09-18T20:15:00Z"
    When mw millhand is run for a "routine" wake
    Then mw millhand succeeds
    And the kickoff prompt of the window holds none of:
      | 2026-09-18T20:15:00Z |

  Scenario: A Millhand window already up starts nothing and leaves with status 5
    Given the window "millhand-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    When mw millhand is run for a "routine" wake
    Then mw millhand is refused saying the Millhand is already up in "millhand-2026-09-19-05"
    And mw millhand leaves with the status 5
    And no window was opened
    And no reaper was armed

  Scenario: Whatever wakes it, a Millhand window already up starts nothing
    Given the window "millhand-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    When mw millhand is run for a "review" wake
    Then mw millhand leaves with the status 5
    And no window was opened

  Scenario: Another seat's window does not count as the Millhand's
    Given the window "mayor-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    When mw millhand is run for a "routine" wake
    Then mw millhand succeeds
    And exactly one window was opened

  Scenario: A failure is not the status of a Millhand already up
    Given the "millhand" seat has no charter
    When mw millhand is run for a "routine" wake
    Then mw millhand fails
    And mw millhand leaves with the status 1
    And no window was opened

  Scenario: The config key millhand_routine_model overrides the routine model
    Given the config file sets "millhand_routine_model" to "haiku"
    When mw millhand is run for a "routine" wake
    Then mw millhand succeeds
    And the window's command carries:
      | --model haiku |

  Scenario: The config key millhand_review_model overrides the review model
    Given the config file sets "millhand_review_model" to "fable"
    When mw millhand is run for a "review" wake
    Then mw millhand succeeds
    And the window's command carries:
      | --model fable |

  Scenario: A wake by hand follows the routine model key
    Given the config file sets "millhand_routine_model" to "haiku"
    When mw millhand is run with no wake named
    Then mw millhand succeeds
    And the window's command carries:
      | --model haiku |

  Scenario: A kind of wake mw does not know is refused
    When mw millhand is run for a "nightly" wake
    Then mw millhand is refused saying "nightly" is not a kind of wake
    And no window was opened
