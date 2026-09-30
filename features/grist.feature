Feature: mw grist grind, the mill
  An app sends the factory grist: AI work to be answered, not built, sealed to
  the mill key and carried by the postern backend (postern's docs/protocol.md
  section 18). mw grist grind is one pass of the mill: it reads what is waiting
  for the mill key, answers each grist exactly once with a grist record sealed
  to its sender, and ends. It refuses what the sender's licence, the app's
  grind or the factory's ceilings do not allow, without a session; otherwise it
  grinds it in one short harness session with no seat, which takes one of this
  host's story slots, first come first served.

  Background:
    Given a mill on the host "laptop" with a cap of 1
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"

  Scenario: A grist is ground, answered to its sender, and its photos deleted
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the answer is a grist record from the mill key, sealed to the phone, re the grist
    And the answer says "answered" with the grind's answer and the commit "0123456789abcdef0123456789abcdef01234567"
    And the grind ran on "sonnet" at "low" effort, given the photo by its full path and the request between its markers
    And the grind's private directory is gone
    And the grist's photos were deleted from the backend
    And the mill's record holds one line for the grist, "answered", with its Fuel and nothing of its content

  Scenario: A grist whose app the sender's licence does not open is refused
    Given a phone whose licence opens "spellforge"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And the answer says "refused" because "This phone's licence does not open the app named in the grist."
    And no grind was run

  Scenario: The Governor's own key may use an app's grinds
    Given the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting

  Scenario: A grist for a grind the app does not have is refused
    Given the phone sends a "cairn" "inventory" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "The app has no grind for this kind of grist."
    And no grind was run

  Scenario: A grist for a version the grind does not take is refused
    Given the phone sends a "cairn" "sweep" grist, version "2.0", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "The app's grind does not take this version of the grist."

  Scenario: A grist carrying more photos than its grind takes is refused
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 5 photos
    When the mill grinds
    Then the answer says "refused" because "This grist carries 5 photos; its grind takes at most 4."
    And no grind was run
    And the grist's photos were deleted from the backend

  Scenario: The factory's ceiling is lower than the grind's own limit
    Given the factory allows at most 2 photos a grist
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 3 photos
    When the mill grinds
    Then the answer says "refused" because "This grist carries 3 photos; its grind takes at most 2."

  Scenario: A grind whose model the factory does not allow is refused
    Given the factory allows only the models "haiku"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "The factory does not allow this grind's model."
    And no grind was run

  Scenario: A grist that asks for a model and an effort is ground with them
    Given the grist asks for the model "haiku" and the effort "medium"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the grind was called on "haiku" at "medium" effort
    And the mill's record says the model "haiku" came from the "grist" and the effort "medium" came from the "grist"

  Scenario: A grist that asks for only a model takes the grind file's effort
    Given the grist asks for the model "opus" and the effort ""
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the grind was called on "opus" at "low" effort
    And the mill's record says the model "opus" came from the "grist" and the effort "low" came from the "grind file"

  Scenario: A grist that asks for nothing is ground with the grind file's model and effort
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the grind was called on "sonnet" at "low" effort
    And the mill's record says the model "sonnet" came from the "grind file" and the effort "low" came from the "grind file"

  Scenario: A grist asking for a model the factory does not allow is refused
    Given the factory allows only the models "haiku,sonnet"
    And the grist asks for the model "opus" and the effort ""
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "The factory does not allow the model opus this grist asks for."
    And no grind was run

  Scenario: A grist asking for an effort above the factory's cap is refused
    Given the grist asks for the model "" and the effort "max"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "The factory does not allow the effort max this grist asks for."
    And no grind was run

  Scenario: The factory's effort cap can be widened
    Given the factory allows only the efforts "low,max"
    And the grist asks for the model "" and the effort "max"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the grind was called on "sonnet" at "max" effort

  Scenario: A sender over the daily limit is refused
    Given the factory allows 2 grist a day from one key
    And the phone has already sent 2 grist today
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the answer says "refused" because "This key has sent its 2 grist for today; send it again tomorrow."
    And no grind was run

  Scenario: A grind that fails is answered failed, and recorded
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind's session ends in an error
    When the mill grinds
    Then the mill answered 0, refused 0, failed 1, and left 0 waiting
    And the answer says "failed" because "The grind's session did not finish; send it again."
    And the mill's record holds one line for the grist, "failed", with its Fuel and nothing of its content
    And the grind's private directory is gone

  Scenario: A grind that gives no answer in its schema is answered failed
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind finishes without a structured answer
    When the mill grinds
    Then the answer says "failed" because "The grind's answer did not match its schema; send it again."

  Scenario: A grist is ground once, however many passes read it
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    And the mill grinds again, reading from the start
    Then 1 grind was run
    And 1 answer was delivered

  Scenario: Records that are not grist, or not for the mill, are left alone
    Given a message to the mill key
    And a grist from the phone to another key
    When the mill grinds
    Then no answer was delivered
    And no grind was run
    And the mill's cursor is past them

  Scenario: A grist waits, without a grind, while the host is at its cap
    Given a story is already running on "laptop"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the mill answered 0, refused 0, failed 0, and left 1 waiting
    And the mill says it is waiting because "the host is at its cap (1 of 1 sessions running)"
    And no grind was run
    And no answer was delivered
    And the mill's cursor is before the grist
    When the running story finishes
    And the mill grinds again
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting

  Scenario: An answer the backend will not take is kept and delivered by the next pass
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    And the backend will not take a record
    When the mill grinds
    Then no answer was delivered
    And the grist's photos are still on the backend
    When the backend takes records again
    And the mill grinds again
    Then 1 grind was run
    And 1 answer was delivered
    And the grist's photos were deleted from the backend

  Scenario: A pass that finds another pass running leaves the grist to it
    Given another pass of the mill is running
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill says another pass is running
    And no answer was delivered

  Scenario: mw dispatch counts a running grind as one of its sessions
    Given a grind is running on "laptop"
    And a story is ready on "laptop"
    When mw dispatch runs on "laptop" with a cap of 1
    Then dispatch started nothing, since "laptop has taken 1 of the 1 sessions it may run at once"
    And dispatch says a grist grind holds one of its sessions

  Scenario: A grist left waiting is answered by the next dispatch tick, with no new grist and no hook
    Given a story is already running on "laptop"
    And "laptop" is home
    And [grist] is configured
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    Then the mill answered 0, refused 0, failed 0, and left 1 waiting
    When the running story finishes
    And mw dispatch runs on "laptop" with a cap of 1
    Then the dispatch's grist pass answered 1, refused 0, failed 0, and left 0 waiting
    And 1 grind was run
    And 1 answer was delivered

  Scenario: A dispatch tick on a host that is not home runs no grist pass
    Given "desktop" is home
    And [grist] is configured
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When mw dispatch runs on "laptop" with a cap of 1
    Then the dispatch ran no grist pass
    And no grind was run
    And no answer was delivered

  Scenario: A dispatch tick with no [grist] runs no grist pass
    Given "laptop" is home
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When mw dispatch runs on "laptop" with a cap of 1
    Then the dispatch ran no grist pass
    And no grind was run
    And no answer was delivered
