Feature: mw status
  mw status reads, for this host, what is running, what is ready, what is
  blocked, and today's fuel — and, for every other host, when it last synced
  and what is pathed to it, so that work stranded on a host that has gone
  quiet is seen and re-pathed by hand. It writes nothing at all: no claim, no
  comment, no state, no note, no ledger line, no session. Every line of the
  report fits a phone-width terminal, at most 60 columns, however long a
  title is.

  Background:
    Given the status epic "mw-gq6" on the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A running story is listed under RUNNING, with its session name
    Given a status story "mw-gq6.1" filed under it
    And the status story "mw-gq6.1" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.1" running with session "mw-gq6_1"

  Scenario: Ready and blocked stories are listed under their own headings
    Given a status story "mw-gq6.2" filed under it
    And a status story "mw-gq6.3" filed under it, waiting on "mw-gq6.2"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.2" as ready
    And the report lists "mw-gq6.3" as blocked

  Scenario: A ready story that any host may take is listed under ready, and says so
    Given a status story "mw-gq6.30" filed under it, overriding "host" with "auto"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.30" as ready
    And the report says "mw-gq6.30" is "host auto"
    And the report does not list "mw-gq6.30" under any other host

  Scenario: A ready story labelled hitl is listed under its own heading, not under ready
    Given a status story "mw-gq6.23" filed under it
    And the status story "mw-gq6.23" is labelled "hitl"
    And a status story "mw-gq6.24" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.23" as waiting for the Governor
    And the report does not list "mw-gq6.23" as ready
    And the report lists "mw-gq6.24" as ready

  Scenario: A claimed story labelled hitl is listed under its own heading, not under running
    Given a status story "mw-gq6.25" filed under it
    And the status story "mw-gq6.25" is labelled "hitl"
    And the status story "mw-gq6.25" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.25" as waiting for the Governor
    And the report does not show "mw-gq6.25" as running

  Scenario: With no story labelled hitl the report has no heading for the Governor
    Given a status story "mw-gq6.26" filed under it
    And a status story "mw-gq6.27" filed under it
    And the status story "mw-gq6.27" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for stories waiting for the Governor

  Scenario: An open bead labelled hitl with no path is listed under the heading for the Governor
    Given a status bead "mw-6ww.30" titled "Governor: turn on the VPS mail notifier" filed under no epic
    And the status story "mw-6ww.30" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.30" as waiting for the Governor
    And the report shows "mw-6ww.30" under the heading for the Governor with its title and no rig or host line

  Scenario: A pathless bead labelled hitl is listed on whichever host is asked
    Given a status bead "mw-6ww.31" titled "Governor: pick the Laptop's name" filed under no epic
    And the status story "mw-6ww.31" is labelled "hitl"
    And a status story "mw-gq6.35" filed under it, overriding "host" with "laptop"
    And the status story "mw-gq6.35" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.31" as waiting for the Governor
    And the report lists "mw-gq6.35" as waiting for the Governor

  Scenario: A closed bead labelled hitl is not listed under the heading for the Governor
    Given a status bead "mw-6ww.32" titled "Governor: an errand already done" filed under no epic
    And the status story "mw-6ww.32" is labelled "hitl"
    And the status story "mw-6ww.32" is finished
    When mw status reads the host
    Then reading status succeeds
    And the report does not list "mw-6ww.32" as waiting for the Governor
    And the report has no heading for stories waiting for the Governor

  Scenario: A bead labelled hitl that an open bead blocks is not listed under the heading for the Governor
    Given a status bead "mw-6ww.33" titled "Governor: the errand that comes first" filed under no epic
    And a status bead "mw-6ww.34" titled "Governor: the errand that waits" filed under no epic, waiting on "mw-6ww.33"
    And the status story "mw-6ww.34" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report does not list "mw-6ww.34" as waiting for the Governor
    And the report has no heading for stories waiting for the Governor

  Scenario: A bead labelled hitl is listed once the bead that blocked it is finished
    Given a status bead "mw-6ww.36" titled "Governor: the errand that came first" filed under no epic
    And a status bead "mw-6ww.37" titled "Governor: the errand that waited" filed under no epic, waiting on "mw-6ww.36"
    And the status story "mw-6ww.37" is labelled "hitl"
    And the status story "mw-6ww.36" is finished
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.37" as waiting for the Governor

  Scenario: A bead that is not labelled hitl is not listed under the heading for the Governor
    Given a status bead "mw-6ww.38" titled "A loose ticket for nobody in particular" filed under no epic
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for stories waiting for the Governor

  Scenario: A bead labelled hitl and hitl:todo is listed under his to do, not under the heading for the Governor
    Given a status bead "mw-6ww.114" titled "Renew the passport" filed under no epic
    And the status story "mw-6ww.114" is labelled "hitl" and "hitl:todo"
    When mw status reads the host
    Then reading status succeeds
    And the report does not list "mw-6ww.114" as waiting for the Governor
    And the report lists "mw-6ww.114" under "HIS TO DO (1)"

  Scenario: A bead labelled hitl alone stays under the heading for the Governor beside his to do
    Given a status bead "mw-6ww.115" titled "Renew the passport" filed under no epic
    And the status story "mw-6ww.115" is labelled "hitl" and "hitl:todo"
    And a status bead "mw-6ww.116" titled "Governor: pick a name" filed under no epic
    And the status story "mw-6ww.116" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.116" as waiting for the Governor
    And the report does not list "mw-6ww.116" under "HIS TO DO (1)"

  Scenario: With no bead labelled hitl:todo the report has no HIS TO DO section
    Given a status bead "mw-6ww.117" titled "Governor: pick a name" filed under no epic
    And the status story "mw-6ww.117" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report has no HIS TO DO section

  Scenario: The beads waiting for the Governor are listed by title, most urgent first
    Given a status bead "mw-6ww.40" titled "Governor: a routine errand" filed under no epic
    And the status story "mw-6ww.40" is labelled "hitl"
    And a status bead "mw-6ww.41" titled "Governor: an urgent errand" filed under no epic
    And the status story "mw-6ww.41" is labelled "hitl"
    And the status story "mw-6ww.41" is at priority 0
    And a status story "mw-gq6.36" titled "Governor: a story that is pathed" filed under it
    And the status story "mw-gq6.36" is labelled "hitl"
    And the status story "mw-gq6.36" is at priority 1
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.41" before "mw-gq6.36" under the heading for the Governor
    And the report lists "mw-gq6.36" before "mw-6ww.40" under the heading for the Governor

  Scenario: The heading for the Governor fits a phone screen too
    Given a status story "mw-gq6.28" titled "A story whose title runs on and on and on and on and on and on and on and on, well past a phone screen" filed under it
    And the status story "mw-gq6.28" is labelled "hitl"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.28" as waiting for the Governor
    And every line of the report is at most 60 columns wide

  Scenario: A blocked story lists only what it is still waiting for
    Given a status story "mw-gq6.20" filed under it
    And a status story "mw-gq6.21" filed under it
    And a status story "mw-gq6.22" filed under it, waiting on "mw-gq6.20" and "mw-gq6.21"
    And the status story "mw-gq6.20" is finished
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.22" needs "mw-gq6.21" and nothing else

  Scenario: A story filed with path overrides only is shown with the epic's defaults filled in
    Given a status story "mw-gq6.4" filed under it, overriding "model" with "sonnet"
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.4" on the rig "millwright"

  Scenario: A claimed story with an open formula step says its close-out is blocked
    Given a status story "mw-gq6.5" filed under it
    And the status story "mw-gq6.5" is claimed with its session running
    And the formula poured for "mw-gq6.5" has a step still open
    When mw status reads the host
    Then reading status succeeds
    And the report says the close-out of "mw-gq6.5" is blocked by an open formula step

  Scenario: A story marked run=stopped is shown as stopped, not running
    Given a status story "mw-gq6.6" filed under it
    And the status story "mw-gq6.6" is claimed with its session running
    And the status story "mw-gq6.6" is marked run=stopped
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.6" as run=stopped, not running

  Scenario: A story marked run=stuck is shown as stuck, not running
    Given a status story "mw-gq6.7" filed under it
    And the status story "mw-gq6.7" is claimed with its session running
    And the status story "mw-gq6.7" is marked run=stuck
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.7" as run=stuck, not running

  Scenario: A refused story is shown as waiting on the Mayor, not as running
    Given a status story "mw-gq6.9" filed under it
    And the status story "mw-gq6.9" is claimed with its session running
    And the status story "mw-gq6.9" is marked run=blocked
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.9" as refused, waiting on the Mayor

  Scenario: A story whose session stopped at the usage limit says so (mw-gq6.433)
    Given a status story "mw-gq6.10" filed under it
    And the status story "mw-gq6.10" is claimed with its session running
    And the status story "mw-gq6.10" is marked run=blocked
    And the session of "mw-gq6.10" left a result saying "You've hit your weekly limit · resets Oct 16, 7am (UTC)"
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.10" as stopped at the usage limit

  Scenario: A story whose session stopped at the usage limit before it wrote its JSON says so too (mw-gq6.471)
    Given a status story "mw-gq6.10" filed under it
    And the status story "mw-gq6.10" is claimed with its session running
    And the status story "mw-gq6.10" is marked run=blocked
    And the session of "mw-gq6.10" printed "Claude AI usage limit reached|1792155600" instead of a result
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.10" as stopped at the usage limit

  # mw-1ljpo9.6: a Builder's session is capped at next.max_turns (120); one
  # past 100 turns is named while it runs, read from its transcript.
  Scenario: A running session past 100 turns is named with its turns so far
    Given a status story "mw-gq6.10" filed under it
    And the status story "mw-gq6.10" is claimed with its session running
    And a status story "mw-gq6.11" filed under it
    And the status story "mw-gq6.11" is claimed with its session running
    And the session of "mw-gq6.10" has taken 130 turns so far
    And the session of "mw-gq6.11" has taken 100 turns so far
    When mw status reads the host
    Then reading status succeeds
    And the report says "past 100 turns: 130 so far"
    And the report does not say "100 so far"
    And every line of the report is at most 60 columns wide

  Scenario: A story whose session failed for another reason is not shown at the usage limit
    Given a status story "mw-gq6.10" filed under it
    And the status story "mw-gq6.10" is claimed with its session running
    And the status story "mw-gq6.10" is marked run=blocked
    And the session of "mw-gq6.10" left a result saying "API Error: Connection lost mid-response"
    When mw status reads the host
    Then reading status succeeds
    And the report does not show "mw-gq6.10" as stopped at the usage limit

  Scenario: A story that is running is not shown as refused
    Given a status story "mw-gq6.9" filed under it
    And the status story "mw-gq6.9" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And the report does not show "mw-gq6.9" as refused

  Scenario: A story started more than once shows how many attempts it has had
    Given a status story "mw-gq6.8" filed under it
    And the status story "mw-gq6.8" is claimed with its session running
    And the status story "mw-gq6.8" has been started 2 times
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.8" with 2 attempts

  Scenario: A story given back after several attempts shows them among the ready
    Given a status story "mw-gq6.8" filed under it
    And the status story "mw-gq6.8" has been started 3 times
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.8" as ready
    And the report shows "mw-gq6.8" with 3 attempts

  Scenario: A story started once shows no attempts
    Given a status story "mw-gq6.8" filed under it
    And the status story "mw-gq6.8" is claimed with its session running
    And the status story "mw-gq6.8" has been started 1 time
    When mw status reads the host
    Then reading status succeeds
    And the report shows no attempts for "mw-gq6.8"

  Scenario: Today's fuel is summed from the ledger lines dated today
    Given the builder's ledger holds a line from today burning 12000 tokens
    And the builder's ledger holds a line from today burning 3000 tokens
    And the builder's ledger holds a line from 2020-01-01 burning 900000 tokens
    When mw status reads the host
    Then reading status succeeds
    And the report says today's fuel is 15,000 tokens

  Scenario: Today's fuel is zero when the seat has no ledger yet
    When mw status reads the host
    Then reading status succeeds
    And the report says today's fuel is 0 tokens

  Scenario: No line of the report exceeds 60 columns, even with a long title
    Given a status story "mw-gq6.8" titled "A story whose title runs on and on and on and on and on and on and on and on, well past a phone screen" filed under it
    And the status story "mw-gq6.8" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And every line of the report is at most 60 columns wide

  Scenario: mw status writes nothing
    Given a status story "mw-gq6.9" filed under it
    And a status story "mw-gq6.10" filed under it, waiting on "mw-gq6.9"
    And a status story "mw-gq6.11" filed under it
    And the status story "mw-gq6.11" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And nothing was written through the tracker, the ledger or the runner

  Scenario: A story pathed to another host that synced recently is listed under that host, not stranded
    Given a status story "mw-gq6.12" filed under it, overriding "host" with "laptop"
    And the host "laptop" last synced 1 hour ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.12" under the other host "laptop"
    And the report does not call the host "laptop" asleep

  Scenario: A story pathed to a host whose last sync is older than the threshold is stranded
    Given a status story "mw-gq6.13" filed under it, overriding "host" with "laptop"
    And the host "laptop" last synced 28 hours ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.13" as stranded on the host "laptop"
    And the report shows the last sync time of the host "laptop"
    And the report says how to re-path a story stranded on the host "laptop"

  Scenario: A host that has never synced is shown as never synced, and its work as stranded
    Given a status story "mw-gq6.14" filed under it, overriding "host" with "laptop"
    And the host "laptop" has never synced
    When mw status reads the host
    Then reading status succeeds
    And the report says the host "laptop" has never synced
    And the report lists "mw-gq6.14" as stranded on the host "laptop"

  Scenario: A story claimed on a sleeping host is stranded, like a ready one
    Given a status story "mw-gq6.15" filed under it, overriding "host" with "laptop"
    And the status story "mw-gq6.15" is claimed with its session running
    And the claim on "mw-gq6.15" was last renewed 28 hours ago
    And the host "laptop" last synced 28 hours ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.15" as stranded on the host "laptop"

  Scenario: How long a host may be silent is what the configuration says
    Given the configuration says a host is asleep after 6 hours
    And a status story "mw-gq6.16" filed under it, overriding "host" with "laptop"
    And the host "laptop" last synced 5 hours ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.16" under the other host "laptop"
    And the report does not call the host "laptop" asleep

  Scenario: A last sync note that is not a time is read as asleep, not as an error
    Given a status story "mw-gq6.17" filed under it, overriding "host" with "laptop"
    And the host "laptop" left a last sync note that is not a time
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.17" as stranded on the host "laptop"

  Scenario: The other hosts section fits a phone screen too
    Given a status story "mw-gq6.18" titled "A story whose title runs on and on and on and on and on and on and on and on, well past a phone screen" pathed to the host "laptop"
    And the host "laptop" last synced 28 hours ago
    When mw status reads the host
    Then reading status succeeds
    And every line of the report is at most 60 columns wide

  Scenario: Reading what another host holds writes nothing and re-paths nothing
    Given a status story "mw-gq6.19" filed under it, overriding "host" with "laptop"
    And the host "laptop" last synced 28 hours ago
    When mw status reads the host
    Then reading status succeeds
    And nothing was written through the tracker, the ledger or the runner
    And nothing pathed to another host was re-pathed or touched

  Scenario: A rig whose memory is under the budget shows no RIG MEMORY section at all
    Given the builder's memory of the rig "millwright" is 7999 bytes
    When mw status reads the host
    Then reading status succeeds
    And the report has no RIG MEMORY section

  Scenario: A rig whose memory is over the budget is named with its size and the budget
    Given the builder's memory of the rig "millwright" is 8412 bytes
    And the builder's memory of the rig "fellowship" is 300 bytes
    When mw status reads the host
    Then reading status succeeds
    And the report warns that the memory of the rig "millwright" is 8412 of 8000 bytes
    And the report does not warn about the memory of the rig "fellowship"
    And every line of the report is at most 60 columns wide

  Scenario: The budget of a rig's memory is what the configuration says
    Given the configuration says a rig's memory may be 500 bytes
    And the builder's memory of the rig "millwright" is 600 bytes
    And the builder's memory of the rig "fellowship" is 400 bytes
    When mw status reads the host
    Then reading status succeeds
    And the report warns that the memory of the rig "millwright" is 600 of 500 bytes
    And the report does not warn about the memory of the rig "fellowship"

  Scenario: A rig kept as facts is measured on what a Builder would read, and the fix named is retiring
    Given the builder's memory of the rig "millwright" is kept as facts that render to 8412 bytes
    And the builder's memory of the rig "fellowship" is kept as facts that render to 300 bytes
    When mw status reads the host
    Then reading status succeeds
    And the report warns that the memory of the rig "millwright" is 8412 of 8000 bytes
    And the report says to retire or supersede the facts of the rig "millwright"
    And the report does not warn about the memory of the rig "fellowship"
    And every line of the report is at most 60 columns wide

  Scenario: An archive of a rig's memory is never counted, however large
    Given the builder's memory of the rig "millwright" is 200 bytes
    And the builder's archive of the rig "millwright" is 90000 bytes
    When mw status reads the host
    Then reading status succeeds
    And the report has no RIG MEMORY section

  Scenario: A rig with no memory file is not an error
    Given a status story "mw-gq6.29" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report has no RIG MEMORY section

  Scenario: Reading the rigs' memory writes nothing
    Given the builder's memory of the rig "millwright" is 8412 bytes
    When mw status reads the host
    Then reading status succeeds
    And nothing was written through the tracker, the ledger or the runner

  # Ticks: a timer whose runs fail looks, from outside, like one that works. The
  # logs the dispatch and the Millhand's tick keep are counted, on this host, and
  # the counts go to the other host with every sync.
  Scenario: Three failed dispatch runs after a good one are counted, with the time of the good one
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "vps" then holds 3 failed runs
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "last good 2026-09-21 09:00Z"
    And the TICKS section says of the dispatch: "failed 3 in a row"

  Scenario: Local network faults are counted apart from failed runs
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "vps" then holds 7 local network faults
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "7 local network faults, 0 failed"

  Scenario: Failed runs and local network faults each keep their own count
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "vps" then holds 2 failed runs
    And the dispatch log of "vps" then holds 1 local network fault
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "1 local network fault, 2 failed"

  Scenario: Skipped ticks, the remote unreachable, are counted apart from failed runs
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "vps" then holds 2 skipped runs
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "remote unreachable 2 in a row"
    And the TICKS section does not say of the dispatch: "failed 2 in a row"

  Scenario: A good run resets the count
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "vps" then holds 3 failed runs
    And the dispatch log of "vps" then holds 1 good run
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "failed 0 in a row"
    And the TICKS section says of the dispatch: "last good 2026-09-21 10:00Z"

  Scenario: A log with no good run in it says so
    Given the dispatch log of "vps" holds 4 failed runs
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "no good run logged"
    And the TICKS section says of the dispatch: "failed 4 in a row"

  Scenario: The Millhand's tick is counted the same way, beside the dispatch
    Given the dispatch log of "vps" holds a good run at "2026-09-21T09:00:00Z"
    And the Millhand tick log of "vps" holds a good run at "2026-09-21T09:05:00Z"
    And the Millhand tick log of "vps" then holds 3 failed runs
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the dispatch: "failed 0 in a row"
    And the TICKS section says of the Millhand tick: "last good 2026-09-21 09:05Z"
    And the TICKS section says of the Millhand tick: "failed 3 in a row"

  Scenario: A host with no log prints no TICKS section
    When mw status reads the host
    Then reading status succeeds
    And the report has no TICKS section

  Scenario: A host with only one of the logs prints only that one
    Given the Millhand tick log of "vps" holds a good run at "2026-09-21T09:05:00Z"
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the Millhand tick: "last good 2026-09-21 09:05Z"
    And the TICKS section has nothing on the dispatch

  Scenario: A resume grace still open is shown under the Millhand tick
    Given the Millhand tick log of "vps" holds a good run at "2026-09-18T11:00:00Z"
    And the Millhand tick log of "vps" then holds a line saying "resumed after 45m0s" at "2026-09-18T11:58:00Z"
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the Millhand tick: "resumed 11:58Z (grace until 12:01Z)"

  Scenario: A resume grace that has run out says nothing under the Millhand tick
    Given the Millhand tick log of "vps" holds a good run at "2026-09-18T11:00:00Z"
    And the Millhand tick log of "vps" then holds a line saying "resumed after 45m0s" at "2026-09-18T11:50:00Z"
    When mw status reads the host
    Then reading status succeeds
    And the TICKS section says of the Millhand tick: "last good 2026-09-18 11:50Z"
    And the TICKS section does not say of the Millhand tick: "resumed"

  Scenario: The other host's counts appear under OTHER HOSTS after it syncs
    Given a status story "mw-gq6.12" filed under it, overriding "host" with "laptop"
    And the dispatch log of "laptop" holds a good run at "2026-09-21T09:00:00Z"
    And the dispatch log of "laptop" then holds 12 failed runs
    And the Millhand tick log of "laptop" holds a good run at "2026-09-21T09:05:00Z"
    And the host "laptop" syncs
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows for the dispatch: "last good 2026-09-21 09:00Z"
    And under OTHER HOSTS the host "laptop" shows for the dispatch: "failed 12 in a row"
    And under OTHER HOSTS the host "laptop" shows for the Millhand tick: "failed 0 in a row"

  Scenario: A host that syncs with no log leaves no note of ticks, and shows none
    Given a status story "mw-gq6.12" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows nothing of its ticks

  Scenario: Reading the counts writes nothing
    Given the dispatch log of "vps" holds 3 failed runs
    When mw status reads the host
    Then reading status succeeds
    And nothing was written through the tracker, the ledger or the runner

  Scenario: A hands bead with no step is listed under WAITING ON THE MAYOR with its age
    Given a status story "mw-gq6.40" filed under it
    And the status story "mw-gq6.40" is labelled "hitl"
    And the status story "mw-gq6.40" was filed 90 minutes ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.40" under WAITING ON THE MAYOR aged "1h30m"
    And the landed memory was not written

  Scenario: A hands bead with a question card is not listed under WAITING ON THE MAYOR
    Given a status story "mw-gq6.42" filed under it
    And the status story "mw-gq6.42" is labelled "hitl"
    And the status story "mw-gq6.42" was filed 90 minutes ago
    And the status story "mw-gq6.42" carries a question card
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for needs waiting on the Mayor

  Scenario: With nothing waiting on the Mayor the report has no heading for him
    Given a status story "mw-gq6.41" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for needs waiting on the Mayor

  Scenario: A held bead with a hands step is listed under HELD, WITH A HANDS STEP
    Given a status story "mw-gq6.50" filed under it
    And the status story "mw-gq6.50" is labelled "hitl"
    And the status story "mw-gq6.50" is held
    And the status story "mw-gq6.50" keeps the hands step "speak"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.50" under HELD, WITH A HANDS STEP
    And the report has no heading for needs waiting on the Mayor

  Scenario: An open bead with a hands step is not listed under HELD, WITH A HANDS STEP
    Given a status story "mw-gq6.51" filed under it
    And the status story "mw-gq6.51" is labelled "hitl"
    And the status story "mw-gq6.51" keeps the hands step "speak"
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for held beads with a hands step

  # Room (mw-t0z3fu.1): mw dispatch starts no story on a host with no room, and
  # mw status says how many of the cap are in use and, when the host is held
  # back, why.

  Scenario: The report says how many of the cap are in use, and says nothing of room while the host has it
    Given a status story "mw-gq6.1" filed under it
    And the status story "mw-gq6.1" is claimed with its session running
    And this host runs at most 4 sessions at once
    And this host is at load 3.0 of 16 cores with 8000 MB of memory available
    When mw status reads the host
    Then reading status succeeds
    And the report says "host: 1 of cap 4"
    And the report does not say the host is held back

  Scenario: The report says which window sets the cap while one does (mw-gq6.429)
    Given this host runs at most 4 sessions at once
    And this host's cap is set by "night window to 06:00 America/New_York"
    When mw status reads the host
    Then reading status succeeds
    And the report says "host: 0 of cap 4"
    And the report says "cap 4: night window to 06:00 America/New_York"
    And every line of the report is at most 60 columns wide

  Scenario: The report says why the host is held back when it has no room
    Given a status story "mw-gq6.1" filed under it
    And this host runs at most 4 sessions at once
    And this host is at load 20.0 of 16 cores with 1500 MB of memory available
    When mw status reads the host
    Then reading status succeeds
    And the report says "host: 0 of cap 4"
    And the report says the host is held back: "load 20.0 of 16 cores; 1500 MB free, under 2048 MB"
    And every line of the report is at most 60 columns wide
    And nothing was written through the tracker, the ledger or the runner

  Scenario: A load that cannot be read does not hold the host back in the report
    Given this host runs at most 4 sessions at once
    And this host's load cannot be read for the report
    When mw status reads the host
    Then reading status succeeds
    And the report says "host: 0 of cap 4"
    And the report does not say the host is held back

  # Grist comes first in the caps (mw-t0z3fu.4): mw status says whether a tutor
  # is in use, how the answers compare with their par, and the cap that holds
  # while it is.

  Scenario: The report says grist is active, its median against its par, and the cap it lowers
    Given this host runs at most 4 sessions at once
    And this host's mill answered 45 tutor-turn grists in 10 seconds each long ago, then 3 in 14 seconds each in the last 5 minutes
    When mw status reads the host
    Then reading status succeeds
    And the report says "grist: active (3 in 10 min), median 14 s (par 10 s)"
    And the report says "2 stories at most while it lasts"
    And every line of the report is at most 60 columns wide

  Scenario: The report says why the host is held back when tutor answers are slow
    Given this host runs at most 4 sessions at once
    And this host's mill answered 45 tutor-turn grists in 10 seconds each long ago, then 5 in 30 seconds each in the last 5 minutes
    When mw status reads the host
    Then reading status succeeds
    And the report says "grist: slow (5 in 10 min), median 30 s (par 10 s)"
    And the report says the host is held back: "slow grist: tutor-turn 30 s, par 10 s"

  Scenario: The report says grist is quiet when none ran lately
    Given this host runs at most 4 sessions at once
    And this host's mill answered 3 tutor-turn grists in 10 seconds each long ago, then 0 in 10 seconds each in the last 5 minutes
    When mw status reads the host
    Then reading status succeeds
    And the report says "grist: quiet (none in 10 min)"
    And the report does not say the host is held back

  Scenario: A host with no mill says nothing of grist
    Given this host runs at most 4 sessions at once
    When mw status reads the host
    Then reading status succeeds
    And the report does not say "grist:"

  # Benchmarks (mw-t0z3fu.2): every close-out leaves its gate time, the host's
  # load and the stories running in its result file; mw status compares what a
  # host usually does with what it just did, and shows what each host landed
  # at each count of stories running at once.

  Scenario: The report sets each host's usual gate time against its latest
    Given these close-outs were benchmarked:
      | host   | rig    | kind                  | running | gate | actual | par  | landed |
      | laptop | lampas | lampas/feature/sonnet | 3       | 310  | 3600   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 3       | 310  | 3600   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 3       | 310  | 3600   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 3       | 542  | 3600   | 3600 | yes    |
    When mw status reads the host
    Then reading status succeeds
    And the report says "lampas on laptop: gate usual 5m10s, latest 9m02s"
    And every line of the report is at most 60 columns wide

  Scenario: The report shows what each host landed at each count of stories running
    Given these close-outs were benchmarked:
      | host   | rig    | kind                  | running | gate | actual | par  | landed |
      | laptop | lampas | lampas/feature/sonnet | 4       | 360  | 7200   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 4       | 360  | 7200   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 4       | 360  | 7200   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 2       | 120  | 3600   | 3600 | yes    |
      | laptop | lampas | lampas/feature/sonnet | 2       | 120  | 3600   | 3600 | yes    |
      | desktop | lampas | lampas/feature/sonnet | 1       | 60   | 1800   | 1800 | yes    |
    When mw status reads the host
    Then reading status succeeds
    And the report says "laptop at 2 running: 2.0/h, 2.0 par-h/h, gate 2m00s"
    And the report says "laptop at 4 running: 2.0/h, 2.0 par-h/h, gate 6m00s"
    And the report says "desktop at 1 running: 2.0/h, 1.0 par-h/h, gate 1m00s"
    And every line of the report is at most 60 columns wide

  Scenario: The report flags a kind whose par has been off
    Given these close-outs were benchmarked:
      | host   | rig    | kind            | running | gate | actual | par | landed |
      | laptop | lampas | lampas/bug/opus | 3       | 100  | 200    | 100 | yes    |
      | laptop | lampas | lampas/bug/opus | 3       | 100  | 50     | 100 | yes    |
      | laptop | lampas | lampas/bug/opus | 3       | 100  | 400    | 100 | yes    |
      | laptop | lampas | lampas/bug/opus | 3       | 100  | 200    | 100 | yes    |
      | laptop | lampas | lampas/bug/opus | 3       | 100  | 200    | 100 | yes    |
    When mw status reads the host
    Then reading status succeeds
    And the report says "lampas/bug/opus: par off 100% over 5 landed"
    And the report says "1 of 5 over 2x par or timed out"
    And the report says "FLAG lampas/bug/opus: try a wider window"
    And every line of the report is at most 60 columns wide

  Scenario: The report does not flag a kind with fewer landings than par_min, but still says how far off it is
    Given these close-outs were benchmarked:
      | host    | rig           | kind                          | running | gate | actual | par | landed |
      | desktop | trade-tracker | trade-tracker/feature/sonnet | 1       | 100  | 156    | 100 | yes    |
    When mw status reads the host
    Then reading status succeeds
    And the report says "trade-tracker/feature/sonnet: par off 56% over 1 landed"
    And the report does not say "FLAG"

  Scenario: A story labelled hitl and hitl:review is listed as waiting for the Governor, saying review
    Given a status story "mw-gq6.60" filed under it
    And the status story "mw-gq6.60" is labelled "hitl" and "hitl:review"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.60" as waiting for the Governor
    And the report does not list "mw-gq6.60" as ready
    And the report says "mw-gq6.60" waits as "ready, review"
    And the report has no warning

  Scenario: A story labelled hitl and two hitl kinds is a warning
    Given a status story "mw-gq6.61" filed under it
    And the status story "mw-gq6.61" is labelled "hitl" and "hitl:review" and "hitl:decision"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.61" as waiting for the Governor
    And the report warns that "mw-gq6.61" has two hitl kinds, "review" and "decision"

  Scenario: A bead waiting on others is listed to recheck its Done-when, with who and since when
    Given a status bead "mw-6ww.70" titled "Get the sign-off" filed under no epic
    And the status story "mw-6ww.70" is labelled "waiting:others" and "asked-of:sam"
    And the status story "mw-6ww.70" has waited on others since "2026-10-05T14:30:00Z"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-6ww.70" to recheck its Done-when, waiting on "sam" since "5 Oct 14:30 UTC"
    And every line of the report is at most 60 columns wide

  Scenario: Without a bead waiting on others the report has no heading for them
    Given a status bead "mw-6ww.71" titled "Get the sign-off" filed under no epic
    And the status story "mw-6ww.71" is labelled "asked-of:sam"
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for beads waiting on others

  # mw-2zh33g.1: a story refused lately and still without a named cause.

  Scenario: A story refused within 7 days with no cause is listed under CAUSES MISSING
    Given a status story "mw-gq6.70" titled "Fix the gate" filed under it
    And the status story "mw-gq6.70" was refused 2 days ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.70" under CAUSES MISSING

  Scenario: A refused story whose cause is named, or refused earlier, is left out of CAUSES MISSING
    Given a status story "mw-gq6.71" titled "Named already" filed under it
    And the status story "mw-gq6.71" was refused 2 days ago
    And the status story "mw-gq6.71" has its cause recorded as "code"
    And a status story "mw-gq6.72" titled "Refused long ago" filed under it
    And the status story "mw-gq6.72" was refused 9 days ago
    And a status story "mw-gq6.73" titled "Never refused" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for causes missing

  Scenario: Only the refused story without a cause is listed when others are named
    Given a status story "mw-gq6.74" titled "Named already" filed under it
    And the status story "mw-gq6.74" was refused 1 days ago
    And the status story "mw-gq6.74" has its cause recorded as "test"
    And a status story "mw-gq6.75" titled "Still unnamed" filed under it
    And the status story "mw-gq6.75" was refused 3 days ago
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.75" under CAUSES MISSING
    And the report does not list "mw-gq6.74" under CAUSES MISSING

  Scenario: An app in [grist-apps] whose checkout has no VISION.md is named
    Given the host's [grist-apps] checks out the app "cairn" and the app "pilgrim"
    And the checkout of the app "pilgrim" has a VISION.md
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "VISION: cairn has no VISION.md"
    And the report has no line "VISION: pilgrim has no VISION.md"

  Scenario: Every app with a VISION.md leaves no VISION line
    Given the host's [grist-apps] checks out the app "cairn" and the app "pilgrim"
    And the checkout of the app "cairn" has a VISION.md
    And the checkout of the app "pilgrim" has a VISION.md
    When mw status reads the host
    Then reading status succeeds
    And the report has no line starting "VISION:"

  # Ideas only (postern's docs/protocol.md section 25): while the Governor has put
  # his key in the mode, the Mayor is told at boot, in one line, that he is away on
  # purpose and how many of his ideas mw is keeping.

  Scenario: While the Governor is in Ideas only, the report opens with one line saying so
    Given the status report knows the Governor's Ideas only is on until "2026-10-12T21:00:00Z" with 2 ideas held
    When mw status reads the host
    Then reading status succeeds
    And the status report has a line "Governor: Ideas only until 21:00 UTC (2 ideas held)" right under its title
    And the status report has the line "  to-dos still work" under it

  Scenario: Out of Ideas only the report has no such line
    Given the status report knows the Governor's Ideas only is off
    When mw status reads the host
    Then reading status succeeds
    And the status report has no line beginning "Governor: Ideas only"

  # mw-8w7xb7.3: the last whole-suite result of each rig that runs one nightly.

  Scenario: SUITE shows a rig's last result with the beads that hold its failing files
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the epic "mw-suite" holds the held bug "mw-a" for "src/a.test.ts" of "lampas"
    And the epic "mw-suite" holds the held bug "mw-b" for "src/b.test.ts" of "lampas"
    And the last suite of "lampas" was kept today at "9f3c2ab1d", failing "src/a.test.ts" and "src/b.test.ts"
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "SUITE"
    And the report has the line "  lampas 2026-09-18 9f3c2ab1d: 2 failing: mw-a, mw-b"

  # mw-gq6.610: a failure whose bug has landed is not an unfiled one.

  Scenario: SUITE names the bug that fixed a failing file, closed since the run began
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the epic "mw-suite" holds the bug "mw-c" for "src/c.test.ts" of "lampas", closed today at "11:20"
    And the last suite of "lampas" was kept today at "9f3c2ab1d", failing "src/c.test.ts"
    When mw status reads the host
    Then the SUITE section reads "lampas 2026-09-18 9f3c2ab1d: 1 failing: fixed by mw-c (landed 11:20Z), until the next run"

  Scenario: SUITE says unfiled for a failing file no bug names
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the last suite of "lampas" was kept today at "9f3c2ab1d", failing "src/d.test.ts"
    When mw status reads the host
    Then the SUITE section reads "lampas 2026-09-18 9f3c2ab1d: 1 failing: unfiled"

  Scenario: A rig whose last suite passed says all pass
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the last suite of "lampas" was kept today at "9f3c2ab1d", all passed
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "  lampas 2026-09-18 9f3c2ab1d: all pass"

  Scenario: A last suite two days old says how long it has not run
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the last suite of "lampas" was kept 2 days ago at "9f3c2ab1d", all passed
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "  lampas 2026-09-16 9f3c2ab1d: all pass · not run for 2 days"

  Scenario: A last suite from yesterday is not yet late
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    And the last suite of "lampas" was kept 1 day ago at "9f3c2ab1d", all passed
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "  lampas 2026-09-17 9f3c2ab1d: all pass"

  Scenario: A rig that has never run its nightly suite says so
    Given the status rig "lampas" runs the nightly suite, its bugs filed under the epic "mw-suite"
    When mw status reads the host
    Then reading status succeeds
    And the report has the line "  lampas: never run"

  Scenario: A rig that does not run the nightly suite is not under SUITE
    Given the status rig "plain" does not run the nightly suite
    When mw status reads the host
    Then reading status succeeds
    And the report has no heading for the suite

  # mw-gq6.604: a story queued for batch landing has no session by design, so
  # its run-out lease is no sign that it is stuck.
  Scenario: A claimed story queued for batch landing is not called stranded, and says since when
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.60" filed under it
    And the status story "mw-gq6.60" is claimed with its session running
    And the claim on "mw-gq6.60" was last renewed 3 hours ago
    And the status story "mw-gq6.60" was queued for batch landing 40 minutes ago
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.60" is queued for batch landing since 40 minutes ago
    And the report does not call any story stranded
    And the report does not say any story is queued too long
    And every line of the report is at most 60 columns wide

  Scenario: A queued story says so when its rig's batch is running
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.61" filed under it
    And the status story "mw-gq6.61" is claimed with its session running
    And the claim on "mw-gq6.61" was last renewed 3 hours ago
    And the status story "mw-gq6.61" was queued for batch landing 150 minutes ago
    And a batch-land job for the rig "millwright" began running 20 minutes ago
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.61" is queued for batch landing since 150 minutes ago
    And the report says "mw-gq6.61" is in the batch running since 20 minutes ago
    And the report does not call any story stranded
    And the report does not say any story is queued too long
    And every line of the report is at most 60 columns wide

  # mw-gq6.652: a batch outlives the newest 200 events of a busy log, and is
  # still running.
  Scenario: A batch that began running before the newest 500 events is still the batch running
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.65" filed under it
    And the status story "mw-gq6.65" is claimed with its session running
    And the claim on "mw-gq6.65" was last renewed 3 hours ago
    And the status story "mw-gq6.65" was queued for batch landing 150 minutes ago
    And a batch-land job for the rig "millwright" began running 20 minutes ago
    And 500 other events were written since
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.65" is in the batch running since 20 minutes ago
    And the report does not say any story is queued too long

  # mw-gq6.727: a follower restarted in the middle of a batch runs the pass again
  # with a reason that names no rig, and the batch is still the one running.
  Scenario: A batch the restarted follower runs again is still the batch running
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.68" filed under it
    And the status story "mw-gq6.68" is claimed with its session running
    And the claim on "mw-gq6.68" was last renewed 3 hours ago
    And the status story "mw-gq6.68" was queued for batch landing 150 minutes ago
    And a batch-land job for the rig "millwright" began running 20 minutes ago
    And the follower restarted and ran the batch-land job again
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.68" is queued for batch landing since 150 minutes ago
    And the report does not say any story is queued too long

  Scenario: A batch that has ended is not the batch running, however many events lie since
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.66" filed under it
    And the status story "mw-gq6.66" is claimed with its session running
    And the claim on "mw-gq6.66" was last renewed 3 hours ago
    And the status story "mw-gq6.66" was queued for batch landing 150 minutes ago
    And a batch-land job for the rig "millwright" began running 20 minutes ago
    And the batch-land job ended
    And 500 other events were written since
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.66" is queued too long

  # mw-gq6.655: a job's newest state is read from what the log keeps of each job,
  # not by scanning the log, so a batch that began 100000 events back is seen
  # at once.
  Scenario: A batch that began 100000 events back is the batch running, and the log is not scanned for it
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.67" filed under it
    And the status story "mw-gq6.67" is claimed with its session running
    And the claim on "mw-gq6.67" was last renewed 3 hours ago
    And the status story "mw-gq6.67" was queued for batch landing 150 minutes ago
    And a batch-land job for the rig "millwright" began running 20 minutes ago
    And 100000 other events were written since
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.67" is in the batch running since 20 minutes ago
    And reading status scanned fewer than 1000 events of the log

  Scenario: A story queued past twice its rig's batch_every with no batch running is the alarm
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.62" filed under it
    And the status story "mw-gq6.62" is claimed with its session running
    And the claim on "mw-gq6.62" was last renewed 3 hours ago
    And the status story "mw-gq6.62" was queued for batch landing 61 minutes ago
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.62" is queued too long
    And the report says "mw-gq6.62" is queued for batch landing since 61 minutes ago
    And the report does not call any story stranded
    And every line of the report is at most 60 columns wide

  Scenario: A story queued on another host that is awake is not called stranded either
    Given the rig "millwright" lands in batches every 30 minutes
    And a status story "mw-gq6.63" filed under it, overriding "host" with "laptop"
    And the status story "mw-gq6.63" is claimed with its session running
    And the claim on "mw-gq6.63" was last renewed 3 hours ago
    And the status story "mw-gq6.63" was queued for batch landing 40 minutes ago
    And the host "laptop" last synced 1 hour ago
    When mw status reads the host
    Then reading status succeeds
    And the report says "mw-gq6.63" is queued for batch landing since 40 minutes ago
    And the report does not call any story stranded

  Scenario: A claimed story that is not queued and whose lease ran out is still stranded
    Given a status story "mw-gq6.64" filed under it
    And the status story "mw-gq6.64" is claimed with its session running
    And the claim on "mw-gq6.64" was last renewed 3 hours ago
    When mw status reads the host
    Then reading status succeeds
    And the report calls "mw-gq6.64" stranded
    And the report does not say any story is queued too long

  # mw-gq6.702: dispatch passes every story of a rig that installs dependencies
  # while the network is metered, and nothing said so. A story pinned to this
  # host is named under DAMPERS with the cure.

  Scenario: A story pinned to this host on a rig that installs dependencies is named while the network is metered
    Given a status story "mw-gq6.90" filed under it
    And a status story "mw-gq6.91" filed under it, overriding "host" with "auto"
    And the network is metered
    And the status config names the rig "millwright" as installing dependencies
    When mw status reads the host
    Then reading status succeeds
    And the DAMPERS section names "mw-gq6.90" as pinned to "vps", metered, its rig installs dependencies, saying: set host=auto
    And the DAMPERS section does not name "mw-gq6.91"

  Scenario: Nothing is said when the network is not metered
    Given a status story "mw-gq6.92" filed under it
    And the network is not metered
    And the status config names the rig "millwright" as installing dependencies
    When mw status reads the host
    Then reading status succeeds
    And the report has no DAMPERS section

  Scenario: Nothing is said of a story pinned to another host
    Given a status story "mw-gq6.93" filed under it, overriding "host" with "laptop"
    And the network is metered
    And the status config names the rig "millwright" as installing dependencies
    When mw status reads the host
    Then reading status succeeds
    And the report has no DAMPERS section

  Scenario: Nothing is said of a rig that does not install dependencies
    Given a status story "mw-gq6.94" filed under it
    And the network is metered
    When mw status reads the host
    Then reading status succeeds
    And the report has no DAMPERS section

  # mw-gq6.703: a ready story every host passes for a reason of the host's own
  # (the rig is not checked out there, the home is metered and the rig installs
  # dependencies) sat unclaimed with nothing in mw status saying why. Each
  # host's dispatch keeps a note of what it passed that way; status reads them all.

  Scenario: A ready story every host has passed says so on its READY line, naming each host's reason
    Given a status story "mw-gq6.100" filed under it, overriding "host" with "auto"
    And the host "laptop" passed "mw-gq6.100" 5 minutes ago at the gate "metered-network" because "the rig millwright installs dependencies and the network is metered"
    And the host "desktop" passed "mw-gq6.100" 5 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on desktop: add it under [rigs] in the config file"
    And the host "vps" passed "mw-gq6.100" 5 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on vps: add it under [rigs] in the config file"
    When mw status reads the host
    Then reading status succeeds
    And the READY line of "mw-gq6.100" says every host has passed it, naming "laptop: the rig millwright installs dependencies and the network is metered" and "desktop: the rig millwright is not checked out on desktop"
    And the report has no DAMPERS section

  Scenario: A ready story one host can take is not said to be passed by every host
    Given a status story "mw-gq6.101" filed under it, overriding "host" with "auto"
    And the host "laptop" passed "mw-gq6.101" 40 minutes ago at the gate "metered-network" because "the rig millwright installs dependencies and the network is metered"
    And the host "desktop" passed "mw-gq6.101" 40 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on desktop: add it under [rigs] in the config file"
    When mw status reads the host
    Then reading status succeeds
    And the READY line of "mw-gq6.101" does not say every host has passed it
    And the report has no DAMPERS section

  Scenario: A story every host has passed for 21 minutes is named under DAMPERS with the fix
    Given a status story "mw-gq6.102" filed under it, overriding "host" with "auto"
    And the host "laptop" passed "mw-gq6.102" 21 minutes ago at the gate "metered-network" because "the rig millwright installs dependencies and the network is metered"
    And the host "desktop" passed "mw-gq6.102" 21 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on desktop: add it under [rigs] in the config file"
    And the host "vps" passed "mw-gq6.102" 21 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on vps: add it under [rigs] in the config file"
    When mw status reads the host
    Then reading status succeeds
    And the DAMPERS section says "no host can take it" of "mw-gq6.102", with the fixes "clone millwright on desktop and mw config set rigs.millwright" and "clone millwright on vps and mw config set rigs.millwright"

  Scenario: A story every host has passed for 19 minutes is not yet named under DAMPERS
    Given a status story "mw-gq6.103" filed under it, overriding "host" with "auto"
    And the host "laptop" passed "mw-gq6.103" 19 minutes ago at the gate "metered-network" because "the rig millwright installs dependencies and the network is metered"
    And the host "desktop" passed "mw-gq6.103" 19 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on desktop: add it under [rigs] in the config file"
    And the host "vps" passed "mw-gq6.103" 19 minutes ago at the gate "rig-not-checked-out" because "the rig millwright is not checked out on vps: add it under [rigs] in the config file"
    When mw status reads the host
    Then reading status succeeds
    And the READY line of "mw-gq6.103" says every host has passed it, naming "laptop: the rig millwright installs dependencies" and "desktop: the rig millwright is not checked out on desktop"
    And the report has no DAMPERS section

  Scenario: A Boost whose doctor left boost-config faulty shows the check and its reason under its host
    Given a status story "mw-gq6.110" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    And the doctor of "laptop" runs now and finds "boost-config" faulty: "tests.millwright differs"
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows the doctor line: "doctor boost-config faulty: tests.millwright differs"
    And the DAMPERS section says the doctor of "laptop" finds "boost-config" faulty: "tests.millwright differs"

  Scenario: Every check the doctor of a Boost leaves faulty reaches the home by the same note
    Given a status story "mw-gq6.111" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    And the doctor of "laptop" runs now and finds "boost-config" faulty: "tests.millwright differs" and "rigtests" faulty: "make test fails"
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows the doctor line: "doctor boost-config faulty: tests.millwright differs"
    And under OTHER HOSTS the host "laptop" shows the doctor line: "doctor rigtests faulty: make test fails"

  Scenario: A Boost whose doctor found everything ok shows no doctor line and no doctor alarm
    Given a status story "mw-gq6.112" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    And the doctor of "laptop" runs now and finds everything ok
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows no doctor line
    And the report has no DAMPERS section

  Scenario: A Boost whose doctor report is a day old is said to be stale, not ok
    Given a status story "mw-gq6.113" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    And the doctor of "laptop" ran 30 hours ago and found everything ok
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows the doctor line: "doctor report is 1d old: stale"
    And the report has no DAMPERS section

  Scenario: A stale faulty report is not trusted as a fresh alarm but names what it last said
    Given a status story "mw-gq6.114" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    And the doctor of "laptop" ran 30 hours ago and found "boost-config" faulty: "tests.millwright differs"
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows the doctor line: "doctor report is 1d old: stale (last said: boost-config faulty)"
    And the report has no DAMPERS section

  Scenario: A Boost that has never reported a doctor run shows no doctor line
    Given a status story "mw-gq6.115" filed under it, overriding "host" with "laptop"
    And the host "laptop" syncs
    When mw status reads the host
    Then reading status succeeds
    And under OTHER HOSTS the host "laptop" shows no doctor line
