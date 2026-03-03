FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /build/wrapper-bin ./wrapper/

# Pin to digest once confirmed stable:
# docker pull mattfly/hanb:main && docker inspect --format='{{index .RepoDigests 0}}' mattfly/hanb:main
# Then replace `:main` with `@sha256:<digest>`
FROM mattfly/hanb:main AS hanb

# Use debian-slim: hanb is dynamically linked with glibc which Alpine lacks
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -r sshland && useradd --no-log-init -r -g sshland -s /usr/sbin/nologin sshland && \
    mkdir -p /data /proxy_key && chown sshland:sshland /data /proxy_key

COPY --from=builder /build/wrapper-bin /usr/local/bin/wrapper
COPY --from=hanb /hanb/hanb /usr/local/bin/hanb

USER sshland

EXPOSE 2222

ENTRYPOINT ["/usr/local/bin/wrapper", "/usr/local/bin/hanb"]
