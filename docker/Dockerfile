FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o sshland .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /build/sshland /usr/local/bin/sshland

EXPOSE 22

ENTRYPOINT ["/usr/local/bin/sshland"]
