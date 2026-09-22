# A bare base for scripts/check-bootstrap.sh: nothing preinstalled but a
# non-root user, so scripts/install.sh has to prove it can take a machine
# from nothing to a built mw itself, exactly as a fresh host would. Every
# package install.sh installs (git, tmux, jq, ripgrep, python3, curl,
# ca-certificates, gh, make, Go, bd) is left for it to install.
FROM debian:12-slim
RUN useradd -m -s /bin/sh tester
