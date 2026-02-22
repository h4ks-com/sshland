FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

RUN git clone https://github.com/davidcroda/sshwordle.git && \
    cd sshwordle && \
    go build -o sshwordle .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

# Copy the whole repo directory so runtime word-list files are present alongside the binary
COPY --from=builder /app/sshwordle/ /app/sshwordle/

WORKDIR /app/sshwordle
EXPOSE 1337

ENTRYPOINT ["/app/sshwordle/sshwordle", "-host", "0.0.0.0", "-port", "1337"]
