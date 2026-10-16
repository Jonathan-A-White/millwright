Feature: mw grist smoke tests an app's grist end to end
  An app's grinds keep scenarios under grinds/examples/<kind>/<name>.json: a
  request, the photos beside it and what the answer must show (expect).
  `mw grist smoke <app>` sends every one through the live backend as the
  factory's test key and holds each answer to the grind's answer schema and to
  its expect (a field's check may hold "all", a list of checks that must every
  one hold). A landing that touches grinds/, an app's grist client paths, or
  (for the factory's own rig) the mill itself, runs it; a failure is an alarm,
  a line of mw status and a hold on the rig's open stories. A smoke the mill
  would not take for a reason that is not the examples' (the key's daily limit,
  its licence, the backend out of reach, the factory's usage limit) is "not
  run": it holds nothing. An example whose mill session did not finish is the
  mill's fault too: it is sent once more before it fails.

  Background:
    Given the app "trade-tracker" has the grind "price-check" whose answer must carry a price and a confidence
    And the app "trade-tracker" has the example "price-check/blank-paper" containing:
      """
      {"request": {"schemaVersion": "1", "note": "a blank sheet"},
       "photos": ["blank.jpg"],
       "expect": {"price": {"is_null": true}, "confidence": "low"}}
      """
    And the example "price-check/blank-paper" of "trade-tracker" has the photo "blank.jpg"

  Scenario: an example answered as it expects passes
    Given the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then the smoke sent 1 grist as "trade-tracker/price-check" with the photo "blank.jpg"
    And the smoke passed, saying "trade-tracker: ok, 1 examples"
    And no alarm was posted

  Scenario: an example whose schemaVersion stands beside its request is sent with that version
    Given the app "lampas" has the grind "tutor" whose answer must carry a price and a confidence
    And the app "lampas" has the example "tutor/hello" containing:
      """
      {"schemaVersion": "1", "request": {"note": "no version in here"}, "expect": {"confidence": "low"}}
      """
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "lampas"
    Then the smoke sent the grist of "lampas/tutor" with the version "1"
    And the smoke passed, saying "lampas: ok, 1 examples"

  Scenario: an example whose request holds the schemaVersion is still sent with it
    Given the app "cairn" has the grind "sweep" whose answer must carry a price and a confidence
    And the app "cairn" has the example "sweep/one" containing:
      """
      {"request": {"schemaVersion": "2"}, "expect": {"confidence": "low"}}
      """
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "cairn"
    Then the smoke sent the grist of "cairn/sweep" with the version "2"
    And the smoke passed, saying "cairn: ok, 1 examples"

  Scenario: an example with two different schemaVersions is wrong, and nothing is sent
    Given the app "lampas" has the grind "tutor" whose answer must carry a price and a confidence
    And the app "lampas" has the example "tutor/hello" containing:
      """
      {"schemaVersion": "1", "request": {"schemaVersion": "2"}, "expect": {"confidence": "low"}}
      """
    When mw grist smoke is run for "lampas"
    Then the smoke failed, saying "tutor/hello: the example is wrong: schemaVersion is 1 beside the request but 2 in it"
    And the smoke sent 0 grist

  Scenario: an answer that misses an expect fails, naming the example, the field, what was wanted and what came
    Given the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "price-check/blank-paper: the answer misses what the example expects: price: wanted null, got 12.5"

  Scenario: a refused grist (no licence) fails the smoke
    Given the mill refuses the next grist because "no licence opens trade-tracker"
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "the mill refused it: no licence opens trade-tracker"

  Scenario: an answer that does not fit the grind's schema fails the smoke
    Given the mill answers the next grist with:
      """
      {"price": "twelve", "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "does not fit the grind's schema: answer.price is a string"

  Scenario: an answer that never comes fails the smoke
    Given the mill does not answer the next grist
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "no answer in 5m0s"

  Scenario: a kind with no example is a warning, not a failure
    Given the app "trade-tracker" has the grind "label-read" whose answer must carry a price and a confidence
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then the smoke passed, saying "trade-tracker/label-read: no example"
    And the smoke sent 1 grist as "trade-tracker/price-check" with the photo "blank.jpg"

  Scenario: --kind smokes one grind only
    Given the app "trade-tracker" has the grind "label-read" whose answer must carry a price and a confidence
    When mw grist smoke is run for "trade-tracker" kind "label-read"
    Then the smoke sent 0 grist
    And the smoke passed, saying "trade-tracker/label-read: no example"

  Scenario: a landing in the app's rig that touches grinds/ smokes the app
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then the landing smoked "trade-tracker"
    And no alarm was posted

  Scenario: a landing that touches the app's configured grist client paths smokes the app
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the rig "trade-tracker" names the grist client path "src/grist"
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When a landing in "trade-tracker" changes "src/grist/send.ts"
    Then the landing smoked "trade-tracker"

  Scenario: a landing that touches nothing of grist smokes nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    When a landing in "trade-tracker" changes "src/ui/button.ts"
    Then the landing smoked nothing

  Scenario: a change to the mill in the factory's rig smokes every app with grinds
    Given the app "cairn" has the grind "sweep" whose answer must carry a price and a confidence
    And the app "cairn" has the example "sweep/one" containing:
      """
      {"request": {"schemaVersion": "1"}, "expect": {"confidence": "low"}}
      """
    And the app "plain" has no grinds
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When a landing in "millwright" changes "application/gristsend.go"
    Then the landing smoked "cairn, plain, trade-tracker"
    And the smoke sent 1 grist as "cairn/sweep" with no photo
    And the smoke sent 1 grist as "trade-tracker/price-check" with the photo "blank.jpg"

  Scenario: a failing smoke after a landing posts an alarm, shows in mw status and holds the rig's open stories
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "no licence opens trade-tracker"
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then an alarm event was posted saying "grist smoke: trade-tracker FAILED"
    And mw status shows "trade-tracker FAILED"
    And mw status shows "the open stories of trade-tracker are held"
    And the open stories of "trade-tracker" are held, saying "the grist smoke of trade-tracker failed after a landing in trade-tracker"
    And the open stories of "elsewhere" are not held

  Scenario: a smoke that passes lifts the hold
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "no licence opens trade-tracker"
    And a landing in "trade-tracker" changes "grinds/price-check.json"
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then the open stories of "trade-tracker" are not held
    And mw status shows "trade-tracker ok"

  Scenario: the hold can be lifted by hand
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "no licence opens trade-tracker"
    And a landing in "trade-tracker" changes "grinds/price-check.json"
    When the hold of the smoke of "trade-tracker" is lifted
    Then the open stories of "trade-tracker" are not held

  Scenario: a kind with no example shows in mw status as a warning
    Given the app "trade-tracker" has the grind "label-read" whose answer must carry a price and a confidence
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When mw grist smoke is run for "trade-tracker"
    Then mw status shows "warning: trade-tracker/label-read: no"
    And mw status shows "trade-tracker ok"

  Scenario: a smoke the mill refuses for the daily limit is not run, and holds nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "This key has sent its 50 grist for today; send it again tomorrow."
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then mw status shows "trade-tracker not run"
    And mw status shows "not run: This key has sent its 50 grist for today"
    And the open stories of "trade-tracker" are not held
    And no alarm was posted

  Scenario: a smoke the mill refuses for the licence is not run, and holds nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "This phone's licence does not open the app named in the grist."
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then mw status shows "trade-tracker not run"
    And mw status shows "not run: This phone's licence does not open the app"
    And the open stories of "trade-tracker" are not held
    And no alarm was posted

  Scenario: a smoke that cannot reach the mill is not run, and holds nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the postern backend cannot be reached for the next grist
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then mw status shows "trade-tracker not run"
    And mw status shows "not run: the postern backend could not be reached"
    And the open stories of "trade-tracker" are not held

  Scenario: a smoke the mill cannot grind for its usage limit is not run, and holds nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill fails the next grist because "The factory cannot grind just now: it has reached its usage limit; send it again after 2026-10-16 13:00Z."
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then mw status shows "trade-tracker not run"
    And mw status shows "not run: The factory cannot grind just now: it has"
    And mw status shows "reached its usage limit"
    And the open stories of "trade-tracker" are not held
    And no alarm was posted

  Scenario: mw grist smoke says it was not run for the usage limit, and did not fail
    Given the mill fails the next grist because "The factory cannot grind just now: it has reached its usage limit; send it again after 2026-10-16 13:00Z."
    When mw grist smoke is run for "trade-tracker"
    Then the smoke was not run, saying "trade-tracker: not run: The factory cannot grind just now: it has reached its usage limit; send it again after 2026-10-16 13:00Z."

  Scenario: a grind that fails for itself still fails the smoke
    Given the mill fails the next grist because "The grind's answer did not match its schema; send it again."
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "the mill failed it: The grind's answer did not match its schema; send it again."
    And the smoke sent 1 grist

  Scenario: an example whose mill session did not finish is sent once more, and passes when it is answered
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill fails the next grist because "The grind's session did not finish; send it again."
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "low"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then the smoke sent 2 grist as "trade-tracker/price-check" with the photo "blank.jpg"
    And the landing's smoke line says "trade-tracker: ok, 1 examples"
    And the open stories of "trade-tracker" are not held
    And no alarm was posted

  Scenario: an example whose mill session did not finish twice fails, saying it was retried
    Given the mill fails the next grist because "The grind's session did not finish; send it again."
    And the mill fails the next grist because "The grind's session did not finish; send it again."
    When mw grist smoke is run for "trade-tracker"
    Then the smoke failed, saying "price-check/blank-paper: the mill failed it, and again when it was retried: The grind's session did not finish; send it again."
    And the smoke sent 2 grist

  Scenario: mw grist smoke says it was not run, and did not fail
    Given the mill refuses the next grist because "This key has sent its 50 grist for today; send it again tomorrow."
    When mw grist smoke is run for "trade-tracker"
    Then the smoke was not run, saying "trade-tracker: not run: This key has sent its 50 grist for today"

  Scenario: a refusal beside an example that is wrong is still a failure
    Given the app "trade-tracker" has the example "price-check/second" containing:
      """
      {"request": {"schemaVersion": "1"}, "expect": {"confidence": "low"}}
      """
    And the mill refuses the next grist because "This key has sent its 50 grist for today; send it again tomorrow."
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "high"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then mw status shows "trade-tracker FAILED"
    And the open stories of "trade-tracker" are held, saying "the grist smoke of trade-tracker failed"

  Scenario: a smoke that is not run leaves an earlier failure's hold in place
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the mill refuses the next grist because "no licence opens trade-tracker"
    And a landing in "trade-tracker" changes "grinds/price-check.json"
    And the mill refuses the next grist because "This key has sent its 50 grist for today; send it again tomorrow."
    When mw grist smoke is run for "trade-tracker"
    Then the open stories of "trade-tracker" are held, saying "the grist smoke of trade-tracker failed after a landing in trade-tracker"

  Scenario: a failed smoke whose every failing example is named on an open bead holds nothing
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the bead "mw-5r3p30.313" is open and names the failing example "trade-tracker/price-check/blank-paper"
    And the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then the landing's smoke line says "FAILED 1 of 1 examples, all filed on mw-5r3p30.313: not held"
    And mw status shows "filed on mw-5r3p30.313, not held"
    And the open stories of "trade-tracker" are not held
    And no alarm was posted

  Scenario: one failing example that is not named still holds the rig
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the app "trade-tracker" has the example "price-check/second" containing:
      """
      {"request": {"schemaVersion": "1"}, "expect": {"confidence": "low"}}
      """
    And the bead "mw-5r3p30.313" is open and names the failing example "trade-tracker/price-check/blank-paper"
    And the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    And the mill answers the next grist with:
      """
      {"price": null, "confidence": "high"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then the open stories of "trade-tracker" are held, saying "the grist smoke of trade-tracker failed after a landing in trade-tracker"
    And mw status shows "the open stories of trade-tracker are held"

  Scenario: closing the bead that names the examples ends it, and the next failure holds again
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the bead "mw-5r3p30.313" is open and names the failing example "trade-tracker/price-check/blank-paper"
    And the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    And a landing in "trade-tracker" changes "grinds/price-check.json"
    And the bead "mw-5r3p30.313" is closed by its fix
    And the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    When a landing in "trade-tracker" changes "grinds/price-check.json"
    Then the open stories of "trade-tracker" are held, saying "the grist smoke of trade-tracker failed after a landing in trade-tracker"

  Scenario: naming the failing examples on a bead by hand lifts the hold they put on the rig
    Given a rig "trade-tracker" checked out where the app "trade-tracker" is
    And the bead "mw-5r3p30.313" is open
    And the mill answers the next grist with:
      """
      {"price": 12.5, "confidence": "low"}
      """
    And a landing in "trade-tracker" changes "grinds/price-check.json"
    When the failing examples "price-check/blank-paper" of "trade-tracker" are named on the bead "mw-5r3p30.313"
    Then the open stories of "trade-tracker" are not held
    And mw status shows "filed on mw-5r3p30.313, not held"
