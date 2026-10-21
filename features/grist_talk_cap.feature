Feature: A talk's session ends at its context cap or turn cap, and starts again from its key points
  A talk's session grows with every turn. When a turn's result puts the talk's
  context tokens at or over [grist] talk_context_cap (default 80000), or its
  turns at [grist] talk_max_turns (default 40), the mill runs one more
  --resume turn on that session asking for its key points (a fixed prompt, a
  schema of {key_points}, at most 1500 characters), keeps them on the talk's
  record and ends the session. The app's turn is answered as it was: the
  answer's talk says "resumed". The next turn of the talk runs cold with a new
  session id, its message opening with `key_points_of_this_talk_so_far: <text>`
  and the request with the app's history; its answer says talk.session
  "refreshed" and carries key_points once, and its turns restart at 1. A
  session that cannot write key points is ended without them, and the next
  turn is "fresh" (because "ended") with the app's history. An ended session's
  transcript is deleted 24 hours after its end, and a live talk unused for 30
  days is ended and its transcript deleted, both at the next talk grist for
  the app. `mw grist talks --end <app> <talk>` ends one now.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the mill runs its talk grinds through a stand-in claude

  Scenario: A turn that reaches the context cap ends the session with its key points and the next turn starts from them
    Given the talk context cap is 2510 tokens
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Then the answer to "q1" says the talk's session is "fresh" because "new", at turn 1
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the answer to "q2" says the talk's session is "resumed", at turn 2
    And session 3 resumed session 1's with the key-points prompt and schema
    And the talk's record holds the key points "The reader's son Luke is 14." and its session is ended
    Given the phone sends a talk grist in "rom.8@1" asking "q3"
    When the mill grinds again
    Then session 4 started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the last session's id is not session 1's
    And session 4's message opens with the key points "The reader's son Luke is 14." and then the request with its history
    And the answer to "q3" says the talk's session is "refreshed", at turn 1, with the key points "The reader's son Luke is 14."
    And the talk's record holds no key points
    Given the phone sends a talk grist in "rom.8@1" asking "q4"
    When the mill grinds again
    Then session 5 resumed session 4's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the answer to "q4" says the talk's session is "resumed", at turn 2

  Scenario: A turn that reaches the turn cap does the same with the context small
    Given the talk turn cap is 2
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Then 1 sessions ran
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then the answer to "q2" says the talk's session is "resumed", at turn 2
    And session 3 resumed session 1's with the key-points prompt and schema
    And the talk's record holds the key points "The reader's son Luke is 14." and its session is ended
    Given the phone sends a talk grist in "rom.8@1" asking "q3"
    When the mill grinds again
    Then the answer to "q3" says the talk's session is "refreshed", at turn 1, with the key points "The reader's son Luke is 14."

  Scenario: A talk under both caps keeps its session and asks for no key points
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then 2 sessions ran
    And the answer to "q2" says the talk's session is "resumed", at turn 2
    And the talk's record holds no key points

  Scenario: A session that cannot write key points is ended without them
    Given the talk turn cap is 2
    And every key-points call exits 1
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then the answer to "q2" says the talk's session is "resumed", at turn 2
    And the talk's record holds no key points and its session is ended
    Given the phone sends a talk grist in "rom.8@1" asking "q3"
    When the mill grinds again
    Then the answer to "q3" says the talk's session is "fresh" because "ended", at turn 1
    And session 4 started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 4's message holds the request with its history
    And session 4's message opens with no key points

  Scenario: A session whose key points come back empty is ended without them
    Given the talk turn cap is 2
    And every key-points call answers with empty key points
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then the talk's record holds no key points and its session is ended

  Scenario: An ended session's transcript is deleted a day after its end, and not before
    Given the talk turn cap is 2
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then session 1's transcript is kept
    Given the talk's ended session ended 23 hours ago
    And the phone sends a talk grist in "other" asking "o1"
    When the mill grinds again
    Then session 1's transcript is kept
    Given the talk's ended session ended 25 hours ago
    And the phone sends a talk grist in "third" asking "t1"
    When the mill grinds again
    Then session 1's transcript is gone
    And the other talk's session is kept

  Scenario: A live talk unused for 30 days is ended and its transcript deleted
    Given the phone sends a talk grist in "old" asking "q1"
    When the mill grinds
    Given the talk "old" last turned 31 days ago
    And the phone sends a talk grist in "new" asking "n1"
    When the mill grinds again
    Then session 1's transcript is gone
    And the talk "old" has no live session and no key points
    When mw grist talks is run
    Then it prints no line for "old"

  Scenario: A live talk unused for 29 days is kept
    Given the phone sends a talk grist in "old" asking "q1"
    When the mill grinds
    Given the talk "old" last turned 29 days ago
    And the phone sends a talk grist in "new" asking "n1"
    When the mill grinds again
    Then session 1's transcript is kept

  Scenario: mw grist talks --end ends a talk now without key points
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    And mw grist talks --end is run for cairn's "rom.8@1"
    Then it printed that it ended 1 talk
    And the talk's record holds no key points and its session is ended
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then 2 sessions ran
    And the answer to "q2" says the talk's session is "fresh" because "ended", at turn 1

  Scenario: mw grist talks --end of a talk the mill keeps no session for is an error
    When mw grist talks --end is run for cairn's "nothing"
    Then it refused, saying the mill keeps no live talk "nothing" of cairn
