Feature: mw toc
  A long markdown doc is easier to use with a table of contents, and a table
  that is not kept current misleads. `mw toc fix` writes one between the lines
  <!-- toc --> and <!-- /toc -->, built from the ## and ### headings with
  GitHub's anchors, before the first ## heading. A doc wants one at 4 or more
  ## headings; a doc with the markers keeps a current one whatever its count; a
  doc holding the line <!-- toc: none --> and every CHANGELOG file are left
  alone. `mw toc check` names each doc whose table is stale or missing, then the
  one command that cures them, and exits 1; when all are current it is silent.

  Scenario: Fix writes the table before the first section, and again changes nothing
    Given the toc doc "guide.md" has 5 "##" headings and 2 "###" headings and no markers
    When mw toc fixes the docs
    Then the toc doc "guide.md" has its table before the first "##" heading
    And toc printed "guide.md: TOC written"
    When mw toc fixes the docs
    Then the toc doc "guide.md" is byte-identical to what it was before that fix
    And toc printed nothing
    When mw toc checks the docs
    Then toc passes, printing nothing

  Scenario: A heading with punctuation between spaces keeps both hyphens in its anchor
    Given the toc doc "amp.md" has the "##" headings "A & B", "Plan / Notes", "Step 2: go" and "Last" and no markers
    When mw toc fixes the docs
    Then the toc doc "amp.md" has the table line "- [A & B](#a--b)"
    And the toc doc "amp.md" has the table line "- [Plan / Notes](#plan--notes)"
    And the toc doc "amp.md" has the table line "- [Step 2: go](#step-2-go)"
    When mw toc fixes the docs
    Then the toc doc "amp.md" is byte-identical to what it was before that fix

  Scenario: Check names the stale and the missing doc and cures them with one command
    Given the toc doc "stale.md" has markers around an old list and 2 "##" headings
    And the toc doc "missing.md" has 4 "##" headings and 0 "###" headings and no markers
    And the toc doc "below.md" has 3 "##" headings and 0 "###" headings and no markers
    And the toc doc "optout.md" has 6 "##" headings and 0 "###" headings and the line "<!-- toc: none -->"
    And the toc doc "current.md" has markers around a current list and 2 "##" headings
    And the toc doc "CHANGELOG.md" has 10 "##" headings and 0 "###" headings and no markers
    When mw toc checks the docs
    Then toc fails
    And toc printed exactly these lines:
      """
      stale.md: TOC stale
      missing.md: TOC missing (4 ## headings)
      mw toc fix stale.md missing.md
      """

  Scenario: A doc below the threshold is wanted once --min says so
    Given the toc doc "below.md" has 3 "##" headings and 0 "###" headings and no markers
    When mw toc checks the docs with --min 3
    Then toc fails
    And toc printed "below.md: TOC missing (3 ## headings)"
