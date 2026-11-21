Feature: After a held batch, mw lands the rest together instead of one story a batch
  A batch of several held red is landed again by the batch-land job (the
  follower), at once (mw-gq6.1246). Landing each story alone cost every
  innocent story a gate of its own: 28 one-story batches, 26 gate-hours in 4.3
  days (mw-6ww.119.11.1.2). Now the suspect, the one story the batch's fixer
  named as the cause, is left out and the others land together, then it lands
  alone. With none named the stories land as two halves, and only a half held
  red again goes one story at a time. Whatever is pushed has passed the whole
  gate as merged, so the guess costs at most one more gate, never a landing.
  mw land <rig> --set-aside <story> --why "..." takes one story out of the queue
  by hand (mw-gq6.1541).

  Background:
    Given lampas lands in batches, with six stories queued, oldest first: lp-1 to lp-6

  Scenario: A batch of six held red with a fixer naming story 3 is followed by a batch of five and one of one
    Given batch 600 of lampas was held red with all six, its fixer naming "lp-3" as the cause
    When the batch-land job runs
    Then the job asked mw to land lampas, in this order:
      | lp-1,lp-2,lp-4,lp-5,lp-6 |
      | lp-3                     |

  Scenario: A batch of six held red with no story named is followed by two batches of three
    Given batch 600 of lampas was held red with all six, its fixer naming no story
    When the batch-land job runs
    Then the job asked mw to land lampas, in this order:
      | lp-1,lp-2,lp-3 |
      | lp-4,lp-5,lp-6 |

  Scenario: Only a half held red again goes one story at a time
    Given batch 600 of lampas was held red with all six, its fixer naming no story
    And the batch of lp-1,lp-2,lp-3 is held red again
    When the batch-land job runs
    Then the job asked mw to land lampas, in this order:
      | lp-1,lp-2,lp-3 |
      | lp-1           |
      | lp-2           |
      | lp-3           |
      | lp-4,lp-5,lp-6 |

  Scenario: A story set aside by hand is held with its branch kept, and the next batch is made of the rest
    When the Mayor sets lp-3 aside, saying "its spec fails on main"
    Then lp-3 is held, given back, with a comment opening "HELD: SET-ASIDE: its spec fails on main" that names its branch "mw/lp-3"
    And mw land lampas --set-aside with no why is refused
    When the batch-land job runs
    Then the job asked mw to land lampas, in this order:
      | everything queued |
