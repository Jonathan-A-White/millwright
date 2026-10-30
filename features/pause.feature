Feature: mw pause and mw resume
  The Governor says pause, and the factory stops where it stands: every host
  holds new claims (the usage-limit hold note, never cap 0 and never a masked
  unit), every Builder in hand checks its work in to its branch and says
  PAUSED, cloud boxes come down, and the set of what was paused is kept.
  mw resume reverses it from that set.

  Background:
    Given a factory of the hosts "laptop" and "desktop" and the cloud box "cloud1", at 12:00 UTC on 22 October 2026
    And the host "laptop" has the session cap 5
    And the host "desktop" has the session cap 4
    And the host "cloud1" has the session cap 3
    And cloud.max_boxes is 4

  Scenario: Every host is held by the usage-limit hold note and no cap is changed
    When the Governor runs mw pause with the reason "so we're at 97% usage"
    Then the hold note of "laptop" is until "2026-10-29T12:00:00Z" and says "usage-limit"
    And the hold note of "desktop" is until "2026-10-29T12:00:00Z" and says "usage-limit"
    And the hold note of "cloud1" is until "2026-10-29T12:00:00Z" and says "usage-limit"
    And the paused set records the cap 5 for "laptop", 4 for "desktop" and 3 for "cloud1"
    And the output says "so we're at 97% usage"

  Scenario: A Builder that answers the PAUSE line pushes its own branch and says PAUSED
    Given the Builder of "mw-aaa.1" is running on "laptop" with work committed, and answers the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the session of "mw-aaa.1" on "laptop" was told "PAUSE:"
    And the session of "mw-aaa.1" on "laptop" is still running
    And the branch of "mw-aaa.1" is pushed
    And mw did not push the branch of "mw-aaa.1" itself
    And the story "mw-aaa.1" has a PAUSED comment written by "builder"

  Scenario: A Builder that does not answer has its work committed and pushed by mw after the wait
    Given the Builder of "mw-bbb.2" is running on "desktop" with uncommitted work, and does not answer the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the session of "mw-bbb.2" on "desktop" was told "PAUSE:"
    And mw committed and pushed the branch of "mw-bbb.2" itself
    And the branch of "mw-bbb.2" is pushed
    And the story "mw-bbb.2" has a PAUSED comment written by "mw"
    And the session of "mw-bbb.2" on "desktop" is still running

  Scenario: A story whose session is already gone is saved too
    Given the story "mw-ccc.3" is claimed on "laptop" but its session is gone, with work committed and not pushed
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the branch of "mw-ccc.3" is pushed
    And the paused set lists "mw-ccc.3" on "laptop" with its session not alive

  Scenario: A cloud box's sessions end and the box comes down once its branches are pushed
    Given the Builder of "mw-ddd.4" is running on "cloud1" with work committed, and answers the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the session of "mw-ddd.4" on "cloud1" was ended
    And the box "cloud1" was brought down
    And cloud.max_boxes is now 0

  Scenario: A cloud box is kept up while a branch on it could not be pushed
    Given the story "mw-eee.5" is claimed on "cloud1" but its session is gone, with no worktree
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the box "cloud1" was not brought down
    And the output says "box cloud1 kept up"

  Scenario: The paused set is kept in the vault, on a bead and in a mail to the Mayor, and printed
    Given the Builder of "mw-aaa.1" is running on "laptop" with work committed, and answers the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow" and the bead "mw-rec.1"
    Then the vault holds plans/pause-2026-10-22/paused.json with the story "mw-aaa.1" on "laptop", branch "mw/mw-aaa.1", pushed, with a PAUSED comment
    And the bead "mw-rec.1" carries the paused set
    And a mail went to the Mayor titled "The factory is paused"
    And the output says "story mw-aaa.1 on laptop"

  Scenario: A second pause changes nothing and says so
    Given the Builder of "mw-aaa.1" is running on "laptop" with work committed, and answers the PAUSE line
    And the Governor ran mw pause with the reason "reset tomorrow"
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then nothing more was written
    And the output says "already paused"

  Scenario: A host that cannot be reached is named, and a second pause reaches it
    Given the host "desktop" cannot be reached
    And the Builder of "mw-bbb.2" is running on "desktop" with work committed, and answers the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the output says "NOT REACHED: desktop"
    When the host "desktop" can be reached again
    And the Governor runs mw pause with the reason "reset tomorrow"
    Then the hold note of "desktop" is until "2026-10-29T12:00:00Z" and says "usage-limit"
    And the branch of "mw-bbb.2" is pushed
    And the output does not say "NOT REACHED"

  Scenario: The seats' windows are never touched
    Given the Builder of "mw-seats" is running on "laptop" with work committed, and answers the PAUSE line
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then the session of "mw-seats" on "laptop" was told nothing
    And the session of "mw-seats" on "laptop" is still running
    And the output says "the seats' own"
    When the Governor runs mw resume
    Then the session of "mw-seats" on "laptop" was told nothing

  Scenario: Until a time sets the hold note's end and schedules the resume as a one-shot timer
    When the Governor runs mw pause with the reason "reset tomorrow" until "2026-10-23T09:30:00Z"
    Then the hold note of "laptop" is until "2026-10-23T09:30:00Z" and says "usage-limit"
    And the hold note of "desktop" is until "2026-10-23T09:30:00Z" and says "usage-limit"
    And a resume timer "mw-resume-2026-10-22" is scheduled for "2026-10-23T09:30:00Z"
    And the output says "mw-resume-2026-10-22"

  Scenario: A pause with no time and no usage report is open-ended and sets no timer
    When the Governor runs mw pause with the reason "reset tomorrow"
    Then no resume timer is scheduled

  Scenario: Resume clears the hold notes, tells idle Builders to carry on and retries the gone ones
    Given the Builder of "mw-aaa.1" is running on "laptop" with work committed, and answers the PAUSE line
    And the Builder of "mw-ddd.4" is running on "cloud1" with work committed, and answers the PAUSE line
    And the story "mw-ccc.3" is claimed on "laptop" but its session is gone, with work committed and not pushed
    And the Governor ran mw pause with the reason "reset tomorrow" until "2026-10-23T09:30:00Z"
    When the Governor runs mw resume
    Then the hold note of "laptop" is clear
    And the hold note of "desktop" is clear
    And the session of "mw-aaa.1" on "laptop" was told "RESUME:"
    And mw retry was run for "mw-ccc.3"
    And mw retry was run for "mw-ddd.4"
    And mw retry was not run for "mw-aaa.1"
    And cloud.max_boxes is now 4
    And the resume timer "mw-resume-2026-10-22" is cancelled
    And a mail went to the Mayor titled "The factory is resumed"
    And the paused set under plans/pause-2026-10-22 says it was resumed

  Scenario: A second resume changes nothing and says so
    Given the Governor ran mw pause with the reason "reset tomorrow"
    And the Governor ran mw resume
    When the Governor runs mw resume
    Then nothing more was written
    And the output says "not paused"

  Scenario: Resume with no pause in force changes nothing
    When the Governor runs mw resume
    Then the output says "not paused"
    And cloud.max_boxes is now 4

  Scenario Outline: The reserve R(t) is 10% with a week left and 0% with an hour left
    Then with <left> hours left the reserve is <reserve> percent

    Examples:
      | left  | reserve |
      | 168   | 10.00   |
      | 100   | 5.93    |
      | 84.5  | 5.00    |
      | 25    | 1.44    |
      | 1     | 0.00    |
      | 0.5   | 0.00    |

  Scenario: A reported usage at or over 100% less the reserve pauses the factory by itself
    Given the Builder of "mw-aaa.1" is running on "laptop" with work committed, and answers the PAUSE line
    When the Governor reports a usage of 99 percent with the week resetting at "2026-10-23T13:00:00Z"
    Then the output says "has reached it"
    And the hold note of "laptop" is until "2026-10-29T12:00:00Z" and says "usage-limit"
    And the branch of "mw-aaa.1" is pushed

  Scenario: A reported usage under the reserve does not pause
    When the Governor reports a usage of 97 percent with the week resetting at "2026-10-23T13:00:00Z"
    Then the output says "is under it"
    And the hold note of "laptop" is clear

  Scenario: A floor keeps the reserve up for the tutor until its time, then all out
    Given the Governor set a usage floor of 3 percent until "2026-10-22T12:20:00Z"
    When the Governor reports a usage of 98 percent with the week resetting at "2026-10-22T12:30:00Z"
    Then the output says "has reached it"
    And the output says "floor 3.00%"
    And the hold note of "laptop" is until "2026-10-29T12:00:00Z" and says "usage-limit"

  Scenario: After the floor's time the reserve is R(t) alone
    Given the Governor set a usage floor of 3 percent until "2026-10-22T12:20:00Z"
    And the clock moves on 30 minutes
    When the Governor reports a usage of 98 percent with the week resetting at "2026-10-22T13:00:00Z"
    Then the output says "is under it"
    And the output does not say "floor"
    And the hold note of "laptop" is clear

  Scenario: Floor off clears the floor
    Given the Governor set a usage floor of 3 percent until "2026-10-22T12:20:00Z"
    And the Governor cleared the usage floor
    When the Governor reports a usage of 98 percent with the week resetting at "2026-10-22T12:30:00Z"
    Then the output says "is under it"
    And the hold note of "laptop" is clear

  Scenario: Usage show prints the floor and its end beside R(t)
    Given the Governor set a usage floor of 3 percent until "2026-10-22T12:20:00Z"
    And the Governor reported 91 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-22T12:30:00Z"
    When the Governor runs mw usage show
    Then the output says "floor 3.0% until 2026-10-22T12:20:00Z"
    And the output says "reserve R(t) 0.00%"

  Scenario: Usage show says STALE when the newest reading is old
    Given the Governor reported 95 percent at "2026-10-22T09:00:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    When the Governor runs mw usage show
    Then the output says "STALE: the newest reading is 3h old"

  Scenario: Usage show says nothing of staleness when the newest reading is fresh
    Given the Governor reported 95 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    When the Governor runs mw usage show
    Then the output does not say "STALE"

  Scenario: A reading taken before a reset that has passed is stale and never trips
    Given the Governor reported 100 percent at "2026-10-22T11:00:00Z" with the week resetting at "2026-10-22T11:30:00Z"
    When the Governor runs mw usage show
    Then the output says "STALE: taken before the week reset at 2026-10-22T11:30:00Z"
    And the output does not say "has reached it"

  Scenario: Status shows the STALE line beside the usage when the newest reading is old
    Given the Governor reported 95 percent at "2026-10-22T09:00:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    When the Governor runs mw status
    Then the output says "USAGE 95.0%"
    And the output says "STALE: the newest reading is 3h old"

  Scenario: Status shows no STALE line when the newest reading is fresh
    Given the Governor reported 95 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    When the Governor runs mw status
    Then the output says "USAGE 95.0%"
    And the output does not say "STALE"

  Scenario: Status says STALE for a reading taken before a reset that has passed
    Given the Governor reported 100 percent at "2026-10-22T11:00:00Z" with the week resetting at "2026-10-22T11:30:00Z"
    When the Governor runs mw status
    Then the output says "STALE: taken before the week reset at 2026-10-22T11:30:00Z"

  Scenario: A floor that is not in the future is refused
    When the Governor tries to set a usage floor of 3 percent until "2026-10-22T11:00:00Z"
    Then mw refuses, saying "not in the future"

  Scenario: The resume time is the moment the quota left runs out 15 minutes before the reset
    Given the Governor reported 91 percent at "2026-10-21T12:00:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    And the Governor reported 97 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    And the ledgers show 48000000 tokens on each of 2026-10-20, 2026-10-21 and 2026-10-22
    When the Governor runs mw pause as a dry run
    Then the output says "burn 0.125%/h"
    And the output says "3.0% left"
    And the output says "runs out in 24h0m0s"
    And the output says "= 2026-10-22T12:45:00Z"
    And the hold note of "laptop" is clear

  Scenario: The computed time is the hold note's end and the timer's moment
    Given the Governor reported 91 percent at "2026-10-21T12:00:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    And the Governor reported 97 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    And the ledgers show 48000000 tokens on each of 2026-10-20, 2026-10-21 and 2026-10-22
    When the Governor runs mw pause with the reason "so we're at 97% usage"
    Then the hold note of "laptop" is until "2026-10-22T12:45:00Z" and says "usage-limit"
    And a resume timer "mw-resume-2026-10-22" is scheduled for "2026-10-22T12:45:00Z"

  Scenario: A rate given by hand stands in for the measured one
    Given the Governor reported 97 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    And the burn at full throttle is given as 0.5 percent an hour
    When the Governor runs mw pause as a dry run
    Then the output says "= 2026-10-23T06:45:00Z"

  Scenario: With one reading and no rate the resume time cannot be measured and the pause is open-ended
    Given the Governor reported 97 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-23T13:00:00Z"
    When the Governor runs mw pause with the reason "so we're at 97% usage"
    Then the output says "the pause is open-ended"
    And no resume timer is scheduled

  Scenario: A computed resume time that is already now pauses nothing
    Given the Governor reported 99 percent at "2026-10-22T11:40:00Z" with the week resetting at "2026-10-22T14:00:00Z"
    And the burn at full throttle is given as 0.1 percent an hour
    When the Governor tries mw pause with the reason "so we're at 99% usage"
    Then mw refuses, saying "already now"
    And the hold note of "laptop" is clear

  Scenario: A pause records and stops the night passes
    Given the follower's sleep pass and To do rank are both due on "laptop"
    When the Governor runs mw pause with the reason "so we're at 97% usage"
    Then the paused set records the night passes "sleep" and "todo-rank" as off
    And the output says "night passes off: sleep, todo-rank"
    And the sleep pass is not due on "laptop"
    And the To do rank is not due on "laptop"

  Scenario: A resume turns the night passes back on
    Given the follower's sleep pass and To do rank are both due on "laptop"
    And the Governor ran mw pause with the reason "so we're at 97% usage"
    When the Governor runs mw resume
    Then the output says "night passes back on: sleep, todo-rank"
    And the sleep pass is due on "laptop"
    And the To do rank is due on "laptop"
