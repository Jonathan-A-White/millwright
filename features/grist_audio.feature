Feature: mw grist grind takes a recording, scores it, and keeps every grind's raw record
  A grind may take an audio attachment and say it scores it (its file's
  scoring: audio, and the field of the app's request holding the text that
  was read). The mill then scores the recording with every engine this host is
  configured to run, before the harness session, and hands the session the
  results as text under reading_result in the request: never the audio, which
  is not in the session's directory. An engine that fails is written down in
  its place and never fails the grind. Every grind's raw record is kept under
  the state directory's runs/<txid>/ and listed by mw grist runs
  (docs/scorers.md).

  Background:
    Given a mill on the host "laptop" with a cap of 1
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And the grind "reading" takes a webm recording and scores it against "target_text"
    And the grind "listening" takes a webm recording but scores nothing
    And the scorer engines "local" and "azure" are configured, each scoring any reading as "the cat sat"
    And the mill keeps its runs under its state directory
    And a phone whose licence opens "cairn"
    And the grind answers with a sweep result

  Scenario: A recording is scored by every engine before the session, and the session is given the results as text
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the engines "local" and "azure" were each given the recording, the target "the cat sat" in "en" as "audio/webm"
    And the session's request carries a reading_result from each of "local" and "azure", with 3 words each
    And the session's request keeps the app's own fields
    And the session's directory held no audio
    And the session's prompt and system prompt hold no audio

  Scenario: The answer to a scored grist carries the scores beside the answer, never the audio
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the app's decrypted reply has the grind's answer and a reading_result from "local", with 3 words
    And the app's decrypted reply has a reading_result from "azure", with 3 words
    And the app's decrypted reply has no reading_results
    And the app's decrypted reply holds no audio

  Scenario: An engine that failed is its entry's error in the answer too
    Given the engine "azure" breaks down with "the azure key was refused"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the app's decrypted reply has a reading_result from "local", with 3 words
    And the app's decrypted reply has the error "the azure key was refused" for the engine "azure"

  Scenario: A grist of photos is answered with no reading_result
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the app's decrypted reply has the grind's answer and no reading_result

  Scenario: An engine that fails is written down in its place and does not fail the grind
    Given the engine "azure" breaks down with "the azure key was refused"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the session's request carries a reading_result from "local", with 3 words
    And the session's request carries the error "the azure key was refused" for the engine "azure"

  Scenario: A grind that scores nothing asks no engine and its request is as the app sent it
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And no engine was given a recording
    And the session's request has no reading_result

  Scenario: A recording sent to a grind that does not score is refused
    Given the phone sends a "cairn" "listening" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the answer says "refused" because "A photo in this grist is of a type its grind does not take."
    And no grind was run
    And no engine was given a recording

  Scenario: Every grind's raw record is kept in its own directory
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the run's directory holds input.json, attachment-1.webm, scorers.json, answer.json and timing.json
    And the run's input.json carries reading_result.local and no audio bytes
    And the run's attachment-1.webm is the recording as it was sent
    And the run's scorers.json holds the results of "local" and "azure" for the target "the cat sat"
    And the run's answer.json says "answered" with the grind's answer
    And the run's timing.json has when it was received, scored, started and answered, and how many seconds each took

  Scenario: A grind of photos is kept too, with nothing scored
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the run's directory holds input.json, attachment-1.jpg, scorers.json, answer.json and timing.json
    And the run's scorers.json holds no results

  Scenario: A run's timing.json also keeps when the grist was sent and each scorer's seconds
    Given the mill's clock moves a second at each look
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the run's timing.json has when the grist was sent, so the queue wait, and the seconds of "local" and "azure"

  Scenario: A grind that fails is kept too
    Given the grind's session ends in an error
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the run's answer.json says "failed"

  Scenario: mw grist runs lists each run, with its seconds
    Given the mill's clock moves a second at each look
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    And mw grist runs lists the runs
    Then the list has one line for the grist with its kind "reading", its model "sonnet", more than 0 seconds, and "answered"

  Scenario: mw grist runs lists only the runs since a time
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    And mw grist runs lists the runs since a day after they were made
    Then the list has no lines

  Scenario: A grind file may set the most turns its session takes
    Given the grind "reading" takes at most 6 turns
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the session was called with at most 6 turns

  Scenario: A grind file that sets no limit leaves the session's turns as they were
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the session was called with no limit on its turns

  Scenario: A grist that names a language its grind allows is scored in it, and the run records it
    Given the grind "reading" scores a webm recording against "target_text" in the languages "el" and "en"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording in the language "el"
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the engine "local" was given the language "el"
    And the run's scorers.json records the language "el"

  Scenario: A grist that names no language is scored in the first its grind allows
    Given the grind "reading" scores a webm recording against "target_text" in the languages "el" and "en"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the engine "local" was given the language "el"
    And the run's scorers.json records the language "el"

  Scenario: A grist that names a language its grind does not allow is refused, naming the ones it does
    Given the grind "reading" scores a webm recording against "target_text" in the languages "el" and "en"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording in the language "fr"
    When the mill grinds
    Then the answer says "refused" because "This grist asks for the language fr; its grind scores in el and en."
    And no grind was run
    And no engine was given a recording

  Scenario: A grind that names no languages scores in English, as it always did
    Given the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the engine "local" was given the language "en"
    And the run's scorers.json records the language "en"

  Scenario: A grind that names a language the mill does not know is refused as a broken grind
    Given the grind "reading" scores a webm recording against "target_text" in the languages "tlh" and "en"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording
    When the mill grinds
    Then the answer says "refused" because "The app's grind for this kind of grist cannot be read."
    And no engine was given a recording

  Scenario: An engine that has no such language is skipped for the recording and the pass says so
    Given the engine "azure" has no "el"
    And the grind "reading" scores a webm recording against "target_text" in the languages "el" and "en"
    And the phone sends a "cairn" "reading" grist, version "1.1", of "the cat sat" read aloud in a webm recording in the language "el"
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the engine "local" was given the language "el"
    And the engine "azure" was given no recording
    And the session's request carries a reading_result from "local", with 3 words
    And the pass notes "azure skipped: no el"
