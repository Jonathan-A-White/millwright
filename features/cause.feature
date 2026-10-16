Feature: mw cause
  When a story fails, the Mayor names why: the cause is one of four classes,
  code, test, tooling or process. `mw cause <id> <class> "<why>"` records it
  on the story as metadata, so that causes can be counted, and as a comment
  beginning CAUSE, so that a person reading the story finds it. With --fix it
  names the bead that mends the cause. It touches nothing else of the story.

  Background:
    Given the cause story "mw-x.1" is claimed and has been started 2 times
    And the cause story "mw-y.2" is open

  Scenario: A cause is recorded as metadata and one comment
    When mw records the cause of "mw-x.1" as tooling, saying "the gate's node was missing"
    Then recording the cause succeeds
    And the story "mw-x.1" has the cause metadata "cause" as "tooling"
    And the story "mw-x.1" has the cause metadata "cause_why" as "the gate's node was missing"
    And the story "mw-x.1" has no cause metadata "cause_fix"
    And the story "mw-x.1" has exactly 1 comment, beginning "CAUSE (tooling):"
    And the cause comment on "mw-x.1" says "the gate's node was missing"

  # mw-gq6.450: the class is kept for the attempt it was named for, so that mw
  # retry counts only the attempts that were the story's own code.
  Scenario: The cause is kept as the cause of the story's latest attempt
    When mw records the cause of "mw-x.1" as tooling, saying "the gate's node was missing"
    Then recording the cause succeeds
    And the story "mw-x.1" has the cause metadata "attempt_causes" as "2:tooling"

  Scenario: A fix names the bead that mends the cause
    When mw records the cause of "mw-x.1" as code, saying "the retry lost the claim", fixed by "mw-y.2"
    Then recording the cause succeeds
    And the story "mw-x.1" has the cause metadata "cause_fix" as "mw-y.2"
    And the cause comment on "mw-x.1" says "mw-y.2"

  Scenario: The story is otherwise untouched
    When mw records the cause of "mw-x.1" as process, saying "the story was too big"
    Then recording the cause succeeds
    And the story "mw-x.1" is still in progress, claimed, and started 2 times
    And the cause story "mw-y.2" has no metadata and no comment

  Scenario: A class outside the four is refused, naming the four
    When mw records the cause of "mw-x.1" as flaky, saying "it just broke"
    Then recording the cause is refused, saying: code, test, tooling, process
    And nothing was written by the cause command

  Scenario: An unknown story is refused
    When mw records the cause of "mw-nope.9" as test, saying "no such story"
    Then recording the cause is refused, saying: mw-nope.9
    And nothing was written by the cause command

  Scenario: A cause with no why is refused
    When mw records the cause of "mw-x.1" as test, saying "  "
    Then recording the cause is refused, saying: why
    And nothing was written by the cause command

  Scenario: A fix that is not a bead is refused
    When mw records the cause of "mw-x.1" as code, saying "a bug", fixed by "mw-nope.9"
    Then recording the cause is refused, saying: mw-nope.9
    And nothing was written by the cause command

  Scenario: Naming the cause twice replaces the class and adds a second comment
    When mw records the cause of "mw-x.1" as test, saying "the test was flaky", fixed by "mw-y.2"
    And mw records the cause of "mw-x.1" as code, saying "it was the code after all"
    Then recording the cause succeeds
    And the story "mw-x.1" has the cause metadata "cause" as "code"
    And the story "mw-x.1" has the cause metadata "cause_why" as "it was the code after all"
    And the story "mw-x.1" has no cause metadata "cause_fix"
    And the story "mw-x.1" has exactly 2 comments, beginning "CAUSE ("
