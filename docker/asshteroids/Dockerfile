FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
RUN git clone https://github.com/tomz197/asshteroids.git && \
    cd asshteroids && go build -o asshteroids ./cmd/ssh

FROM alpine:3.21
RUN addgroup -S sshland && adduser -S -G sshland -s /sbin/nologin sshland && \
    mkdir -p /var/lib/asshteroids && chown sshland:sshland /var/lib/asshteroids
COPY --from=builder /app/asshteroids/asshteroids /usr/local/bin/asshteroids
USER sshland
EXPOSE 2222
ENTRYPOINT ["/usr/local/bin/asshteroids"]
