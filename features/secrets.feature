Feature: mw secrets
  The factory's tokens are kept in the vault, in secrets.enc.yaml, encrypted with
  sops to the age recipient the vault's .sops.yaml names. Only the home holds the
  age key that opens them (~/.config/mw/age.key, mode 600). mw secrets put reads
  a value from stdin, never from its arguments; mw secrets get writes it to
  stdout only when stdout is not a terminal; mw secrets list prints names only.
  No value is ever printed, logged or written anywhere but the encrypted file.

  Each scenario makes a throwaway age key and a temp vault, and runs the real
  sops and age-keygen: it is skipped where they are not installed
  (contrib/install-sops-age.sh installs them).

  Background:
    Given a temp vault whose .sops.yaml names a throwaway age key

  Scenario: A value put is the value got back
    When a fresh value is put as the secret "vultr_api_token"
    And the secret "vultr_api_token" is got into a pipe
    Then what was got is the value put as "vultr_api_token", and nothing more
    And mw secrets printed no value

  Scenario: A second secret keeps the first, and a name put again takes its new value
    Given a fresh value is put as the secret "vultr_api_token"
    And a fresh value is put as the secret "github_token"
    When a fresh value is put as the secret "vultr_api_token"
    And the secret "vultr_api_token" is got into a pipe
    Then what was got is the value put as "vultr_api_token", and nothing more
    When the secret "github_token" is got into a pipe
    Then what was got is the value put as "github_token", and nothing more

  Scenario: list shows the names only
    Given a fresh value is put as the secret "vultr_api_token"
    And a fresh value is put as the secret "github_token"
    When the secrets are listed
    Then the list is exactly:
      """
      github_token
      vultr_api_token
      """
    And mw secrets printed no value

  Scenario: get refuses a terminal, and writes nothing to it
    Given a fresh value is put as the secret "vultr_api_token"
    When the secret "vultr_api_token" is got onto a terminal
    Then the get is refused, saying "terminal"
    And nothing was written to the terminal

  Scenario: The file on disk holds no value in the clear
    Given a fresh value is put as the secret "vultr_api_token"
    And a fresh value is put as the secret "github_token"
    Then grep finds no value put in secrets.enc.yaml
    And secrets.enc.yaml names the throwaway age recipient

  Scenario: One trailing newline on stdin is not part of the value
    When the value "tok-with-newline" and a newline is put as the secret "github_token"
    And the secret "github_token" is got into a pipe
    Then what was got is "tok-with-newline", and nothing more

  Scenario: An empty value is refused, and nothing is written
    When an empty value is put as the secret "github_token"
    Then the put is refused, saying "empty"
    And the vault holds no secrets.enc.yaml

  Scenario: A name that is not a plain word is refused
    When a fresh value is put as the secret "../escape"
    Then the put is refused, saying "name"
    And the vault holds no secrets.enc.yaml

  Scenario: get of a name never put says so
    Given a fresh value is put as the secret "vultr_api_token"
    When the secret "github_token" is got into a pipe
    Then the get is refused, saying "no secret named github_token"
    And nothing was written to the pipe
