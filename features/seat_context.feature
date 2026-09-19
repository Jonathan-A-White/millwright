Feature: mw seat context
  mw seat context prints one line saying how full a seat's live session is
  against the limit at which it must hand off: the tokens the last assistant
  turn of the newest transcript put into the model's context, the limit, and
  whether the session is still ok or should hand off, with the first eight
  characters of the session's id. It reads a transcript and nothing else: no
  session is started, nothing is written and no fuel is spent.

  Scenario: A session under the limit is ok
    Given a transcript "0a1b2c3d4e5f" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 3            | 20000                   | 4000                        |
      | 1            | 24000                   | 6000                        |
      | 2            | 30000                   | 5000                        |
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=35002 handoff_at=180000 ok session=0a1b2c3d"

  Scenario: A session over the limit should hand off
    Given a transcript "9f8e7d6c5b4a" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 3            | 90000                   | 4000                        |
      | 1            | 120000                  | 30000                       |
      | 2            | 150000                  | 40000                       |
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=190002 handoff_at=180000 handoff session=9f8e7d6c"

  Scenario: A session exactly at the limit should hand off
    Given a transcript "11223344aabb" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 5            | 100000                  | 3000                        |
      | 10           | 170000                  | 9990                        |
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=180000 handoff_at=180000 handoff session=11223344"

  Scenario: The limit comes from the config file
    Given a transcript "0a1b2c3d4e5f" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 3            | 20000                   | 4000                        |
      | 2            | 30000                   | 5000                        |
    And the config file says the handoff limit is 30000
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=35002 handoff_at=30000 handoff session=0a1b2c3d"

  Scenario: The environment answers ahead of the config file
    Given a transcript "0a1b2c3d4e5f" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 2            | 30000                   | 5000                        |
    And the config file says the handoff limit is 30000
    And the environment says the handoff limit is 40000
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=35002 handoff_at=40000 ok session=0a1b2c3d"

  Scenario: Only the last assistant turn counts
    Given a transcript "cafe0123beef" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 4            | 170000                  | 30000                       |
      | 2            | 12000                   | 1000                        |
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=13002 handoff_at=180000 ok session=cafe0123"

  Scenario: The newest transcript is the one read
    Given a transcript "aaaa1111bbbb" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 2            | 190000                  | 1000                        |
    And a newer transcript "bbbb2222cccc" for the seat directory whose assistant turns used:
      | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
      | 2            | 2000                    | 1000                        |
    When mw seat context reads the seat directory
    Then seat context succeeds
    And the seat context line is "context=3002 handoff_at=180000 ok session=bbbb2222"

  Scenario: A seat directory with no transcript is an error naming the directory
    Given no transcript for the seat directory
    When mw seat context reads the seat directory
    Then seat context fails saying it looked in the seat directory's transcripts

  Scenario: A transcript with no assistant turn yet is an error naming the directory
    Given a transcript "0a1b2c3d4e5f" for the seat directory with no assistant turn
    When mw seat context reads the seat directory
    Then seat context fails saying it looked in the seat directory's transcripts
