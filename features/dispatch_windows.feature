Feature: a host's session cap and Builder knobs follow the clock
  A [[dispatch.windows]] entry in ~/.config/mw/config.toml (from, to, tz, and any of cap,
  builder_jobs, builder_nice, room_load_per_core, room_min_free_mb) overrides the root
  values while the host's local clock in that tz is inside the window. Outside it, and for
  every knob the window does not name, the root values apply. A window may wrap midnight.
  A story already running when a window closes is not stopped: a lower cap only holds back
  new starts. (mw-gq6.429)

  Background:
    Given a host config file holding:
      """
      cap = 2
      builder_jobs = 4

      [dispatch]
      room_min_free_mb = 2048

      [[dispatch.windows]]
      from = "23:00"
      to = "06:00"
      tz = "America/New_York"
      cap = 4
      room_min_free_mb = 1024
      """

  Scenario Outline: the cap follows the clock in the window's own time zone
    When the clock reads "<local>" in America/New_York
    Then the cap in force is <cap>

    Examples:
      | local            | cap |
      | 2026-10-15 23:30 | 4   |
      | 2026-10-15 23:00 | 4   |
      | 2026-10-16 00:00 | 4   |
      | 2026-10-16 05:59 | 4   |
      | 2026-10-16 06:00 | 2   |
      | 2026-10-15 12:00 | 2   |
      | 2026-10-15 22:59 | 2   |

  Scenario: the window reads the time zone, not a fixed offset, across a change of clocks
    # 2026-11-01 is the day New York goes back to standard time (UTC-5).
    When the instant is "2026-11-01T10:59:00Z" UTC
    Then the cap in force is 4
    When the instant is "2026-11-01T11:00:00Z" UTC
    Then the cap in force is 2

  Scenario: a knob the window does not name keeps its root value, and one it names is overridden
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then builder_jobs in force is 4
    And the room needs at least 1024 MB free
    When the clock reads "2026-10-15 12:00" in America/New_York
    Then the room needs at least 2048 MB free

  Scenario: a window can carry builder_jobs and builder_nice
    Given a host config file holding:
      """
      cap = 2
      builder_jobs = 4
      builder_nice = 10

      [[dispatch.windows]]
      from = "23:00"
      to = "06:00"
      tz = "America/New_York"
      builder_jobs = 8
      builder_nice = 0
      room_load_per_core = 1.5
      """
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then builder_jobs in force is 8
    And builder_nice in force is 0
    And the room allows a load of 1.5 a core
    When the clock reads "2026-10-15 12:00" in America/New_York
    Then builder_jobs in force is 4
    And builder_nice in force is 10

  Scenario: the cap says why it is what it is
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then the cap is explained as "window to 06:00 America/New_York"
    When the clock reads "2026-10-15 12:00" in America/New_York
    Then the cap is explained as ""

  Scenario: a window may be named
    Given a host config file holding:
      """
      cap = 2

      [[dispatch.windows]]
      name = "night"
      from = "23:00"
      to = "06:00"
      tz = "America/New_York"
      cap = 4
      """
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then the cap is explained as "night window to 06:00 America/New_York"

  Scenario: the first window that holds the instant wins
    Given a host config file holding:
      """
      cap = 2

      [[dispatch.windows]]
      from = "00:00"
      to = "06:00"
      tz = "America/New_York"
      cap = 3

      [[dispatch.windows]]
      from = "23:00"
      to = "06:00"
      tz = "America/New_York"
      cap = 4
      """
    When the clock reads "2026-10-16 01:00" in America/New_York
    Then the cap in force is 3
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then the cap in force is 4

  Scenario: the environment still answers ahead of the file and its windows
    Given the environment says MW_CAP is 1
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then the cap in force is 1

  Scenario Outline: a malformed window is named by its key and the root values apply
    Given a host config file holding:
      """
      cap = 2

      [[dispatch.windows]]
      from = "<from>"
      to = "<to>"
      tz = "<tz>"
      cap = <cap>
      """
    When the clock reads "2026-10-15 23:30" in America/New_York
    Then the cap in force is 2
    And the windows are refused, naming the key <key>

    Examples:
      | from  | to    | tz               | cap | key  |
      | 25:00 | 06:00 | America/New_York | 4   | from |
      | 23:00 | 6am   | America/New_York | 4   | to   |
      | 23:00 | 06:00 | Mars/Olympus     | 4   | tz   |
      | 23:00 | 23:00 | America/New_York | 4   | to   |
      | 23:00 | 06:00 | America/New_York | 0   | cap  |

  Scenario: a well-formed config has no window problems
    Then the windows are not refused

  Scenario: a window with no from is refused, naming it
    Given a host config file holding:
      """
      cap = 2

      [[dispatch.windows]]
      to = "06:00"
      tz = "America/New_York"
      cap = 4
      """
    Then the windows are refused, naming the key from
