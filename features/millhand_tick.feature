Feature: mw millhand tick
  mw millhand tick is what the routine timer runs, every 15 minutes, for ever. It
  spends no tokens unless something needs the Millhand. It looks, in this order:

    1. If a Millhand window is already open, it says "already up" and stops.
    2. It runs one mw sync, so that this host sees the other one's mail and claims.
    3. It looks for mail addressed to this host's Millhand, millhand@<host> or
       plain millhand, and for a story mw sweep newly finds stuck on this host.
    4. Nothing of either: it says "quiet" and starts nothing.
    5. Either, or both: it starts ONE routine wake of the Millhand, whose reason
       names the mail subjects and the stuck story titles, at most 5 of each and
       then a count.

  It prints one dated line and appends it to a log on this host. It reads the
  mail and marks none of it read. It leaves with 0 whatever it found: a tick that
  did what it should is not a failure. --dry-run says what it would do and starts
  nothing; it does not sweep either, because a sweep records the stories it finds
  stuck, and a story recorded by a rehearsal would never wake anybody. It does not
  consult mw watch.

  Background:
    Given a vault holding the "millhand" seat
    And the "millhand" seat's charter
    And the "millhand" seat keeps its handoffs on this host, and has written:
      | 2026-09-19-04 |
    And today is "2026-09-19"

  Scenario: A Millhand already up starts nothing, and nothing else is looked at
    Given the window "millhand-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up"
    And no window was opened
    And the tick did not sync
    And the tick log holds that line

  Scenario: Nothing to do is quiet and starts nothing
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And the tick synced once
    And no window was opened
    And no reaper was armed
    And the tick log holds that line

  Scenario: Mail wakes the Millhand once, and the reason names the subject
    Given unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "Please look at the queue"
    And exactly one window was opened
    And the window's command carries:
      | --model sonnet |
      | --effort high  |
    And the kickoff prompt of the window holds:
      | a routine wake           |
      | 1 unread message         |
      | Please look at the queue |
    And the tick log holds that line
    And no tick mail was marked read

  Scenario: Mail to the seat without a host wakes the Millhand too
    Given unread tick mail for "millhand" with the subject "For whoever is on"
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window holds:
      | For whoever is on |

  Scenario: A stuck story wakes the Millhand once, and the reason names it
    Given the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "Teach the cat to sit"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake       |
      | Teach the cat to sit |
      | mw-tk.1              |

  Scenario: A stuck story wakes the Millhand once and is not reported again
    Given the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run
    Then exactly one window was opened
    When the Millhand's window is closed
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    And exactly one window was opened

  Scenario: Mail and a stuck story together wake the Millhand once
    Given unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    And the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | Please look at the queue |
      | Teach the cat to sit     |

  Scenario: The reason names at most five of each, and counts the rest
    Given 7 unread tick messages for "millhand@laptop" with the subjects "Mail A" to "Mail G"
    And 7 claimed stories with no session behind them, titled "Story A" to "Story G"
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window holds:
      | 7 unread messages |
      | Mail A            |
      | Mail E            |
      | and 2 more        |
      | 7 stuck stories   |
      | Story A           |
      | Story E           |
    And the kickoff prompt of the window holds none of:
      | Mail F  |
      | Mail G  |
      | Story F |
      | Story G |

  Scenario: Mail to another mailbox is ignored
    Given unread tick mail for "mayor" with the subject "For the Mayor"
    And unread tick mail for "millhand@vps" with the subject "For the other host's Millhand"
    And unread tick mail for "builder@laptop" with the subject "For a Builder"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    And no window was opened

  Scenario: A dry run says what it would do and starts nothing
    Given unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run as a dry run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "dry run"
    And mw millhand tick prints one dated line saying "would wake"
    And mw millhand tick prints one dated line saying "Please look at the queue"
    And no window was opened
    And no reaper was armed
    And no tick mail was marked read

  Scenario: A dry run does not record a story as stuck
    Given the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run as a dry run
    Then mw millhand tick prints one dated line saying "dry run"
    And no window was opened
    And the story "mw-tk.1" is not recorded as stuck by the tick
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window holds:
      | Teach the cat to sit |

  Scenario: A failed sync is reported in the line, and the tick still checks locally
    Given the sync fails saying "the beads database has a merge conflict"
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "sync failed"
    And mw millhand tick prints one dated line saying "the beads database has a merge conflict"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | Please look at the queue |

  Scenario: A failed sync with nothing local to do is quiet, and says so
    Given the sync fails saying "the beads database has a merge conflict"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And mw millhand tick prints one dated line saying "sync failed"
    And no window was opened

  Scenario: Mail that cannot be read is not quiet, and is a failure
    Given the tick mail cannot be read
    When mw millhand tick is run
    Then mw millhand tick fails
    And mw millhand tick prints one dated line saying "could not tell whether the Millhand is needed"
    And mw millhand tick prints one dated line saying "mail could not be read"
    And no window was opened
    And the tick log holds that line

  Scenario: Mail that cannot be read does not keep a stuck story from waking the Millhand
    Given the tick mail cannot be read
    And the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "mail could not be read"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | Teach the cat to sit |

  Scenario: A tick is one line even when the sync's complaint is many
    Given the sync fails saying "first line\nsecond line"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "first line second line"

  Scenario: A Millhand that comes up between the check and the wake is not a failure
    Given a Millhand window opens while the tick syncs
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up"

  Scenario: A wake that fails is reported in the line and is a failure
    Given the "millhand" seat has no charter
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick fails
    And mw millhand tick prints one dated line saying "wake failed"
    And no window was opened
    And the tick log holds that line

  Scenario: Every tick adds one line to the log
    When mw millhand tick is run
    And mw millhand tick is run
    Then the tick log holds 2 lines
