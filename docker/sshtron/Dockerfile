FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
RUN git clone https://github.com/zachlatta/sshtron.git && \
    cd sshtron && go build -o sshtron .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && \
    addgroup -S sshland && adduser -S -G sshland -s /sbin/nologin sshland
COPY --from=builder /app/sshtron/sshtron /usr/local/bin/sshtron
USER sshland
EXPOSE 2022
ENTRYPOINT ["/usr/local/bin/sshtron"]
