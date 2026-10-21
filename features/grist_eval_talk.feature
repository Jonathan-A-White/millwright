Feature: mw grist eval and the smoke run a two-turn talk example and leave no talk behind
  An example (grinds/examples/<kind>/<name>.json) may hold a second turn,
  turn_2: {request, photos}, beside its request, which is turn 1. The eval
  (`mw grist eval <app> <kind> --example <name>`) and the smoke send turn 1
  with a talk named eval:<example>:<nonce> (smoke:<kind>/<example>:<nonce>
  for the smoke), then turn 2 under the same talk, so the mill resumes the
  session and leaves the history out of the request. The example's expect
  judges turn 2's answer; turn 1's must only fit the answer schema. Every talk
  the run started is ended when it has been answered: its record and its
  session's transcript are deleted. The next run tidies what a run killed
  midway left: eval: and smoke: talks of the app unused for an hour.
  An example with no turn_2 runs as it did.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the mill runs its talk grinds through a stand-in claude

  Scenario: The eval sends turn 2 of an example as a resumed turn without the history, and ends the talk
    Given the rig of "cairn" holds the example "sweep/remembers" containing:
      """
      {
        "schemaVersion": "1.1",
        "request": {"q":"turn1","history":[{"role":"user","text":"earlier"}]},
        "turn_2": {"request": {"q":"turn2","history":[]}},
        "expect": {"placeName": "Top drawer", "items.0.name": "scissors"}
      }
      """
    When mw grist eval is run for the example "remembers" of "cairn" kind "sweep"
    Then the eval passed
    And session 1 started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 1's message holds the request with its history
    And session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 2's message holds the request without its history
    And the eval sent both turns under one talk named like "eval:remembers:<nonce>"
    And the eval sent turn 2 with no history
    And the mill keeps no talk
    And no transcript of a session is left

  Scenario: The eval judges turn 2's answer by the example's expectations
    Given the rig of "cairn" holds the example "sweep/remembers" containing:
      """
      {
        "schemaVersion": "1.1",
        "request": {"q":"turn1"},
        "turn_2": {"request": {"q":"turn2"}},
        "expect": {"placeName": "Garage"}
      }
      """
    When mw grist eval is run for the example "remembers" of "cairn" kind "sweep"
    Then the eval failed, saying "turn 2"
    And the eval failed, saying "placeName"
    And the mill keeps no talk
    And no transcript of a session is left

  Scenario: An example with one turn runs exactly as before and starts no talk
    Given the rig of "cairn" holds the example "sweep/plain" containing:
      """
      {
        "schemaVersion": "1.1",
        "request": {"q":"only"},
        "expect": {"placeName": "Top drawer"}
      }
      """
    When mw grist eval is run for the example "plain" of "cairn" kind "sweep"
    Then the eval passed
    And 1 sessions ran
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
    And the eval sent no talk
    And the mill keeps no talk

  Scenario: The smoke runs a two-turn example the same way and leaves no talk
    Given the rig of "cairn" holds the example "sweep/remembers" containing:
      """
      {
        "schemaVersion": "1.1",
        "request": {"q":"turn1","history":[]},
        "turn_2": {"request": {"q":"turn2"}},
        "expect": {"placeName": "Top drawer"}
      }
      """
    When mw grist smoke is run for "cairn" with talks
    Then the smoke with talks passed, saying "1 examples"
    And session 2 resumed session 1's from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h
    And session 2's message holds the request without its history
    And the eval sent both turns under one talk named like "smoke:sweep/remembers:<nonce>"
    And the mill keeps no talk
    And no transcript of a session is left

  Scenario: A run killed after turn 1 leaves a record that the next run ends
    Given the rig of "cairn" holds the example "sweep/plain" containing:
      """
      {
        "schemaVersion": "1.1",
        "request": {"q":"only"},
        "expect": {"placeName": "Top drawer"}
      }
      """
    And the phone sends a talk grist in "eval:remembers:deadbeef" asking "killed1"
    And the phone sends a talk grist in "smoke:sweep/remembers:cafef00d" asking "killed2"
    And the phone sends a talk grist in "eval:recent:0badf00d" asking "recent"
    And the phone sends a talk grist in "rom.8@1" asking "tutor"
    And the mill grinds
    And the talk "eval:remembers:deadbeef" last turned 90 minutes ago
    And the talk "smoke:sweep/remembers:cafef00d" last turned 61 minutes ago
    And the talk "eval:recent:0badf00d" last turned 10 minutes ago
    And the talk "rom.8@1" last turned 90 minutes ago
    When mw grist eval is run for the example "plain" of "cairn" kind "sweep"
    Then the eval passed
    And the mill keeps only the talks "eval:recent:0badf00d" and "rom.8@1"
    And the mill keeps 2 transcripts of sessions
