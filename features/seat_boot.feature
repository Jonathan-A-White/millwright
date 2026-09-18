Feature: Booting a session into a seat
  Every story is worked by a fresh session that boots into a seat. mw assembles
  that boot: a prompt file holding the seat's charter, the seat's memory of the
  rig being worked and the story itself — and nothing else the vault holds —
  together with the command line and the environment that start the session.
  Assembling launches nothing and costs no fuel.

  Background:
    Given a vault
    And the vault holds the charter of the "builder" seat
    And the vault holds the "builder" seat's memory of the rig "millwright"
    And the vault also holds a ledger, a postmortem and a memory of another rig
    And a story "mw-gq6.6" with the path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |
    And the story is titled:
      """
      Seat boot: assemble a Builder session's priming and command line
      """
    And the story's acceptance criteria are:
      """
      make test passes. features/seat_boot.feature covers the boot file.
      """
    And the worktree "/root/.mw-worktrees/mw-gq6.6"

  Scenario: The boot file holds the charter, the rig memory and the story, in that order
    When the session that works the story is assembled for the "builder" seat
    Then the boot file holds, in this order:
      | You are the Builder of millwright |
      | Go is at /usr/local/go/bin        |
      | make test passes                  |

  Scenario: A seat with no memory of this rig still boots
    Given the "builder" seat has no memory of the rig "millwright"
    When the session that works the story is assembled for the "builder" seat
    Then the boot file holds, in this order:
      | You are the Builder of millwright |
      | make test passes                  |
    And the boot file holds none of:
      | Go is at /usr/local/go/bin |

  Scenario: Nothing else in the vault is included
    When the session that works the story is assembled for the "builder" seat
    Then the boot file holds none of:
      | mw-old worked and closed here   |
      | what went wrong last time       |
      | the fellowship rig is elsewhere |

  Scenario: The command line carries the story's model and effort
    When the session that works the story is assembled for the "builder" seat
    Then the command line carries "--model opus"
    And the command line carries "--effort high"

  Scenario: The command line primes the session and keeps its result
    When the session that works the story is assembled for the "builder" seat
    Then the command line primes the session from the vault's "runs/mw-gq6.6/boot.md"
    And the command line writes the result to the vault's "runs/mw-gq6.6/result.json"
    And the command line carries "--output-format json"

  Scenario: The session runs in the worktree, as the seat, on the story
    When the session that works the story is assembled for the "builder" seat
    Then the session runs in "/root/.mw-worktrees/mw-gq6.6"
    And the session's environment holds:
      | BEADS_ACTOR | builder@vps |
      | MW_SEAT     | builder@vps |
      | MW_STORY    | mw-gq6.6    |

  Scenario: The session is told what it is booted into
    When the session that works the story is assembled for the "builder" seat
    Then the kickoff prompt holds:
      | booted into the builder seat        |
      | your story is mw-gq6.6              |
      | Do not push, do not merge           |
      | the directory you are in            |
      | Follow the story's formula steps    |

  Scenario: A story whose title is full of punctuation is assembled safely
    Given the story is titled:
      """
      Seat boot: a "quoted" title; it holds $dollars, 'single quotes'
      and a second line
      """
    When the session that works the story is assembled for the "builder" seat
    Then the boot file holds, in this order:
      | Seat boot: a "quoted" title |
      | and a second line           |
    And the command is one shell line
    And the command line holds no newline
