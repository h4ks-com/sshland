FROM golang:1.23-alpine AS builder

WORKDIR /app

# Pinned to a specific commit for supply-chain safety.
# To update: git ls-remote https://github.com/shazow/ssh-chat HEAD
RUN apk add --no-cache git && \
    git clone https://github.com/shazow/ssh-chat.git && \
    cd ssh-chat && \
    git checkout 844e5d5b2675eb011a37ff36d02d0583a371fa29 && \
    go build -o ssh-chat ./cmd/ssh-chat

FROM alpine:3.21

RUN apk add --no-cache ca-certificates openssh-keygen && \
    addgroup -S sshland && adduser -S -G sshland sshland && \
    mkdir -p /data && chown sshland:sshland /data

COPY --from=builder /app/ssh-chat/ssh-chat /usr/local/bin/ssh-chat

RUN printf '#!/bin/sh\nssh-keygen -t ed25519 -f /tmp/host_key -N "" -q\nexec /usr/local/bin/ssh-chat --bind 0.0.0.0:2022 --identity /tmp/host_key\n' \
    > /entrypoint.sh && chmod +x /entrypoint.sh

USER sshland

EXPOSE 2022

ENTRYPOINT ["/entrypoint.sh"]
