Feature: mw postern serve / mw postern nginx
  mw postern serve writes or replaces this host's postern config lines —
  postern_backend, postern_snapshot_path and postern_governor_key — and makes
  the snapshot's own directory, idempotent on a re-run. Given the postern
  backend's environment file, it also writes or replaces the backend's own
  POSTERN_ lines there: where it listens, the live view it serves, the mw it
  runs for a bead and for each message, the Mayor's key and the Governor's
  key as the licence's issuer. mw postern nginx ensures the /api/events
  location ahead of the general /api/ one, the /snapshot location and the
  /api upstream in the nginx site, idempotent the same way, and only reloads
  nginx once its own test passes. Both back up what they are about to change
  first, print the way back, and --dry-run touches nothing.

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

  Scenario: mw postern nginx inserts the /api/events and /snapshot locations and sets the /api upstream
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          location = /api/healthz {
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
          # mw-api-events
          location = /api/events {
              proxy_pass http://desktop.mw:8787;
              proxy_buffering off;
              proxy_cache off;
              proxy_read_timeout 1h;
              proxy_http_version 1.1;
              proxy_set_header Connection "";
          }
          location /api/ {
              proxy_pass http://desktop.mw:8787;
          }
          location = /api/healthz {
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

  Scenario: mw postern nginx with two backends writes an upstream block and points every /api location at it
    Given an nginx site file that says:
      """
      server {
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          location = /api/healthz {
              proxy_pass http://laptop.mw:8787/healthz;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with the backends "http://laptop.mw:8787" and "http://desktop.mw:8787"
    Then nginxing succeeds
    And the nginx site file holds:
      """
      # mw-api-upstream
      # A standby backend answers 503 before it does anything (story postern-standby),
      # so a request retried on the next backend, a POST too, has run nowhere twice.
      # Only the first backend takes traffic; the rest are backups, used when it fails or answers 503.
      upstream postern_api {
          server laptop.mw:8787;
          server desktop.mw:8787 backup;
      }

      server {
          # mw-api-events
          location = /api/events {
              proxy_pass http://postern_api;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
              proxy_buffering off;
              proxy_cache off;
              proxy_read_timeout 1h;
              proxy_http_version 1.1;
              proxy_set_header Connection "";
          }
          location /api/ {
              proxy_pass http://postern_api;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
          }
          location = /api/healthz {
              proxy_pass http://postern_api/healthz;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
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

  Scenario: A site file written with equal backends gets its second backend rewritten as a backup
    Given an nginx site file that says:
      """
      # mw-api-upstream
      # A standby backend answers 503 before it does anything (story postern-standby),
      # so a request retried on the next backend, a POST too, has run nowhere twice.
      upstream postern_api {
          server laptop.mw:8787;
          server desktop.mw:8787;
      }

      server {
          location /api/ {
              proxy_pass http://postern_api;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with the backends "http://laptop.mw:8787" and "http://desktop.mw:8787"
    Then nginxing succeeds
    And the nginx site file holds:
      """
      # mw-api-upstream
      # A standby backend answers 503 before it does anything (story postern-standby),
      # so a request retried on the next backend, a POST too, has run nowhere twice.
      # Only the first backend takes traffic; the rest are backups, used when it fails or answers 503.
      upstream postern_api {
          server laptop.mw:8787;
          server desktop.mw:8787 backup;
      }

      server {
          # mw-api-events
          location = /api/events {
              proxy_pass http://postern_api;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
              proxy_buffering off;
              proxy_cache off;
              proxy_read_timeout 1h;
              proxy_http_version 1.1;
              proxy_set_header Connection "";
          }
          location /api/ {
              proxy_pass http://postern_api;
              proxy_next_upstream error timeout http_503 non_idempotent; # mw-failover
              proxy_connect_timeout 2s; # mw-failover
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

  Scenario: A second run with the same two backends changes nothing more
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
    When mw postern nginx is run with the backends "http://laptop.mw:8787" and "http://desktop.mw:8787"
    And mw postern nginx is run again with the backends "http://laptop.mw:8787" and "http://desktop.mw:8787"
    Then nginxing succeeds
    And nginx was tested 1 time and reloaded 1 time
    And the nginx site file was backed up exactly 1 time

  Scenario: Going back to one backend takes the upstream block and the failover lines out again
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
    When mw postern nginx is run with the backends "http://laptop.mw:8787" and "http://desktop.mw:8787"
    And mw postern nginx is run again with the backend "http://desktop.mw:8787"
    Then nginxing succeeds
    And the nginx site file holds:
      """
      server {
          # mw-api-events
          location = /api/events {
              proxy_pass http://desktop.mw:8787;
              proxy_buffering off;
              proxy_cache off;
              proxy_read_timeout 1h;
              proxy_http_version 1.1;
              proxy_set_header Connection "";
          }
          location /api/ {
              proxy_pass http://desktop.mw:8787;
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

  Scenario: Backends that do not share a scheme cannot be one upstream
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
    When mw postern nginx is run with the backends "http://laptop.mw:8787" and "https://desktop.mw:8787"
    Then nginxing fails, saying the backends must share a scheme
    And the nginx site file is unchanged

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

  Scenario: mw postern nginx replaces its own /api/events block rather than adding a second
    Given an nginx site file that says:
      """
      server {
          # mw-api-events
          location = /api/events {
              proxy_pass http://laptop.mw:8787;
              proxy_buffering off;
          }
          location /api/ {
              proxy_pass http://laptop.mw:8787;
          }
          # mw-api end
      }
      """
    And the postern snapshot path is "/var/www/postern-snapshot/snapshot.bin"
    When mw postern nginx is run with the backend "http://10.88.0.3:8787"
    Then nginxing succeeds
    And the nginx site file holds:
      """
      server {
          # mw-api-events
          location = /api/events {
              proxy_pass http://10.88.0.3:8787;
              proxy_buffering off;
              proxy_cache off;
              proxy_read_timeout 1h;
              proxy_http_version 1.1;
              proxy_set_header Connection "";
          }
          location /api/ {
              proxy_pass http://10.88.0.3:8787;
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

  Scenario: mw postern serve writes the postern backend's environment file and makes the view directory
    When mw postern serve is run with:
      | backend       | http://10.88.0.3:8787                  |
      | snapshot-path | <home>/state/postern/snapshot.bin      |
      | governor-key  | 02governor                              |
      | env-file      | <home>/postern.env                      |
      | addr          | 10.88.0.3:8787                          |
      | view-path     | <home>/state/view/view.b64           |
      | mw            | /home/gov/.local/bin/mw                 |
      | mayor-key     | 03mayor                                 |
    Then serving succeeds
    And the postern backend's environment file says:
      """
      POSTERN_ADDR="10.88.0.3:8787"
      POSTERN_VIEW_FILE="<home>/state/view/view.b64"
      POSTERN_BEAD_CMD="/home/gov/.local/bin/mw postern bead"
      POSTERN_ON_MESSAGE="/home/gov/.local/bin/mw postern inbox --apply"
      POSTERN_MAYOR_KEY="03mayor"
      POSTERN_ISSUER_KEY="02governor"
      """
    And the postern config file says:
      """
      postern_backend = "http://10.88.0.3:8787"
      postern_snapshot_path = "<home>/state/postern/snapshot.bin"
      postern_governor_key = "02governor"
      postern_view_path = "<home>/state/view/view.b64"
      """
    And the view directory exists
    And the environment file was backed up exactly 0 times

  Scenario: mw postern serve replaces the backend's postern lines already there, leaving the rest alone
    Given a postern backend environment file that says:
      """
      POSTERN_ANCHOR=mzAnchorAddress
      POSTERN_ADDR=127.0.0.1:8787
      # the backend's own data
      POSTERN_DATA=/var/lib/postern
      """
    When mw postern serve is run with:
      | backend       | http://desktop.mw:8787                 |
      | snapshot-path | <home>/state/postern/snapshot.bin      |
      | governor-key  | 02governor                              |
      | env-file      | <home>/postern.env                      |
      | addr          | 10.88.0.3:8787                          |
      | view-path     | <home>/state/view/view.b64           |
      | mw            | /home/gov/.local/bin/mw                 |
      | mayor-key     | 03mayor                                 |
    And mw postern serve is run again with the same values
    Then serving succeeds
    And the postern backend's environment file says:
      """
      POSTERN_ANCHOR=mzAnchorAddress
      POSTERN_ADDR="10.88.0.3:8787"
      # the backend's own data
      POSTERN_DATA=/var/lib/postern
      POSTERN_VIEW_FILE="<home>/state/view/view.b64"
      POSTERN_BEAD_CMD="/home/gov/.local/bin/mw postern bead"
      POSTERN_ON_MESSAGE="/home/gov/.local/bin/mw postern inbox --apply"
      POSTERN_MAYOR_KEY="03mayor"
      POSTERN_ISSUER_KEY="02governor"
      """
    And the environment file was backed up exactly 1 time

  Scenario: --dry-run prints the backend's environment file mw postern serve would write and touches nothing
    When mw postern serve is run with --dry-run and:
      | backend       | http://desktop.mw:8787                 |
      | snapshot-path | <home>/state/postern/snapshot.bin      |
      | governor-key  | 02governor                              |
      | env-file      | <home>/postern.env                      |
      | addr          | 10.88.0.3:8787                          |
      | view-path     | <home>/state/view/view.b64           |
      | mw            | /home/gov/.local/bin/mw                 |
      | mayor-key     | 03mayor                                 |
    Then serving succeeds
    And there is no postern backend environment file
    And there is no view directory
    And the serve report shows the environment line POSTERN_ON_MESSAGE="/home/gov/.local/bin/mw postern inbox --apply"

  Scenario: mw postern serve refuses an environment file without the Mayor's key
    When mw postern serve is run with:
      | backend       | http://desktop.mw:8787                 |
      | snapshot-path | <home>/state/postern/snapshot.bin      |
      | governor-key  | 02governor                              |
      | env-file      | <home>/postern.env                      |
      | addr          | 10.88.0.3:8787                          |
      | view-path     | <home>/state/view/view.b64           |
      | mw            | /home/gov/.local/bin/mw                 |
    Then serving fails, naming the Mayor's key
    And there is no postern backend environment file
    And there is no config file
