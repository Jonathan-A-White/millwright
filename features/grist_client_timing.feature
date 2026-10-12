Feature: An app reports the phone's part of a grist's round trip, and mw grist timing shows the whole of it
  A grist request may carry an optional client_timing about the app's previous
  answered grist: its txid and, in milliseconds, tap_to_sent (the user's tap to
  the request accepted by the backend, upload included), delivered_to_seen (the
  answer delivered on the backend to the app's poll seeing it), seen_to_shown
  (to the answer drawn on screen), network (the round trip of the poll that saw
  the answer plus that of the request that sent it) and, optionally,
  poll_network (the poll's round trip alone). The mill records it against that
  earlier grist, in client_timing.jsonl beside grinds.jsonl, and mw grist
  timing shows tap_to_sent, delivered_to_seen, seen_to_shown, network, the wait
  between checks (delivered_to_seen less poll_network) and a round trip (tap to
  shown) beside the mill's phases. A client_timing the mill cannot trust is
  ignored and logged; it never refuses the grist.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the grind answers with a sweep result

  Scenario: A client_timing for the earlier answer is recorded and shown with a round-trip total
    Given the mill's clock reads 3.2 seconds after the grist was sent
    And the follower noticed the grist 3.0 seconds after it was sent
    And delivering an answer takes 0.4 seconds
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Given the phone's next grist carries the client timing {"tap_to_sent": 900, "network": 260, "poll_network": 100, "delivered_to_seen": 3000, "seen_to_shown": 300} for the grist it sent before
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the mill recorded for the first grist the client timing tap_to_sent 900, network 260, poll_network 100, delivered_to_seen 3000 and seen_to_shown 300 milliseconds
    When mw grist timing is run for the app "cairn" and the kind "sweep"
    Then mw grist timing succeeded
    And it printed for "cairn sweep" 2 grinds and these phases:
      | phase             | count | median | p90 |
      | seconds           | 2     | 0.4    | 0.4 |
      | wake              | 2     | 3.0    | 3.0 |
      | pickup            | 2     | 0.4    | 0.6 |
      | slot              | 2     | 0.0    | 0.0 |
      | startup           | 2     | 0.0    | 0.0 |
      | deliver           | 2     | 0.4    | 0.4 |
      | total             | 2     | 3.8    | 4.0 |
      | tap_to_sent       | 1     | 0.9    | 0.9 |
      | delivered_to_seen | 1     | 3.0    | 3.0 |
      | wait              | 1     | 2.9    | 2.9 |
      | seen_to_shown     | 1     | 0.3    | 0.3 |
      | network           | 1     | 0.3    | 0.3 |
      | round_trip        | 1     | 7.8    | 7.8 |

  Scenario Outline: A client_timing the mill cannot trust is ignored and logged, and the grist is answered as usual
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Given the phone's next grist carries the client timing <timing> for the grist it sent before
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the mill recorded no client timing
    And the mill's report says it ignored the client timing

    Examples:
      | timing                                                  |
      | {"tap_to_sent": -5, "delivered_to_seen": 100}           |
      | {"tap_to_sent": 600001}                                 |
      | {"seen_to_shown": "fast"}                               |
      | {"network": 1e999}                                      |
      | {"unrelated": 5}                                        |

  Scenario: A client_timing naming a grist the mill never answered is ignored and logged
    Given the phone's next grist carries the client timing {"tap_to_sent": 900} for a grist named "direct:never-answered"
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the mill recorded no client timing
    And the mill's report says it ignored the client timing

  Scenario: A client_timing naming another key's grist is ignored and logged
    Given the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Given the phone's next grist carries the client timing {"tap_to_sent": 900} for the grist it sent before
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the mill recorded no client timing
    And the mill's report says it ignored the client timing

  Scenario: A client_timing for a grist already reported is ignored, the first report standing
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Given the phone's next grist carries the client timing {"tap_to_sent": 900} for the grist it sent before
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Given the phone's next grist carries the client timing {"tap_to_sent": 100} for the grist it sent first
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill recorded for the first grist the client timing tap_to_sent 900, network -, poll_network -, delivered_to_seen - and seen_to_shown - milliseconds
    And the mill's report says it ignored the client timing

  Scenario: A grist without client_timing behaves exactly as today
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the mill recorded no client timing
    And the mill's report says nothing of client timing
