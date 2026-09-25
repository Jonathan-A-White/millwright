Feature: mw postern serve / mw postern nginx
  mw postern serve writes or replaces this host's postern config lines —
  postern_backend, postern_snapshot_path and postern_governor_key — and makes
  the snapshot's own directory, idempotent on a re-run. mw postern nginx
  ensures the /snapshot location and the /api upstream in the nginx site,
  idempotent the same way, and only reloads nginx once its own test passes.
  Both back up what they are about to change first, print the way back, and
  --dry-run touches nothing.

  Background:
    Given a throwaway home for the postern hand commands

  Scenario: mw postern serve writes a fresh config file and makes the snapshot directory
    When mw postern serve is run with:
      | backend       | http://desktop.mw:8787                |
      | snapshot-path | <home>/state/postern/snapshot.bin     |
      | governor-key  | abc123                                 |
    Then serving succeeds
    And the postern config file says:
      """
      postern_backend = "http://desktop.mw:8787"
      postern_snapshot_path = "<home>/state/postern/snapshot.bin"
      postern_governor_key = "abc123"
      """
    And the snapshot directory exists
    And the config file was backed up exactly 0 times

  Scenario: mw postern serve replaces postern lines already there, leaving the rest alone
    Given a postern config file that says:
      """
      vault = "/somewhere"
      host  = "vps"
      postern_backend = "http://laptop.mw:8787"
      cap = 1
      """
    When mw postern serve is run with:
      | backend       | http://desktop.mw:8787                |
      | snapshot-path | <home>/state/postern/snapshot.bin     |
      | governor-key  | abc123                                 |
    Then serving succeeds
    And the postern config file says:
      """
      vault = "/somewhere"
      host  = "vps"
      postern_backend = "http://desktop.mw:8787"
      cap = 1
      postern_snapshot_path = "<home>/state/postern/snapshot.bin"
      postern_governor_key = "abc123"
      """
    And the config file was backed up exactly 1 time

  Scenario: A second run of mw postern serve with the same values changes nothing more
    Given a postern config file that says:
      """
      vault = "/somewhere"
      host  = "vps"
      """
    When mw postern serve is run with:
      | backend       | http://desktop.mw:8787                |
      | snapshot-path | <home>/state/postern/snapshot.bin     |
      | governor-key  | abc123                                 |
    And mw postern serve is run again with the same values
    Then serving succeeds
    And the config file was backed up exactly 1 time

  Scenario: --dry-run prints what mw postern serve would do and touches nothing
    When mw postern serve is run with --dry-run and:
      | backend       | http://desktop.mw:8787                |
      | snapshot-path | <home>/state/postern/snapshot.bin     |
      | governor-key  | abc123                                 |
    Then serving succeeds
    And there is no config file
    And there is no snapshot directory
    And the config file was backed up exactly 0 times
    And the serve report says it would write postern_backend

  Scenario: mw postern nginx inserts the /snapshot location and sets the /api upstream
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          location /api/healthz {
              proxy_pass http://laptop.mw:8787/healthz;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with the backend "http://desktop.mw:8787"
    Then nginxing succeeds
    And the nginx site file holds:
      """
      server {
          location /api/ {
              proxy_pass http://desktop.mw:8787;
          }
          location /api/healthz {
              proxy_pass http://desktop.mw:8787/healthz;
          }
          # mw-api end
          # mw-snapshot
          location = /snapshot {
              alias /var/www/postern-snapshot/snapshot.bin;
              add_header Cache-Control "no-store" always;
              add_header X-Content-Type-Options "nosniff" always;
              default_type application/octet-stream;
          }
      }
      """
    And nginx was tested 1 time and reloaded 1 time
    And the nginx site file was backed up exactly 1 time

  Scenario: A second run of mw postern nginx with the same backend changes nothing more
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with the backend "http://desktop.mw:8787"
    And mw postern nginx is run again with the backend "http://desktop.mw:8787"
    Then nginxing succeeds
    And nginx was tested 1 time and reloaded 1 time
    And the nginx site file was backed up exactly 1 time

  Scenario: --dry-run prints what mw postern nginx would do and touches nothing
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with --dry-run and the backend "http://desktop.mw:8787"
    Then nginxing succeeds
    And nginx was tested 0 times and reloaded 0 times
    And the nginx site file is unchanged

  Scenario: A failing nginx -t restores the backup and reloads nothing
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    And the fake nginx test will fail, saying "nginx: [emerg] bad thing"
    When mw postern nginx is run with the backend "http://desktop.mw:8787"
    Then nginxing fails, saying nginx -t failed
    And the nginx site file is unchanged
    And nginx was tested 1 time and reloaded 0 times
