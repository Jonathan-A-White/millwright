Feature: The mill keeps one Claude session per talk, within the hour
  A grist may name its talk (`talk`, at most 80 characters, beside app, kind
  and v). The mill keeps a record per app, sender key and talk under its state
  directory (talks/), and runs every talk grist of an app from one fixed
  directory (cwd/<app>) with CLAUDE_CODE_PROMPT_CACHE_TTL=1h, never on the warm
  pool. A talk's first turn starts a session with --session-id and without
  --no-session-persistence; a later turn within the hour resumes it with
  --resume, the same flags otherwise, and the request's history left out (the
  session has it). One grist at a time per talk. A turn an hour or more after
  the last, a changed spec or billing, a lost transcript or a resume that fails
  starts a fresh session with the history kept. The answer says which: talk
  {session: fresh|resumed, reason, turns, context_tokens}. A grist with no talk
  runs exactly as before. `mw grist talks` lists the live talks.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the mill runs its talk grinds through a stand-in claude

  Scenario: A talk's first turn starts a session and its second resumes it without the history
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Then session 1 started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 1's message holds the request with its history
    And the answer to "q1" says the talk's session is "fresh" because "new", at turn 1
    Given the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 2 was given the same --model, --system-prompt and --json-schema as session 1
    And session 2's message holds the request without its history
    And the answer to "q2" says the talk's session is "resumed", at turn 2

  Scenario: A grist with no talk runs exactly as before
    Given the phone sends a grist asking "plain" with no talk
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And session 1's arguments, its system prompt, schema and message set aside, are exactly:
      """
      --print
      --output-format
      json
      --model
      sonnet
      --effort
      low
      --restricted
      --tools
      Read
      --strict-mcp-config
      --permission-prompts
      none
      --no-session-persistence
      --disable-slash-commands
      --safe-mode
      --system-prompt
      <system prompt>
      --json-schema
      <schema>
      <message>
      """
    And session 1 had no CLAUDE_CODE_PROMPT_CACHE_TTL
    And the answer to "plain" says nothing of a talk
    And the mill keeps no talk

  Scenario: A talk grist never takes a warm session
    Given the mill keeps a warm pool of real sessions
    And the phone sends a talk grist in "rom.8@1" asking "q1"
    When the mill grinds
    Then the answer to "q1" says the talk's session is "fresh" because "new", at turn 1
    And no warm session was started

  Scenario: Two grists of one talk run one after the other, and another talk's beside them
    Given the mill grinds up to 3 grists at once
    And the phone sends a talk grist in "a" asking "a1"
    And the phone sends a talk grist in "a" asking "a2"
    And the phone sends a talk grist in "b" asking "b1"
    And the stand-in holds "a1" until "b1" has started
    When the mill grinds
    Then the mill answered 3, refused 0, failed 0, and left 0 waiting
    And "b1" started before "a1" ended
    And "a2" started after "a1" ended, resuming its session
    And the answer to "a2" says the talk's session is "resumed", at turn 2
    And the answer to "b1" says the talk's session is "fresh" because "new", at turn 1

  Scenario Outline: A talk starts fresh, with its history, when its session cannot be resumed as it was
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And <change>
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then the last session started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the last session's id is not session 1's
    And the last session's message holds the request with its history
    And the answer to "q2" says the talk's session is "fresh" because "<reason>", at turn 1
    And the talk's record names session 1's as ended

    Examples:
      | change                                                                       | reason        |
      | the grist asks for the model "opus" and the effort "low"                     | spec          |
      | the app's grind instructions change                                          | spec          |
      | api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600 | billing   |
      | the talk's transcript is deleted                                             | transcript    |
      | every resume exits 1                                                         | resume-failed |

  Scenario: A resume that fails is tried once, then the turn runs fresh
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And every resume exits 1
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then 3 sessions ran
    And session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the mill answered 1, refused 0, failed 0, and left 0 waiting

  Scenario: A talk quiet for 61 minutes is never resumed: it starts fresh, because quiet
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And the talk's last turn was 61 minutes ago
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then no session ran with --resume
    And the last session's message holds the request with its history
    And the answer to "q2" says the talk's session is "fresh" because "quiet", at turn 1
    And the talk's record names session 1's as ended

  Scenario: A talk quiet for 59 minutes is resumed
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And the talk's last turn was 59 minutes ago
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And the answer to "q2" says the talk's session is "resumed", at turn 2

  Scenario: The record's line says the talk, its session and this turn's cost
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    When the mill grinds again
    Then the mill's last line names the talk "rom.8@1", session 1's id, resumed, and costs 0.0125 of a session total of 0.025

  Scenario: A talk longer than 80 characters is refused
    Given the phone sends a talk grist in "a-talk-name-that-runs-on-and-on-past-the-eighty-characters-the-mill-allows-for-one" asking "q1"
    When the mill grinds
    Then the answer says "refused" because "This grist is not in a shape the factory reads."
    And no session ran

  Scenario: mw grist talks lists the live talks
    Given the phone sends a talk grist in "rom.8@1" asking "q1"
    And the mill grinds
    And the phone sends a talk grist in "rom.8@1" asking "q2"
    And the mill grinds again
    And the phone sends a talk grist in "old" asking "q3"
    And the mill grinds again
    And the talk "old" last turned 61 minutes ago
    When mw grist talks is run
    Then it prints one line for "rom.8@1": cairn, 2 turns, 2,510 context tokens, 2,000 cache read, 0s since last use, and session 1's id
    And it prints no line for "old"
    When mw grist talks --json is run
    Then its JSON lists "rom.8@1" with 2 turns, 2510 context tokens, 2000 cache read and session 1's id
