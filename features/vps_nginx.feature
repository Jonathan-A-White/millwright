Feature: The VPS nginx guard and the standby
  The guard reads the postern_api upstream in the VPS's nginx file. The home's
  server must be the one plain server, and every other one `backup`. When a
  [backend.<rig>] table names a vps_health, the standby's host:port must also
  be there as a `backup` server, or the front door has nowhere to fall back to
  when the home sleeps. With no vps_health the guard asks nothing of the standby.

  Scenario: The standby's backup line is in the upstream
    Given the home is "laptop"
    And the VPS's upstream holds "server laptop.mw:8787;", "server desktop.mw:8787 backup;" and "server 10.88.0.1:8787 backup;"
    And a backend names the standby's vps_health "http://10.88.0.1:8787/healthz"
    When the VPS nginx guard reads the upstream
    Then the VPS nginx guard finds the upstream ok
    And the VPS nginx guard's line is "VPS NGINX ok (home first, 2 backup)"

  Scenario: The standby's backup line is missing from the upstream
    Given the home is "laptop"
    And the VPS's upstream holds "server laptop.mw:8787;" and "server desktop.mw:8787 backup;"
    And a backend names the standby's vps_health "http://10.88.0.1:8787/healthz"
    When the VPS nginx guard reads the upstream
    Then the VPS nginx guard finds a fault
    And the VPS nginx guard's fault says "standby 10.88.0.1:8787 not in the upstream"
    And the VPS nginx guard's fault says "add `server 10.88.0.1:8787 backup;` as its last line"
    And the VPS nginx guard's fault says "mw postern nginx --backend ... --backend http://10.88.0.1:8787"
    And the VPS nginx guard's line is "VPS NGINX FAULT: standby 10.88.0.1:8787 not in the upstream"

  Scenario: With no vps_health the upstream is judged as it always was
    Given the home is "laptop"
    And the VPS's upstream holds "server laptop.mw:8787;" and "server desktop.mw:8787 backup;"
    When the VPS nginx guard reads the upstream
    Then the VPS nginx guard finds the upstream ok
    And the VPS nginx guard's line is "VPS NGINX ok (home first, 1 backup)"

  Scenario: The standby listed as a plain server is not its backup line
    Given the home is "laptop"
    And the VPS's upstream holds "server laptop.mw:8787;" and "server 10.88.0.1:8787;"
    And a backend names the standby's vps_health "http://10.88.0.1:8787/healthz"
    When the VPS nginx guard reads the upstream
    Then the VPS nginx guard finds a fault
    And the VPS nginx guard's fault says "2 servers take traffic"
