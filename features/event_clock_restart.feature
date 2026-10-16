Feature: mw events follow keeps a clock job's time across restarts
  The follower restarts on every millwright landing, and landings come less
  than a clock job's period apart. A restarted follower must not start every
  clock over: it runs each clock job from the job's own last pass in the event
  log, and a job with no pass there, or one older than its period, runs on the
  first look. A job an event springs (dispatch) is not run by its history.

  Scenario: a follower restarted every 10 minutes still runs a 15-minute clock job
    Given a 15-minute clock job on a follower that shares one event log across restarts
    When the follower is restarted 4 times, 10 minutes apart, springing at each start
    And 5 more minutes pass and the follower springs
    Then the clock job has run at least 2 times

  Scenario: a clock job that passed 5 minutes ago waits for the rest of its period
    Given a 15-minute clock job on a follower that shares one event log across restarts
    And the clock job passed 5 minutes ago
    When the follower is restarted 1 times, 10 minutes apart, springing at each start
    Then the clock job has run 0 times
    When 10 minutes pass and the follower springs
    Then the clock job has run 1 times

  Scenario: the dispatch heartbeat is not run by history at the first look
    Given a follower that shares one event log across restarts and runs dispatch on a heartbeat of an hour
    And dispatch passed 3 hours ago
    When the follower is restarted 1 times, 10 minutes apart, springing at each start
    Then dispatch has run 0 times
