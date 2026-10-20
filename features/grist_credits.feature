Feature: The mill bills a grist to an Anthropic API key while it has credit, and to the subscription otherwise
  With [grist] api_credits on and a key file the user alone can read
  (api_key_file, default ~/.config/mw/anthropic-api.key, mode 0600), the mill
  gives the claude session of a grind ANTHROPIC_API_KEY in its environment,
  cold or warm: in the environment alone, never in an argument, a log, an event
  or a line of the record. Otherwise the session has no ANTHROPIC_API_KEY and
  uses the subscription login. Every line of the record says what its grind was
  billed to: "api" or "subscription". A grind billed to the key that fails with
  'Credit balance is too low', a spend limit, or a key the API refuses, writes
  a spent mark in credits.json and runs the same grist again at once on the
  subscription: one answer, one line, one count against the day. One
  grist-credits-spent event is left for the Mayor, in the normal lane: nothing
  pages the Governor. While the mark stands every grist uses the subscription;
  the first grist a day after it tries the key again, and getting through
  clears the mark. Warm sessions started on the key are retired when the mark
  is written.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the mill runs its grinds through a stand-in claude

  Scenario: With api_credits on and a key file the grind's session is given the key in its environment alone
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And no session was without it
    And no argument any session was given holds the key
    And the mill's record holds one line, billed to "api"
    And the key is in no line of the record, no event and no report

  Scenario: With api_credits off the session has no key, even if the mill's own environment holds one
    Given api_credits is off, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the mill's own environment holds the ANTHROPIC_API_KEY "sk-ant-from-the-shell"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And no session was given a key
    And the mill's record holds one line, billed to "subscription"

  Scenario: A key file others can read is refused with a plain note and the subscription is used
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0644
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And the mill's report says the key file is not used because its mode is 0644
    And the mill's record holds one line, billed to "subscription"
    And the key is in no line of the record, no event and no report

  Scenario: A warm session is started on the key when it is in use
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the mill keeps a warm pool of real sessions
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And a warm session waits, started with the key "sk-ant-test-key" in its environment
    And no argument any session was given holds the key

  Scenario: A warm session is started without a key when api_credits is off
    Given api_credits is off, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the mill's own environment holds the ANTHROPIC_API_KEY "sk-ant-from-the-shell"
    And the mill keeps a warm pool of real sessions
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then a warm session waits, started with no ANTHROPIC_API_KEY

  Scenario: 'Credit balance is too low' runs the same grist again on the subscription, once
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API answers a grind billed to the key with "Credit balance is too low"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 sessions ran: the key was tried, then the same grist without it
    And 1 answer was delivered
    And the mill's record holds one line, billed to "subscription"
    And the grist counts once against the day's limit
    And the spent mark was written
    And one grist-credits-spent event was left for the Mayor saying "credits spent", in the normal lane
    And the key is in no line of the record, no event and no report

  Scenario: A spend limit is a spent key too, whatever the case of its words
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API answers a grind billed to the key with "You have reached your workspace's monthly SPEND LIMIT"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 sessions ran: the key was tried, then the same grist without it
    And the spent mark was written

  Scenario: A key the API refuses falls back at once, and the event says so
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API refuses the key
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 sessions ran: the key was tried, then the same grist without it
    And the mill's record holds one line, billed to "subscription"
    And one grist-credits-spent event was left for the Mayor saying "key refused", in the normal lane

  Scenario: While the key is spent every grist uses the subscription
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the key was found spent 2 hours ago
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 2, refused 0, failed 0, and left 0 waiting
    And 2 sessions were given no ANTHROPIC_API_KEY
    And no session was given a key
    And the spent mark was kept

  Scenario: The first grist a day after the mark tries the key again, and getting through clears the mark
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the key was found spent 25 hours ago
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And the mill's record holds one line, billed to "api"
    And the spent mark was cleared

  Scenario: Warm sessions started on the key are retired when the mark is written
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the mill keeps a warm pool of real sessions
    And the API answers a grind billed to the key with "Credit balance is too low"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the spent mark was written
    And no warm session started with the key is still running
    And a warm session waits, started with no ANTHROPIC_API_KEY
