FROM golang:1.24-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /build/wrapper-bin ./wrapper/

RUN apk add --no-cache git && \
    git clone https://github.com/h4ks-com/h4kmally-tui.git && \
    cd h4kmally-tui && \
    CGO_ENABLED=0 GOOS=linux go build -o /build/tui-agar ./cmd/tui-agar

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    addgroup -S sshland && adduser -S -G sshland -s /sbin/nologin sshland

COPY --from=builder /build/wrapper-bin /usr/local/bin/wrapper
COPY --from=builder /build/tui-agar /usr/local/bin/tui-agar

USER sshland

EXPOSE 2222

ENTRYPOINT ["wrapper", "/usr/local/bin/tui-agar", \
  "-name", "{username}", \
  "-server", "wss://api.sigmally.h4ks.com/ws/"]
