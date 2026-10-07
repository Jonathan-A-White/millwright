Feature: mw grist score, a reading scored word by word
  A reading of a target text is handed to a scoring engine, which says for each
  word what phonemes were expected, what was produced, and whether the word
  was read right, left out, added, mispronounced or hesitated over. The engines
  are named in config ([scorers] engines), and each is behind one port, so a
  new engine is another adapter and not another command (docs/scorers.md).

  Background:
    Given the scorer engines "local" and "azure" are configured
    And the engine "local" scores any reading as the words "the", "cat" and "sat"

  Scenario: A reading is scored by the engine named, and the result printed as JSON
    Given an audio file "clip.webm" holding a reading
    When mw grist score reads "clip.webm" against "the cat sat" with the engine "local"
    Then mw grist score printed a result from the engine "local" with 3 words
    And the engine "local" was given the target "the cat sat" in "en" as "audio/webm"
    And the engine "azure" was given nothing

  Scenario: The language and the audio's type are passed on
    Given an audio file "clip.wav" holding a reading
    When mw grist score reads "clip.wav" against "la casa" in "es" with the engine "local"
    Then the engine "local" was given the target "la casa" in "es" as "audio/wav"

  Scenario: An engine that is not configured is refused, and the configured ones named
    Given an audio file "clip.webm" holding a reading
    When mw grist score reads "clip.webm" against "the cat sat" with the engine "nosuch"
    Then mw grist score refused, saying "no scorer engine named \"nosuch\""
    And mw grist score refused, saying "azure, local"
    And mw grist score printed nothing

  Scenario: An audio file that is not there is refused before any engine is asked
    When mw grist score reads "missing.webm" against "the cat sat" with the engine "local"
    Then mw grist score refused, saying "missing.webm"
    And the engine "local" was given nothing

  Scenario: An engine's failure is the command's failure
    Given an audio file "clip.webm" holding a reading
    And the engine "local" fails with "the scorer at http://127.0.0.1:8765 is not running"
    When mw grist score reads "clip.webm" against "the cat sat" with the engine "local"
    Then mw grist score refused, saying "not running"
    And mw grist score printed nothing
