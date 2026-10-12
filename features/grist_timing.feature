Feature: Every grind line records where an answer's seconds went, and mw grist timing sums them up
  A grist is sent on the backend, noticed by the follower's watch (or by the
  dispatch tick's pass), picked up by the pass, waits for a grind slot, has the
  harness start up and the model run, and its answer is delivered. The mill's
  record (grinds.jsonl) keeps each part on the grind's line, in seconds with
  one decimal: wake_s, pickup_s, slot_s, startup_s, model_s, deliver_s and
  total_s beside the seconds field, which means what it always did. A time the
  mill does not know is left out, never 0. mw grist timing reads the record and
  prints, for each app and kind, the count and the median and p90 of each
  phase. It reads no tokens and calls nothing.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the grind answers with a sweep result

  Scenario: An answer's seconds are recorded phase by phase
    Given the mill's clock reads 3.2 seconds after the grist was sent
    And the follower noticed the grist 3.0 seconds after it was sent
    And fetching a photo takes 0.1 seconds
    And the harness takes 4.7 seconds, 3.9 of them its own run
    And delivering an answer takes 0.4 seconds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the record holds one line for the grist
    And that line says wake 3.0, pickup 0.2, slot 0.0, deliver 0.4 and total 8.4 seconds
    And that line says startup 0.8 and model 3.9 seconds, which add up to 4.7
    And that line says it took 5.2 seconds

  Scenario: A pass the dispatch tick runs counts the wake from its own start
    Given the mill's clock reads 3.2 seconds after the grist was sent
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the record holds one line for the grist
    And that line says wake 3.2 and pickup 0.0 seconds

  Scenario: With both slots busy, slot_s is the wait for one
    Given the mill grinds up to 2 grists at once
    And the grinds are held until 2 of them are running at once
    And delivering an answer takes 10 seconds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the first two grists waited 0.0 seconds for a slot
    And the third grist waited 10.0 seconds for a slot

  Scenario: A refused grist has no harness, no slot and so no such phases
    Given the mill's clock reads 3.2 seconds after the grist was sent
    And a phone whose licence opens "lampas"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the record holds one line for the grist
    And that line has no slot, startup or model phase

  Scenario: An answer the backend would not take has no deliver or total
    Given the mill's clock reads 3.2 seconds after the grist was sent
    And the backend will not take a record
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the record holds one line for the grist
    And that line has no deliver or total phase

  Scenario: An old line without phases is read, and counted only in seconds
    Given the record holds an old line of "lampas" "verse-read" that took 7.5 seconds
    When mw grist timing is run for the app "lampas" and the kind "verse-read"
    Then mw grist timing succeeded
    And it printed for "lampas verse-read" 1 grind and these phases:
      | phase   | count | median | p90 |
      | seconds | 1     | 7.5    | 7.5 |

  Scenario: mw grist timing prints the count, median and p90 of each phase
    Given the record holds these grinds of "lampas" "verse-read":
      | seconds | wake_s | pickup_s | slot_s | startup_s | model_s | deliver_s | total_s |
      | 6.0     | 1.0    | 0.1      | 0.0    | 0.5       | 4.0     | 0.4       | 7.0     |
      | 7.0     | 2.0    | 0.2      | 0.0    | 0.6       | 5.0     | 0.4       | 9.2     |
      | 8.0     | 3.0    | 0.3      | 1.0    | 0.7       | 6.0     | 0.4       | 11.4    |
      | 9.0     | 4.0    | 0.4      | 0.0    | 0.8       | 7.0     | 0.4       | 13.6    |
      | 10.0    | 5.0    | 0.5      | 0.0    | 0.9       | 8.0     | 0.4       | 15.8    |
    And the record holds a grind of "lampas" "other-kind" that took 99.0 seconds
    And the record holds a grind of "cairn" "sweep" that took 98.0 seconds
    When mw grist timing is run for the app "lampas" and the kind "verse-read"
    Then mw grist timing succeeded
    And it printed for "lampas verse-read" 5 grinds and these phases:
      | phase   | count | median | p90  |
      | seconds | 5     | 8.0    | 10.0 |
      | wake    | 5     | 3.0    | 5.0  |
      | pickup  | 5     | 0.3    | 0.5  |
      | slot    | 5     | 0.0    | 1.0  |
      | startup | 5     | 0.7    | 0.9  |
      | model   | 5     | 6.0    | 8.0  |
      | deliver | 5     | 0.4    | 0.4  |
      | total   | 5     | 11.4   | 15.8 |
    And it printed nothing for "cairn sweep" or "lampas other-kind"

  Scenario: --since leaves out the lines before it
    Given the record holds an old line of "lampas" "verse-read" that took 7.5 seconds
    And the record holds a grind of "lampas" "verse-read" that took 2.5 seconds, a day and a half later
    When mw grist timing is run for the app "lampas" with --since 24h
    Then it printed for "lampas verse-read" 1 grind and these phases:
      | phase   | count | median | p90 |
      | seconds | 1     | 2.5    | 2.5 |
