Feature: mw grist timing --otlp writes each answered grist as an OpenTelemetry trace
  Grafana, Tempo, Jaeger or any other OpenTelemetry tool can read the factory's
  timing from a file, with no service to run. mw grist timing --otlp writes the
  mill's record as OTLP JSON (resourceSpans, scopeSpans, spans): one trace for
  each grist, its id the first 16 bytes of the grist's txid, a root span
  "grist <kind>" and one child for each phase the record knows: phone and
  network (the app's report, service name the app) and wake, pickup, slot,
  startup, model and deliver (service name mw-mill). A phase nobody timed is
  left out. The line's own time is when the answer was delivered; the phases
  run back from it.

  Background:
    Given a mill on the host "laptop"

  Scenario: A grist with every phase known is one trace of eight spans under a root
    Given the record holds a grist whose txid is "direct:cacaf9063f4a4e1fa3b8e2b5f0f1b8b1849a" answered by "lampas" "verse-read" at 12:00:30.0 UTC, with these phases in seconds:
      | seconds | wake_s | pickup_s | slot_s | startup_s | model_s | deliver_s | total_s |
      | 6.0     | 1.0    | 0.5      | 0.0    | 0.8       | 4.0     | 0.4       | 7.0     |
    And the app reported for that grist: tap_to_sent 900, network 260, delivered_to_seen 3000 and seen_to_shown 300 milliseconds
    When mw grist timing is run with --otlp
    Then mw grist timing succeeded
    And the output is valid OTLP JSON
    And it holds one trace, whose id is "cacaf9063f4a4e1fa3b8e2b5f0f1b8b1"
    And the root span is named "grist verse-read" and runs from 12:00:22.1 to 12:00:30.0
    And its eight phase spans have the root for their parent, and run:
      | span    | service | start      | end        |
      | phone   | lampas  | 12:00:22.1 | 12:00:23.0 |
      | network | lampas  | 12:00:22.74 | 12:00:23.0 |
      | wake    | mw-mill | 12:00:23.0 | 12:00:24.0 |
      | pickup  | mw-mill | 12:00:24.0 | 12:00:24.5 |
      | slot    | mw-mill | 12:00:24.5 | 12:00:24.5 |
      | startup | mw-mill | 12:00:24.5 | 12:00:25.3 |
      | model   | mw-mill | 12:00:25.3 | 12:00:29.3 |
      | deliver | mw-mill | 12:00:29.6 | 12:00:30.0 |

  Scenario: An old line with only its seconds is one span
    Given the record holds an old line of "lampas" "verse-read" that took 7.5 seconds
    When mw grist timing is run with --otlp
    Then mw grist timing succeeded
    And the output is valid OTLP JSON
    And the output holds 1 span, the root "grist verse-read"
