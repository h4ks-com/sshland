#!/bin/sh
set -e

# Both /data and /proxy_key can be root-owned when created by Coolify or
# docker compose before the image runs. Chown them as root before dropping
# to the sshland user so the app can write host_key, nicks/, identities/,
# and the proxy key that wrapper containers read back.
chown sshland:sshland /data /proxy_key

exec su-exec sshland /usr/local/bin/sshland "$@"
