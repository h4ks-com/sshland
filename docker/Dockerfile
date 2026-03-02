FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o sshland .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates su-exec && \
    addgroup -S sshland && adduser -S -G sshland sshland && \
    mkdir -p /proxy_key && chown sshland:sshland /proxy_key

COPY --from=builder /build/sshland /usr/local/bin/sshland
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 2222
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
