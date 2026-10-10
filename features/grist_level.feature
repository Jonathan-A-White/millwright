Feature: the home's checkout of an app's rig follows every landing, and the app is smoked
  The mill reads an app's grinds from the home's checkout of the app's rig
  ([grist-apps]). A story landed from another host fast-forwards only that host's
  checkout, so the mill went on grinding with the old grinds/ while the live app
  sent the new fields, and the grist smoke of the landing ran only on the host
  that landed it (mw-gq6.346). The follower on the home sees every landing as an
  event: when the rig is one named in [grist-apps], it fetches, fast-forwards the
  checkout onto the rig's main and makes the grist smoke of what the landing
  changed, as a landing on the home does. A checkout that is dirty, on another
  branch or has commits of its own is left exactly as it is, and mw status and the
  Mayor's mail say why. A rig that is not in [grist-apps] is never touched.

  Background:
    Given the home names the app "lampas" in [grist-apps], checked out in the rig "lampas"
    And the rig "notes", which is not in [grist-apps], is checked out on the home
    And the home's follower is running on the home

  Scenario: A landing from another host moves the checkout and the app is smoked
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is at the rig's main
    And the grist smoke of "lampas" was made once
    And the home's mw status shows "lampas ok"

  Scenario: A landing that changes nothing the app's grist reads moves the checkout and smokes nothing
    When another host lands a story on "lampas" that changes "README.md"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is at the rig's main
    And the grist smoke of "lampas" was made 0 times

  Scenario: A dirty checkout is refused with the reason, and nothing is reset
    Given the home's checkout of "lampas" has an uncommitted change
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is still at its old commit, with its uncommitted change
    And the grist smoke of "lampas" was made 0 times
    And the home's mw status shows "lampas checkout left alone"
    And the home's mw status shows "uncommitted"
    And the Mayor has 1 mail about the checkout of "lampas"

  Scenario: A refusal that stands is said once to the Mayor, not at every landing
    Given the home's checkout of "lampas" has an uncommitted change
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    And another host lands a story on "lampas" that changes "grinds/other.json"
    And the home's follower runs a cycle
    Then the Mayor has 1 mail about the checkout of "lampas"

  Scenario: A checkout with commits of its own is not touched
    Given the home's checkout of "lampas" has a commit of its own
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is still at its own commit
    And the grist smoke of "lampas" was made 0 times
    And the home's mw status shows "lampas checkout left alone"
    And the Mayor has 1 mail about the checkout of "lampas"

  Scenario: Once the checkout is clean the next landing moves it and the refusal is gone
    Given the home's checkout of "lampas" has an uncommitted change
    And another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    When the home's checkout of "lampas" is cleaned
    And another host lands a story on "lampas" that changes "grinds/other.json"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is at the rig's main
    And the grist smoke of "lampas" was made once
    And the home's mw status does not show "left alone"

  Scenario: A rig that is not in [grist-apps] is untouched
    When another host lands a story on "notes" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the home's checkout of "notes" is still at its old commit
    And the grist smoke of "lampas" was made 0 times
    And the Mayor has 0 mail about the checkout of "lampas"

  Scenario: A landing the home made itself, which left the checkout level, is not smoked again
    Given the home has already brought its checkout of "lampas" level
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the grist smoke of "lampas" was made 0 times

  Scenario: A host that is not home does nothing
    Given the home's follower is running on a host that is not home
    When another host lands a story on "lampas" that changes "grinds/bible-talk.json"
    And the home's follower runs a cycle
    Then the home's checkout of "lampas" is still at its old commit
