#!/bin/sh
exec /usr/local/bin/wrapper /usr/local/bin/tobby \
    --db ":memory:" \
    --server "${IRC_SERVER}" \
    --port "${IRC_PORT}" \
    --ssl \
    --nick "{username}" \
    --setup-if-not-configured \
    --channel "${IRC_CHANNEL}" \
    --restrict-server "${IRC_SERVER}" \
    --restrict-port "${IRC_PORT}" \
    --restrict-user "{username}" \
    --do-not-store-password
