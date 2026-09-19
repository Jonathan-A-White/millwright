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
