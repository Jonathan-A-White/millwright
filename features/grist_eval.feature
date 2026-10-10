Feature: mw grist eval, trying a grind on photos with known answers
  The Governor tunes an app's grind by running it over a directory of photos
  that each have a file beside them listing what is really in the photo. mw
  grist eval runs the grind once per photo on each model named, through the
  same grinder the mill uses, and scores each answer against the file: the
  items it found, the ones it missed, the ones it made up, and the Fuel spent.
  It never touches the backend, the mill's counts or the dispatch cap, and the
  photos and answers never enter a bead or the vault.

  Background:
    Given a rig checkout holding the grind "sweep" on the model "haiku" at "low" effort
    And the factory allows the models "haiku,sonnet"
    And the photo "drawer.jpg" expecting:
      """
      scissors
      tape
      glue
      """
    And the photo "shelf.png" expecting:
      """
      Container: Shoebox
        candles
        string
      matches
      """

  Scenario: Two photos on two models give four rows scored against what is expected
    Given the model "haiku" answers for "drawer.jpg" with the items "scissors, tape, hammer" at a cost of 0.010 USD
    And the model "sonnet" answers for "drawer.jpg" with the items "Scissors,  TAPE, glue" at a cost of 0.040 USD
    And the model "haiku" answers for "shelf.png" with the items "matches" at a cost of 0.010 USD
    And the model "sonnet" answers for "shelf.png" with the items "matches" at a cost of 0.040 USD
    When mw grist eval runs on the models "haiku,sonnet"
    Then the eval has 4 rows
    And the row for "drawer.jpg" on "haiku" has 2 hits of 3, misses "glue", extras "hammer" and 0 unsure
    And the row for "drawer.jpg" on "sonnet" has 3 hits of 3, misses "", extras "" and 0 unsure
    And the row for "drawer.jpg" on "haiku" spent 0.010 USD and 8 tokens in and 512 out
    And the summary for "sonnet" has 2 photos, 4 hits of 7 and a mean cost of 0.040 USD
    And the printed table names "drawer.jpg", "shelf.png", "haiku" and "sonnet"

  Scenario: A Container line's items are expected too, and a Container's answer counts
    Given the model "haiku" answers for "shelf.png" with this answer:
      """
      {"schemaVersion":"1.1","responseType":"sweep-result","placeName":"Shelf",
       "items":[{"name":"matches"}],
       "containers":[{"name":"Shoebox","items":[{"name":"Candles"},{"name":"string"},{"name":"ribbon"}]}]}
      """
    And the model "haiku" answers for "drawer.jpg" with the items "scissors, tape, glue" at a cost of 0.010 USD
    When mw grist eval runs on the models "haiku"
    Then the eval has 2 rows
    And the row for "shelf.png" on "haiku" has 4 hits of 4, misses "", extras "ribbon" and 0 unsure

  Scenario: An unsure item counts as answered and in the unsure column
    Given the model "haiku" answers for "drawer.jpg" with this answer:
      """
      {"schemaVersion":"1.1","responseType":"sweep-result","placeName":"Drawer",
       "items":[{"name":"scissors"},{"name":"tape","unsure":true},{"name":"glue"}]}
      """
    When mw grist eval runs on the models "haiku"
    Then the row for "drawer.jpg" on "haiku" has 3 hits of 3, misses "", extras "" and 1 unsure

  Scenario: An answer that lists lines[].text is scored on those texts
    Given the model "haiku" answers for "drawer.jpg" with this answer:
      """
      {"schemaVersion":"1.1","responseType":"receipt-result","placeName":"Hardware","items":[],
       "lines":[{"text":"Scissors"},{"text":"  TAPE "},{"text":"hammer"}]}
      """
    When mw grist eval runs on the models "haiku"
    Then the row for "drawer.jpg" on "haiku" has 2 hits of 3, misses "glue", extras "hammer" and 0 unsure
    And the row for "drawer.jpg" on "haiku" does not say "no names to score"

  Scenario: An answer with neither items nor lines says there are no names to score
    Given the model "haiku" answers for "drawer.jpg" with this answer:
      """
      {"schemaVersion":"1.1","responseType":"receipt-result","placeName":"Hardware","items":[],"total":12.5}
      """
    When mw grist eval runs on the models "haiku"
    Then the row for "drawer.jpg" on "haiku" says "no names to score" and counts no misses
    And the printed table names "drawer.jpg", "haiku", "no names to score" and "hits"

  Scenario: A photo with no answer file is skipped with a note
    Given a photo "attic.webp" with no file listing what is expected
    And the model "haiku" answers for "drawer.jpg" with the items "scissors" at a cost of 0.010 USD
    And the model "haiku" answers for "shelf.png" with the items "matches" at a cost of 0.010 USD
    When mw grist eval runs on the models "haiku"
    Then the eval has 2 rows
    And the eval's notes say "attic.webp"
    And no grind was given "attic.webp"

  Scenario: A model the factory does not allow is refused before any grind runs
    When mw grist eval runs on the models "haiku,opus"
    Then the eval is refused, saying "opus"
    And no eval grind was run
    And nothing was written under the out directory

  Scenario: Without --models the grind file's own model is used
    Given the model "haiku" answers for "drawer.jpg" with the items "scissors" at a cost of 0.010 USD
    And the model "haiku" answers for "shelf.png" with the items "matches" at a cost of 0.010 USD
    When mw grist eval runs on the grind file's own model
    Then the eval has 2 rows
    And every grind ran on "haiku" at "low" effort

  Scenario: A grind that fails is a row with the error and counts of zero
    Given the model "haiku" answers for "drawer.jpg" with the items "scissors, tape, glue" at a cost of 0.010 USD
    And the model "haiku" gives no answer for "shelf.png"
    When mw grist eval runs on the models "haiku"
    Then the eval has 2 rows
    And the row for "shelf.png" on "haiku" failed with an error and counts of zero
    And the summary for "haiku" has 1 failed

  Scenario: Every grind runs in a private directory that is gone afterwards
    Given the model "haiku" answers for "drawer.jpg" with the items "scissors" at a cost of 0.010 USD
    And the model "haiku" answers for "shelf.png" with the items "matches" at a cost of 0.010 USD
    When mw grist eval runs on the models "haiku"
    Then every grind ran alone in its own private directory, now gone
    And each grind was given the preamble, the grind's instructions and the photo by its full path

  Scenario: The rows and the tables are written under the out directory
    Given the model "haiku" answers for "drawer.jpg" with the items "scissors" at a cost of 0.010 USD
    And the model "haiku" answers for "shelf.png" with the items "matches" at a cost of 0.010 USD
    When mw grist eval runs on the models "haiku"
    Then the out directory holds an eval jsonl with 2 lines carrying the full answers
    And the out directory holds an eval md that names the photos by file name only
