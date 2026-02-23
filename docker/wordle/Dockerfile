FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Pinned to a specific commit for supply-chain safety.
# To update: git ls-remote https://github.com/davidcroda/sshwordle HEAD
RUN git clone https://github.com/davidcroda/sshwordle.git && \
    cd sshwordle && \
    git checkout d638128b155dec4acb187c4116f6d6075260dadc && \
    go build -o sshwordle .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    addgroup -S sshland && adduser -S -G sshland -h /home/sshland sshland && \
    mkdir -p /home/sshland && chown sshland:sshland /home/sshland

# Copy the whole repo directory so runtime word-list files are present alongside the binary
COPY --from=builder /app/sshwordle/ /app/sshwordle/

RUN chown -R sshland:sshland /app

ENV HOME=/home/sshland

USER sshland

WORKDIR /app/sshwordle
EXPOSE 1337

ENTRYPOINT ["/app/sshwordle/sshwordle", "-host", "0.0.0.0", "-port", "1337"]
