FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /build/wrapper-bin ./wrapper/

FROM mattfly/hanb:main AS hanb

# Use debian-slim: hanb is dynamically linked with glibc which Alpine lacks
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/wrapper-bin /usr/local/bin/wrapper
COPY --from=hanb /hanb/hanb /usr/local/bin/hanb

RUN mkdir -p /data

EXPOSE 22

ENTRYPOINT ["/usr/local/bin/wrapper", "/usr/local/bin/hanb"]
