Feature: mw millhand tick
  mw millhand tick is what the routine timer runs, every 15 minutes, for ever. It
  spends no tokens unless something needs the Millhand. It looks, in this order:

    1. If a Millhand window is already open, it says "already up" and stops,
       unless that Millhand is finished: it has written a handoff newer than its
       window and its pane is idle at an empty input line on two checks running,
       the rule mw seat reap --when-idle closes by. Then the tick closes the
       window, adds one line to the Millhand's reaper log, says so in its line and
       carries on as if no Millhand were up, so the same tick may wake a fresh
       one. A window whose input line holds text is never closed.
       A Millhand whose wake never got going is restarted: its pane is idle at
       an empty input line on the same two checks and it has written no
       handoff since its window opened. The tick closes the window, says
       "restarted: up but idle since <opened>, no handoff" and wakes a fresh
       Millhand, telling it so. A working pane, text on the input line or a
       pane that cannot be read is still "already up".
    2. It runs one mw sync, so that this host sees the other one's mail and claims.
    3. It looks for mail in every mailbox this host's Millhand reads —
       millhand@<host>, plain millhand, and the host's own box <host>, which the
       Millhand reads once it is up — and for a story mw sweep newly finds stuck
       on this host.
    4. Nothing of either: it says "quiet" and starts nothing.
    5. Either, or both: it starts ONE routine wake of the Millhand, whose reason
       names each mailbox's unread mail, box by box, and the stuck story titles,
       at most 5 of each and then a count.

  On a host with a [watch] table the tick also applies mw watch's rule to the
  host it watches, before it syncs: unwell, stale and down are each a reason for
  the wake, carrying the watch line verbatim, and one wake serves them with the
  mail and the stuck stories. ok and unreachable-once wake nobody. local-fault,
  this host's own network being down, wakes nobody and skips the sync, which
  would only time out; the tick still looks at its own mail and stories. When the
  Mayor's process is gone, or the host is down, the reason ends with the charter's
  one exception. With no [watch] table the tick does not consult mw watch.

  It prints one dated line and appends it to a log on this host. It reads the
  mail and marks none of it read. It leaves with 0 whatever it found: a tick that
  did what it should is not a failure. --dry-run says what it would do and starts
  nothing; it does not sweep either, because a sweep records the stories it finds
  stuck, and a story recorded by a rehearsal would never wake anybody. For the same
  reason it does not run mw watch, which remembers a failed check.

  Background:
    Given a vault holding the "millhand" seat
    And the "millhand" seat's charter
    And the "millhand" seat keeps its handoffs on this host, and has written:
      | 2026-09-19-04 |
    And today is "2026-09-19"

  Scenario: A Millhand already up starts nothing, and nothing else is looked at
    Given the window "millhand-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    And the pane of the window "millhand-2026-09-19-05" is working
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up"
    And no window was opened
    And the tick did not sync
    And the tick log holds that line

  Scenario: A finished Millhand's window is closed and the same tick wakes a fresh Millhand for waiting mail
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "closed the finished Millhand's window millhand-2026-09-19-04"
    And mw millhand tick prints one dated line saying "woke the Millhand"
    And the window "millhand-2026-09-19-04" was closed
    And the tick synced once
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake           |
      | Please look at the queue |
    And the window's session denies a permission it would have asked for
    And the reaper log holds one line saying "closed by the tick: finished at 2026-09-19T06:00:00Z, window left open"
    And the tick log holds that line

  Scenario: A finished Millhand's window is closed, and with nothing waiting the tick says so
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "closed the finished Millhand's window millhand-2026-09-19-04"
    And mw millhand tick prints one dated line saying "quiet"
    And the window "millhand-2026-09-19-04" was closed
    And no window was opened
    And the reaper log holds one line saying "closed by the tick: finished at 2026-09-19T06:00:00Z, window left open"

  Scenario: A window with text on its input line is never closed
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    And the pane of the window "millhand-2026-09-19-04" has text on its input line
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-2026-09-19-04)"
    And the window "millhand-2026-09-19-04" was not closed
    And the tick did not sync
    And no window was opened
    And the reaper log holds no line

  Scenario: A working pane is already up
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    And the pane of the window "millhand-2026-09-19-04" is working
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-2026-09-19-04)"
    And the window "millhand-2026-09-19-04" was not closed
    And the reaper log holds no line

  Scenario: An idle Millhand with no handoff since its window opened is restarted
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "woke the Millhand"
    And mw millhand tick prints one dated line saying "restarted: up but idle since 2026-09-19T08:00:00Z, no handoff"
    And the window "millhand-test" was closed
    And the tick synced once
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake                                |
      | idle at its prompt since 2026-09-19T08:00:00Z |
      | no handoff                                    |
    And the reaper log holds one line saying "closed by the tick: up but idle since 2026-09-19T08:00:00Z, no handoff"
    And the tick log holds that line
    And the tick log counts as a good run

  Scenario: A restarted Millhand is told of the mail that is waiting too
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "restarted: up but idle since 2026-09-19T08:00:00Z, no handoff"
    And the window "millhand-test" was closed
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | Please look at the queue |
      | no handoff               |

  Scenario: A working pane with no handoff since its window opened is already up
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    And the pane of the window "millhand-test" is working
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-test)"
    And the window "millhand-test" was not closed
    And the tick did not sync
    And no window was opened
    And the reaper log holds no line

  Scenario: Text on the input line with no handoff since the window opened is already up
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    And the pane of the window "millhand-test" has text on its input line
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-test)"
    And the window "millhand-test" was not closed
    And no window was opened
    And the reaper log holds no line

  Scenario: A pane that cannot be read with no handoff since the window opened is already up
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    And the terminal cannot say what the pane of the window "millhand-test" is doing
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-test)"
    And the window "millhand-test" was not closed
    And no window was opened
    And the reaper log holds no line

  Scenario: An idle pane with no handoff that turns busy between the checks is already up
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    And the pane of the window "millhand-test" turns to text on its input line while the tick waits between its checks
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-test)"
    And the window "millhand-test" was not closed
    And no window was opened
    And the reaper log holds no line

  Scenario: A dry run says it would restart an idle Millhand with no handoff, and closes nothing
    Given the window "millhand-test" was opened at "2026-09-19T08:00:00Z"
    When mw millhand tick is run as a dry run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "dry run: would wake the Millhand"
    And mw millhand tick prints one dated line saying "would be restarted: up but idle since 2026-09-19T08:00:00Z, no handoff"
    And the window "millhand-test" was not closed
    And no window was opened
    And the reaper log holds no line

  Scenario: A pane that is idle on the first check but busy on the second is already up
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    And the pane of the window "millhand-2026-09-19-04" turns to text on its input line while the tick waits between its checks
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-2026-09-19-04)"
    And the window "millhand-2026-09-19-04" was not closed
    And the reaper log holds no line

  Scenario: A terminal that cannot be asked about a pane leaves the window alone
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    And the terminal cannot say what the pane of the window "millhand-2026-09-19-04" is doing
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "already up (millhand-2026-09-19-04)"
    And the window "millhand-2026-09-19-04" was not closed
    And the reaper log holds no line

  Scenario: A dry run says it would close a finished window and closes nothing
    Given the window "millhand-2026-09-19-04" was opened at "2026-09-19T05:00:00Z"
    When mw millhand tick is run as a dry run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "dry run: would close the finished Millhand's window millhand-2026-09-19-04"
    And the window "millhand-2026-09-19-04" was not closed
    And the reaper log holds no line
    And no window was opened

  Scenario: Nothing to do is quiet and starts nothing
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And the tick synced once
    And no window was opened
    And no reaper was armed
    And the tick log holds that line
    And the tick log counts as a good run

  Scenario: Mail wakes the Millhand once, and the reason names the subject and its box
    Given unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "Please look at the queue"
    And exactly one window was opened
    And the window's command carries:
      | --model sonnet |
      | --effort high  |
    And the kickoff prompt of the window holds:
      | a routine wake              |
      | 1 unread in millhand@laptop |
      | Please look at the queue    |
    And the tick log holds that line
    And no tick mail was marked read

  Scenario: Mail to the seat without a host wakes the Millhand too
    Given unread tick mail for "millhand" with the subject "For whoever is on"
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window holds:
      | 1 unread in millhand: |
      | For whoever is on     |

  Scenario: Mail in the host's own box wakes the Millhand, and the reason names that box
    Given unread tick mail for "laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "1 unread in laptop"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake           |
      | 1 unread in laptop:      |
      | Please look at the queue |
    And the tick log holds that line
    And no tick mail was marked read

  Scenario: The three boxes the Millhand reads are each counted, not merged into one
    Given unread tick mail for "millhand@laptop" with the subject "For the host-qualified box"
    And unread tick mail for "millhand" with the subject "For the plain box"
    And unread tick mail for "laptop" with the subject "For the host's own box"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | 1 unread in millhand@laptop |
      | For the host-qualified box  |
      | 1 unread in millhand:       |
      | For the plain box           |
      | 1 unread in laptop:         |
      | For the host's own box      |

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
      | 7 unread in millhand@laptop |
      | Mail A                      |
      | Mail E                      |
      | and 2 more                  |
      | 7 stuck stories             |
      | Story A                     |
      | Story E                     |
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

  Scenario: A doctor note wakes the Millhand once, naming the check and its text
    Given a doctor note "wifi" saying "2026-09-19T11:00:00Z cannot-tell faulty (waiting 5m)"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "woke the Millhand"
    And mw millhand tick prints one dated line saying "doctor: wifi: 2026-09-19T11:00:00Z cannot-tell"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake       |
      | doctor: wifi         |
      | Run `mw doctor wifi` |

  Scenario: The same doctor note is not woken for twice
    Given a doctor note "wifi" saying "2026-09-19T11:00:00Z cannot-tell faulty (waiting 5m)"
    When mw millhand tick is run
    Then exactly one window was opened
    When the Millhand's window is closed
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    And exactly one window was opened

  Scenario: A doctor note that clears and returns wakes the Millhand again
    Given a doctor note "wifi" saying "2026-09-19T11:00:00Z cannot-tell faulty (waiting 5m)"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "woke the Millhand"
    When the Millhand's window is closed
    And the doctor note "wifi" is cleared
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    When a doctor note "wifi" saying "2026-09-19T12:30:00Z cannot-tell faulty (waiting 5m) again"
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "woke the Millhand"

  Scenario: A doctor note rewritten with only a new timestamp and log tail is not woken for again
    Given a doctor note "wifi" saying "2026-09-19T11:00:00Z cannot-tell faulty (waiting 5m) | last log lines: a | b | c"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "woke the Millhand"
    When the Millhand's window is closed
    And a doctor note "wifi" saying "2026-09-19T11:05:00Z cannot-tell faulty (waiting 5m) | last log lines: d | e | f"
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    And exactly one window was opened

  Scenario: A doctor note that changes verdict wakes the Millhand again, with no clear between
    Given a doctor note "wifi" saying "2026-09-19T11:00:00Z cannot-tell faulty (waiting 5m)"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "woke the Millhand"
    When the Millhand's window is closed
    And a doctor note "wifi" saying "2026-09-19T12:00:00Z faulty no reach"
    And mw millhand tick is run
    Then mw millhand tick prints one dated line saying "woke the Millhand"

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
    And the tick log counts as a failed run

  Scenario: Mail that cannot be read is not quiet, and is a failure
    Given the tick mail cannot be read
    When mw millhand tick is run
    Then mw millhand tick fails
    And mw millhand tick prints one dated line saying "could not tell whether the Millhand is needed"
    And mw millhand tick prints one dated line saying "mail could not be read"
    And no window was opened
    And the tick log holds that line
    And the tick log counts as a failed run

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
    And the tick log counts as a good run

  Scenario: A wake that fails is reported in the line and is a failure
    Given the "millhand" seat has no charter
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick fails
    And mw millhand tick prints one dated line saying "wake failed"
    And no window was opened
    And the tick log holds that line
    And the tick log counts as a failed run

  Scenario: Every tick adds one line to the log
    When mw millhand tick is run
    And mw millhand tick is run
    Then the tick log holds 2 lines

  Scenario: A host that is unwell wakes the Millhand once, with the watch line
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=unwell:load1"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "woke the Millhand"
    And mw millhand tick prints one dated line saying "unwell load1"
    And the tick synced once
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake |
      | unwell load1   |
    And the kickoff prompt of the window holds none of:
      | respawn |
    And the tick log holds that line

  Scenario: A host whose health line is stale wakes the Millhand once
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 50 minutes old and ends "verdict=ok"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake |
      | stale          |

  Scenario: A host that is down wakes the Millhand once, with the watch line
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And ssh to the watched host fails
    And the watched host first failed a check 10 minutes ago
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "down signs=none"
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | a routine wake  |
      | down signs=none |

  Scenario: A host that is well wakes nobody
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=ok"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And the tick synced once
    And no window was opened
    And no reaper was armed

  Scenario: A host that could not be reached once wakes nobody, and the line says so
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And ssh to the watched host fails
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And mw millhand tick prints one dated line saying "unreachable-once signs=none"
    And no window was opened

  Scenario: A local fault wakes nobody and skips the sync
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick cannot reach either outside place
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "quiet"
    And mw millhand tick prints one dated line saying "local-fault"
    And the tick did not sync
    And ssh to the watched host was not tried
    And no window was opened
    And the tick log holds that line
    And the tick log counts as a local network fault

  Scenario: A local fault does not keep this host's own mail from waking the Millhand
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick cannot reach either outside place
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And mw millhand tick prints one dated line saying "local-fault"
    And the tick did not sync
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | Please look at the queue |
    And the kickoff prompt of the window holds none of:
      | local-fault |

  Scenario: Mail and an unwell host still wake the Millhand once, with both reasons
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=unwell:disk_pct"
    And unread tick mail for "millhand@laptop" with the subject "Please look at the queue"
    When mw millhand tick is run
    Then mw millhand tick succeeds
    And exactly one window was opened
    And the kickoff prompt of the window holds:
      | 1 unread in millhand@laptop |
      | Please look at the queue    |
      | unwell disk_pct             |

  Scenario: A stuck story and an unwell host still wake the Millhand once
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=unwell:disk_pct"
    And the story "mw-tk.1" titled "Teach the cat to sit" is claimed here with no session behind it
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window holds:
      | Teach the cat to sit |
      | unwell disk_pct      |

  Scenario: A Mayor that is gone ends the reason with the charter's one exception
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=unwell:load1,mayor_gone"
    When mw millhand tick is run
    Then exactly one window was opened
    And the kickoff prompt of the window ends with "If the Mayor's process is gone and no handoff is under way you may run the one respawn command on the VPS."
    And the kickoff prompt of the window holds:
      | unwell load1,mayor_gone |

  Scenario: A host that is down ends the reason with the charter's one exception too
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And ssh to the watched host fails
    And the watched host first failed a check 10 minutes ago
    When mw millhand tick is run
    Then the kickoff prompt of the window ends with "If the Mayor's process is gone and no handoff is under way you may run the one respawn command on the VPS."

  Scenario: A watch that could not be run with nothing else to wake for is not quiet, and is a failure
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the tick's watch memory cannot be read
    When mw millhand tick is run
    Then mw millhand tick fails
    And mw millhand tick prints one dated line saying "could not tell whether the Millhand is needed"
    And mw millhand tick prints one dated line saying "watch failed"
    And no window was opened

  Scenario: With no watch table the tick does not consult mw watch
    Given the tick can reach the outside places
    And the watched host's health line is 50 minutes old and ends "verdict=unwell:mayor_gone"
    When mw millhand tick is run
    Then mw millhand tick prints one dated line saying "quiet"
    And the tick synced once
    And no window was opened
    And nothing was asked of the watched host or the outside places

  Scenario: A dry run does not run mw watch
    Given the tick watches the host "vps" over ssh "vps-ssh", with the outside places "https://one.example" and "https://two.example"
    And the tick can reach the outside places
    And the watched host's health line is 5 minutes old and ends "verdict=unwell:load1"
    When mw millhand tick is run as a dry run
    Then mw millhand tick prints one dated line saying "dry run: quiet"
    And mw millhand tick prints one dated line saying "not run in a dry run"
    And no window was opened
    And nothing was asked of the watched host or the outside places
