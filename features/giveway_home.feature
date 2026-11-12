Feature: The home knows a host gives way to the person at its screen
  A host that gives way keeps its claims (the heartbeats go on), but its sessions
  are silent for as long as the person is at the screen, an hour or an evening.
  After the freeze the host writes a note, host.<host>.gives_way, and clears it
  when it goes on. While the note stands, the alarms that judge a session quiet,
  stuck, stranded or starved pass the host's frozen stories over, and mw status
  says once, under the hosts, that the host gives way. A note nobody renewed for
  twice giveway_stale is not believed: status says so, and every alarm behaves
  as it does with no note.

  Background:
    Given the home "laptop" and the host "desktop", at 17:00Z with giveway_stale of 15 minutes
    And the story "mw-g.1" pathed to "desktop" and claimed by "mw@desktop" 90 minutes ago, its lease lapsed 85 minutes ago
    And the host "desktop" last synced 31 minutes ago

  Scenario Outline: the home's quiet alarm and the host that gives way
    Given the desktop's give-way note is <note>
    When the home reads the quiet alarm
    Then the quiet alarm <result>

    Examples:
      | note     | result                                                  |
      | standing | has no clause                                           |
      | missing  | says: desktop last synced 31 min ago                    |
      | stale    | says: desktop last synced 31 min ago                    |

  Scenario Outline: the quiet alarm on the host that gives way
    Given the desktop's give-way note is <note>
    When the desktop reads its own quiet alarm
    Then the quiet alarm <result>

    Examples:
      | note     | result                                                  |
      | standing | has no clause                                           |
      | missing  | says: mw-g.1 in progress 90 min, no mail                |
      | stale    | says: mw-g.1 in progress 90 min, no mail                |

  Scenario Outline: mw sweep on the host that gives way
    Given the desktop's give-way note is <note>
    When mw sweep reads the desktop
    Then the story "mw-g.1" <result>

    Examples:
      | note     | result                                                  |
      | standing | gets no comment, is not run=stuck and is named as frozen |
      | missing  | gets the stuck comment and is run=stuck                 |
      | stale    | gets the stuck comment and is run=stuck                 |

  Scenario Outline: mw status on the home
    Given the desktop's give-way note is <note>
    When mw status runs on the home
    Then the desktop's block <result>

    Examples:
      | note     | result                                                                            |
      | standing | says "desktop gives way since 11-13 16:20Z (1 story frozen)" and has no STRANDED line |
      | missing  | has a STRANDED line and no gives-way line                                         |
      | stale    | says "gives-way note stale" and has a STRANDED line                               |

  Scenario Outline: mw status on the host that gives way
    Given the desktop's give-way note is <note>
    When mw status runs on the desktop
    Then its own story <result>

    Examples:
      | note     | result                          |
      | standing | is not STRANDED                 |
      | missing  | is STRANDED                     |
      | stale    | is STRANDED                     |

  Scenario Outline: the starved-rig alarm and a rig the host that gives way noted
    Given the desktop noted the rig "lampas" held back for 3 hours with a story ready
    And the desktop's give-way note is <note>
    When the home judges the starved rigs
    Then <result>

    Examples:
      | note     | result                          |
      | standing | no rig is starved               |
      | missing  | the rig "lampas" is starved     |
      | stale    | the rig "lampas" is starved     |

  Scenario Outline: a claim no host names, held by the host that gives way
    Given the story "mw-g.2" pathed to no host and claimed by "mw@desktop" 90 minutes ago, its lease lapsed 85 minutes ago
    And the desktop's give-way note is <note>
    When mw doctor checks the claims with no session
    Then the stuck-claims check <result>

    Examples:
      | note     | result                |
      | standing | finds nothing         |
      | missing  | names "mw-g.2"        |
      | stale    | names "mw-g.2"        |
