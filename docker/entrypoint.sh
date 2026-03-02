#!/bin/sh
set -e

# Create data subdirectories as root before dropping to the sshland user.
# Needed when /data is a host bind-mount owned by root (e.g. Coolify deployments).
NICKS_DIR="${NICKS_DIR:-/data/nicks}"
IDENTITIES_DIR="${IDENTITIES_DIR:-/data/identities}"

mkdir -p "$NICKS_DIR" "$IDENTITIES_DIR"
chown sshland:sshland "$NICKS_DIR" "$IDENTITIES_DIR"

exec su-exec sshland /usr/local/bin/sshland "$@"
