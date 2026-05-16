FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /build/wrapper-bin ./wrapper/ && \
    go build -o /build/h4kshop-bin ./shop/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    addgroup -S sshland && adduser -S -G sshland -s /sbin/nologin sshland && \
    mkdir -p /proxy_key && chown sshland:sshland /proxy_key

COPY --from=builder /build/wrapper-bin /usr/local/bin/wrapper
COPY --from=builder /build/h4kshop-bin /usr/local/bin/h4kshop

USER sshland

EXPOSE 2222

ENTRYPOINT ["/usr/local/bin/wrapper", "/usr/local/bin/h4kshop"]
