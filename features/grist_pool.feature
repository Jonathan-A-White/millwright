Feature: A warm pool of clean grind sessions, so a grist does not wait for one to start
  The follower's mill keeps, for each app and kind, sessions already started
  with the grind's system prompt, model, effort and schema, each waiting for
  its one message in a private directory of its own. A grist for that kind is
  given one: its photos are written into the session's directory and its
  request is the message. The session answers that grist and ends, and is never
  given another; the pool starts its replacement. A kind keeps as many warm
  sessions as it has lately had grists in flight at once, never more than the
  grind slots ([grist] concurrency), and none once it has had no grist for the
  quiet spell ([grist] pool_quiet, default 10 min). A warm session older than
  [grist] pool_max_age (default 15 min), or started for a grind that has since
  changed, is retired unused. A warm session that cannot answer leaves the
  grist to a session started cold, as every grist was before the pool. A pass
  run by mw grist grind or the dispatch tick has no pool, and grinds cold.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the grind answers with a sweep result
    And the mill grinds up to 2 grists at once
    And the mill keeps a warm pool

  Scenario: The first grist starts cold, and leaves a warm session for the next
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 cold session and 0 warm sessions were given grists
    And the pool holds 1 warm session for "cairn" "sweep"
    And mw status's pool line reads "grist pool: cairn/sweep 1 warm, 0 busy"

  Scenario: A grist for a kind with a warm session is answered by it, and the pool starts its replacement
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 cold session and 1 warm session were given grists
    And the warm session was started for the grind's own system prompt, model, effort and schema
    And the warm session's message names the photo in its own directory, and the system prompt's tag
    And the pool holds 1 warm session for "cairn" "sweep"
    And no warm session was given more than one grist

  Scenario: Grists in a row each get a session of their own
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then 1 cold session and 3 warm sessions were given grists
    And no warm session was given more than one grist
    And the pool holds 1 warm session for "cairn" "sweep"

  Scenario: Two grists at once grow the pool to two, and no session takes both
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    And the warm sessions are held until 2 of them are answering at once
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 2, refused 0, failed 0, and left 0 waiting
    And 1 cold session and 2 warm sessions were given grists
    And no warm session was given more than one grist
    And the pool holds 2 warm sessions for "cairn" "sweep"

  Scenario: No grist for the quiet spell shrinks the pool to nothing, and the next grist starts cold
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When 10 minutes pass with no grist
    Then the pool holds no session for "cairn" "sweep"
    And no warm session is left running
    And 1 warm session was retired unused
    When the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 cold sessions and 0 warm sessions were given grists

  Scenario: A changed grind retires the warm sessions started for it unused
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When the grind "sweep"'s instructions change at commit "1111111111111111111111111111111111111111"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 warm session was retired unused
    And 2 cold sessions and 0 warm sessions were given grists
    And the pool holds 1 warm session for "cairn" "sweep"
    And the warm session waiting was started for the changed instructions

  Scenario: A warm session past its age bound is retired, and replaced while grist still comes
    Given the pool's quiet spell is 30 minutes
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When 15 minutes pass with no grist
    Then 1 warm session was retired unused
    And the pool holds 1 warm session for "cairn" "sweep"

  Scenario: A warm session that fails leaves the grist to a cold start, and it is answered
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When the warm sessions fail when given a grist
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 cold sessions and 1 warm session were given grists
    And the report notes that the warm session could not answer, so the grist ran cold

  Scenario: A warm session that cannot start leaves every grist to a cold start
    Given the harness cannot start a warm session
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 cold sessions and 0 warm sessions were given grists
    And the pool holds no session for "cairn" "sweep"

  Scenario: A warm session that ended on its own is not given a grist
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When the warm session ends on its own
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 cold sessions and 0 warm sessions were given grists
    And no warm session was given more than one grist

  Scenario: A grist refused before its grind is given no warm session
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the mill grinds
    When the phone sends a "cairn" "sweep" grist, version "9.9", with 1 photo
    And the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And 1 cold session and 0 warm sessions were given grists
    And the pool holds 1 warm session for "cairn" "sweep"
