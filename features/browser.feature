Feature: mw browser is a Laptop browser he signs into himself over VNC, and forgets it when closed
  mw browser open starts a VNC display bound to 127.0.0.1 only (no VNC password:
  only his ssh tunnel reaches it) and a Chromium on it with a fresh profile in a
  new temp directory of mode 700, its DevTools port on 127.0.0.1 only. It prints
  the one ssh -L line and the VNC address he needs. mw browser status says
  whether it is up, the URL of each tab and the CDP endpoint a script connects
  to. mw browser close stops both programs and deletes the profile, so no
  cookie, storage or password is left anywhere mw writes.

  Scenario: Open starts the display and a Chromium on a fresh private profile
    Given a fake launcher for the browser
    When the browser is opened at "https://bank.example/login"
    Then the VNC display was started listening on 127.0.0.1 only with no password
    And Chromium was started on that display at "https://bank.example/login"
    And Chromium's DevTools port is on 127.0.0.1 only
    And Chromium's profile is a new directory of mode 700
    And the browser said the line "ssh -L 5905:127.0.0.1:5905 lap"
    And the browser said the VNC address "127.0.0.1:5905"

  Scenario: Status names each tab and the CDP endpoint
    Given a fake launcher for the browser
    And the browser is opened at "https://bank.example/login"
    And Chromium shows a tab at "https://bank.example/accounts"
    When the browser's status is asked
    Then the status says the browser is up
    And the status names the tab "https://bank.example/login"
    And the status names the tab "https://bank.example/accounts"
    And the status names the CDP endpoint for chromium.connectOverCDP

  Scenario: Status of a browser that is not open says so
    Given a fake launcher for the browser
    When the browser's status is asked
    Then the status says the browser is not open

  Scenario: Close stops both programs and deletes the profile
    Given a fake launcher for the browser
    And the browser is opened at "https://bank.example/login"
    And Chromium wrote a cookie and local storage into its profile
    When the browser is closed
    Then Chromium and the VNC display were stopped
    And Chromium's profile directory is gone
    And no cookie or storage file is left anywhere mw writes
    And the browser's status says it is not open

  Scenario: A second open while one is up is refused
    Given a fake launcher for the browser
    And the browser is opened at "https://bank.example/login"
    When the browser is opened again at "https://other.example/"
    Then the second open is refused saying: already open
    And only one Chromium was started
