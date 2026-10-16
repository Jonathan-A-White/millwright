Feature: mw cloud cap changes box_cap in one command
  box_cap lives in the home's [cloud] table, its copy in the vault and as cap in
  the tfvars the next box is made with. mw cloud cap N, N from 1 to 8, writes
  all three, says what it was and the way back, and leaves the boxes already up
  to the next cloud check. It runs on the home only.

  Scenario: On the home it writes box_cap and prints the way back
    Given the cap command runs on the home "laptop", where box_cap is 2
    When mw cloud cap 3 is run
    Then box_cap was written as 3 in each place the cloud keeps it
    And the cap command printed "box_cap is 3 (was 2)"
    And the cap command printed "The next cloud check, within a minute, sets it on each box up"
    And the cap command printed "Way back: mw cloud cap 2"

  Scenario: A host that is not home refuses and writes nothing
    Given the cap command runs on "desktop", where "laptop" is home and box_cap is 2
    When mw cloud cap 3 is run
    Then the cap command was refused saying "this host is not home"
    And nothing was written as box_cap

  Scenario Outline: A cap outside 1 to 8 is refused
    Given the cap command runs on the home "laptop", where box_cap is 2
    When mw cloud cap <n> is run
    Then the cap command was refused saying "1 to 8 sessions"
    And nothing was written as box_cap

    Examples:
      | n |
      | 0 |
      | 9 |
