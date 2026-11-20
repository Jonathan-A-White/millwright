Feature: The mill bills each grist by its sender's mark: the Governor's Max by default, the API credit alone for a person marked API only
  The postern backend keeps a billing mark on each registered person, "max"
  (the default) or "api" (the Key screen's 'API only' switch), and says it in
  the person lookup. The Governor's own grists, and those of any sender not
  marked API only (no person, an unmarked person, a lookup that failed), are
  billed to his subscription, whatever the credits say (mw-gq6.1518). A grist
  of a person marked API only is billed to the Anthropic API key alone, whatever
  [grist] api_credits says: with a key file the user alone can read
  (api_key_file, default ~/.config/mw/anthropic-api.key, mode 0600), the mill
  gives its claude session ANTHROPIC_API_KEY in its environment, cold or warm:
  in the environment alone, never in an argument, a log, an event or a line of
  the record, and with no subscription token beside it. Every line of the
  record says what its grind was billed to: "api" or "subscription", or "none"
  for a grist that ran no grind (mw-gq6.1520). An API-only person's grist is
  never billed to the subscription: with no usable key file it is refused, "The
  tutor is resting until the factory's credit is topped up; send it again
  later.", and with the credit spent it is refused saying when the credit
  renews ([grist] credits_renew_day, the 17th): "The tutor is out of credits
  until Oct 17; send it again then." Neither refusal is a use of the day, and an
  API-only person may send 30 grists a day ([grist] api_daily_limit). A grind
  billed to the key that fails with 'Credit balance is too low', a spend limit,
  or a key the API refuses, writes a spent mark in credits.json and leaves one
  grist-credits-spent event for the Mayor, in the normal lane: nothing pages the
  Governor. Only the factory's own grist, the grist smoke's key's, is billed as
  api_credits says and runs again at once on the subscription when the key
  fails: one answer, one line, one count against the day. The first grist a day
  after the mark tries the key again, and getting through clears the mark. Warm
  sessions started on the key are retired when the mark is written.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the mill runs its grinds through a stand-in claude

  Scenario: A person marked API only has their grind's session given the key in its environment alone
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And no session was without it
    And no argument any session was given holds the key
    And the mill's record holds one line, billed to "api"
    And the key is in no line of the record, no event and no report

  Scenario: The Governor's grist is billed to his subscription even with api_credits on and a usable key
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And no session was given a key
    And the mill's record holds one line, billed to "subscription"

  Scenario: With api_credits off the Governor's session has no key, even if the mill's own environment holds one
    Given api_credits is off, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the mill's own environment holds the ANTHROPIC_API_KEY "sk-ant-from-the-shell"
    And the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And no session was given a key
    And the mill's record holds one line, billed to "subscription"

  Scenario: With api_credits off a person marked API only is still billed to the key, never the subscription
    Given api_credits is off, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And no session was without it
    And the mill's record holds one line, billed to "api"

  Scenario: A person marked API only with no key file is not ground: the tutor is resting
    Given api_credits is on, and there is no key file
    And the mill's own environment holds the ANTHROPIC_API_KEY "sk-ant-from-the-shell"
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And no session was given a key
    And the answer says "refused" because "The tutor is resting until the factory's credit is topped up; send it again later."
    And the mill's record holds one line, billed to "none"

  Scenario Outline: A sender not marked API only is billed to the subscription, even with api_credits on and a usable key
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And <who>
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And no session was given a key
    And the mill's record holds one line, billed to "subscription"

    Examples:
      | who                                                |
      | the phone's licence belongs to a person marked "max" |
      | the phone's licence belongs to a person marked ""    |
      | the phone's licence belongs to no person           |
      | the person lookup fails                            |

  Scenario: A key file others can read is refused with a plain note, and an API-only person's grist rests
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0644
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And the answer says "refused" because "The tutor is resting until the factory's credit is topped up; send it again later."
    And the mill's report says the key file is not used because its mode is 0644
    And the key is in no line of the record, no event and no report

  Scenario: A warm session is started on the key when it is in use
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
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
    And the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then a warm session waits, started with no ANTHROPIC_API_KEY

  Scenario: 'Credit balance is too low' answers an API-only person's grist out of credits until the renewal day, and never runs it on the subscription
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API answers a grind billed to the key with "Credit balance is too low"
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And no session was without it
    And the answer says "refused" because "The tutor is out of credits until Oct 17; send it again then."
    And the mill's record holds one line, billed to "api"
    And the spent mark was written
    And one grist-credits-spent event was left for the Mayor saying "credits spent", in the normal lane
    And the key is in no line of the record, no event and no report

  Scenario: 'Credit balance is too low' runs the factory's own grist again on the subscription, once
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API answers a grind billed to the key with "Credit balance is too low"
    And the phone is the key of the grist smoke
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
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And no session was without it
    And the spent mark was written

  Scenario: A key the API refuses falls back at once for the factory's own grist, and the event says so
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the API refuses the key
    And the phone is the key of the grist smoke
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 2 sessions ran: the key was tried, then the same grist without it
    And the mill's record holds one line, billed to "subscription"
    And one grist-credits-spent event was left for the Mayor saying "key refused", in the normal lane

  Scenario: While the key is spent the Governor's grist uses the subscription and an API-only person's is out of credits
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the key was found spent 2 hours ago
    And the phone's licence belongs to a person marked "api"
    And the Governor's key sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 1, failed 0, and left 0 waiting
    And 1 session was given no ANTHROPIC_API_KEY
    And no session was given a key
    And the spent mark was kept
    And the phone's line of the record was refused because "The tutor is out of credits until Oct 17; send it again then."

  Scenario: The first grist a day after the mark tries the key again, and getting through clears the mark
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
    And the key was found spent 25 hours ago
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And 1 session was given the key "sk-ant-test-key" in its environment
    And the mill's record holds one line, billed to "api"
    And the spent mark was cleared

  Scenario: Warm sessions started on the key are retired when the mark is written
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
    And the mill keeps a warm pool of real sessions
    And the API answers a grind billed to the key with "Credit balance is too low"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And the spent mark was written
    And no warm session started with the key is still running
    And a warm session waits, started with no ANTHROPIC_API_KEY

  Scenario: The out-of-credits answer names the next renewal day, rolling over the month's end
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And today is 2026-12-20 on the mill's clock
    And the key was found spent 2 hours ago
    And the phone's licence belongs to a person marked "api"
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And no session was given a key
    And the answer says "refused" because "The tutor is out of credits until Jan 17; send it again then."
    And the mill's record holds one line, billed to "none"

  Scenario: A person marked API only may send 30 grists a day, and the 31st is told to come back tomorrow
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "api"
    And the phone has already sent 30 grist today
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And no session was given a key
    And the answer says "refused" because "This key has sent its 30 grist for today; send it again tomorrow."

  Scenario: A sender on the Max keeps the home's daily limit
    Given api_credits is on, and the key file holds the key "sk-ant-test-key" at mode 0600
    And the phone's licence belongs to a person marked "max"
    And the phone has already sent 30 grist today
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And the newest line of the mill's record is billed to "subscription"
