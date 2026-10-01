Feature: mw prompt
  The Mayor saves a prompt to the postern backend, from a draft in the vault,
  so the Governor can run it by name from the app. mw prompt save keeps it;
  mw prompt run checks the call against the prompt's signature and prints the
  body with its options filled in, then the facts the answer is made of,
  read at no cost in tokens: what waits on the Governor, what landed and is
  not yet VERIFIED with its HOW TO CHECK IT, the open cards, the open demos
  and the hands steps that wait.

  Scenario: Saving a prompt keeps its name, summary, signature and body
    When the Mayor saves the prompt "top5" summarised "The five things to do next" with the options "count:int=5, since:string:required" and the body "Name the top <count> things since <since>."
    Then saving succeeds
    And the backend holds the prompt "top5" summarised "The five things to do next" with the signature "count:int=5, since:string:required"

  Scenario: An option that is not name:type=default is refused and nothing is saved
    When the Mayor saves the prompt "top5" summarised "x" with the options "count" and the body "b"
    Then saving is refused, saying "is not <flag>:<type>=<default>"
    And the backend holds no prompt "top5"

  Scenario: The list gives each prompt's name, summary and signature one to a line
    Given the backend holds the prompt "top5" summarised "The five things to do next" with the signature "count:int=5" and the body "<count>"
    And the backend holds the prompt "plain" summarised "Say hello" with the signature "" and the body "hello"
    When the Mayor lists the prompts
    Then the output has the line "plain\tSay hello\tno options"
    And the output has the line "top5\tThe five things to do next\t--count:int=5"

  Scenario: Show prints the whole prompt
    Given the backend holds the prompt "top5" summarised "The five things to do next" with the signature "count:int=5" and the body "Name the top <count>."
    When the Mayor shows the prompt "top5"
    Then the output has the line "Name the top <count>."
    And the output has the line "signature: --count:int=5"

  Scenario: Showing a prompt that is not saved says so
    When the Mayor shows the prompt "nope"
    Then running is refused, saying "no saved prompt /nope"

  Scenario: Run prints the call, the filled body and the five facts, one of each
    Given the backend holds the prompt "top5" summarised "The five things to do next" with the signature "count:int=5" and the body "Name the top <count> things."
    And a story "mw-f.1" waiting for the Governor
    And a story "mw-f.2" landed with the closing comment "Done. HOW TO CHECK IT, for the Governor: 1. Open the app. 2. Tap Needs you."
    And a story "mw-f.3" with an open card asking "Which way?"
    And a story "mw-f.4" labelled for a demo
    And a story "mw-f.5" with the hands step "linger" on "desktop" that has not run
    When the Mayor runs the prompt "top5" with "--count 3"
    Then running succeeds
    And the output has the line "PROMPT /top5 --count 3"
    And the output has the line "Name the top 3 things."
    And the output has the section "WAITING FOR THE GOVERNOR" naming "mw-f.1"
    And the output has the section "LANDED NOT VERIFIED" naming "mw-f.2"
    And the output has the section "LANDED NOT VERIFIED" naming "1. Open the app. 2. Tap Needs you."
    And the output has the section "OPEN CARDS" naming "mw-f.3"
    And the output has the section "OPEN CARDS" naming "Which way?"
    And the output has the section "OPEN DEMOS" naming "mw-f.4"
    And the output has the section "HANDS STEPS THAT WAIT" naming "mw-f.5"

  Scenario: An option left out takes its default
    Given the backend holds the prompt "top5" summarised "x" with the signature "count:int=5" and the body "Top <count>."
    When the Mayor runs the prompt "top5" with ""
    Then running succeeds
    And the output has the line "PROMPT /top5"
    And the output has the line "Top 5."

  Scenario: A section with nothing in it says none
    Given the backend holds the prompt "plain" summarised "x" with the signature "" and the body "hello"
    When the Mayor runs the prompt "plain" with ""
    Then running succeeds
    And the output has the section "OPEN CARDS" naming "none"

  Scenario: A story that is already running or has its hands step done is not a fact
    Given the backend holds the prompt "plain" summarised "x" with the signature "" and the body "hello"
    And a story "mw-f.5" with the hands step "linger" on "desktop" that has run
    When the Mayor runs the prompt "plain" with ""
    Then the output has the section "HANDS STEPS THAT WAIT" naming "none"

  Scenario: An option the signature does not name is refused, naming the signature
    Given the backend holds the prompt "top5" summarised "x" with the signature "count:int=5" and the body "Top <count>."
    When the Mayor runs the prompt "top5" with "--bogus 1"
    Then running is refused, saying "--bogus is not an option"
    And running is refused, saying "its signature is --count:int=5"
    And the output is empty

  Scenario: A value that is not of its type is refused, naming the signature
    Given the backend holds the prompt "top5" summarised "x" with the signature "count:int=5" and the body "Top <count>."
    When the Mayor runs the prompt "top5" with "--count many"
    Then running is refused, saying "is not an int"
    And running is refused, saying "its signature is --count:int=5"

  Scenario: A required option left out is refused
    Given the backend holds the prompt "since" summarised "x" with the signature "since:string:required" and the body "Since <since>."
    When the Mayor runs the prompt "since" with ""
    Then running is refused, saying "--since is required"
