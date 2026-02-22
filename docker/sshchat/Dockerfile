FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git && \
    git clone https://github.com/shazow/ssh-chat.git && \
    cd ssh-chat && \
    go build -o ssh-chat ./cmd/ssh-chat

FROM alpine:3.21

RUN apk add --no-cache ca-certificates openssh-keygen

COPY --from=builder /app/ssh-chat/ssh-chat /usr/local/bin/ssh-chat

RUN mkdir -p /data && \
    printf '#!/bin/sh\n[ -f /data/host_key ] || ssh-keygen -t ed25519 -f /data/host_key -N ""\nexec /usr/local/bin/ssh-chat --bind 0.0.0.0:2022 --identity /data/host_key\n' \
    > /entrypoint.sh && chmod +x /entrypoint.sh

EXPOSE 2022

ENTRYPOINT ["/entrypoint.sh"]
