# go-builder runs on the native build platform (no QEMU). The wrapper is
# cross-compiled to match whatever TARGETARCH Docker resolves (amd64 or arm64).
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS go-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /build/wrapper-bin ./wrapper/

# Final stage: native platform on the VPS (no emulation).
# Previously pinned to linux/amd64 to work around missing @opentui/core-linux-arm64
# symbols, but that caused QEMU crashes on arm64 hosts. Run natively; if the
# arm64 @opentui binaries are still broken the error will be at runtime, not build.
FROM oven/bun:alpine

# Install bun packages to /opt/bun (world-readable) so the sshland user can
# access them at runtime. BUN_INSTALL must be set before any bun install call.
ENV BUN_INSTALL=/opt/bun

# Create non-root user and install tobby globally into /opt/bun.
RUN addgroup -S sshland && adduser -S -G sshland sshland && \
    mkdir -p /home/sshland /data /proxy_key && \
    chown sshland:sshland /home/sshland /data /proxy_key

COPY --from=go-builder /build/wrapper-bin /usr/local/bin/wrapper

# Install tobby globally. This is the slow step (~6 min, 595 packages); keep it
# as its own layer so Docker caches it across iterative fixes below.
# tobby's published package omits bin/tobby.js (only dist/index.js exists), leaving
# bun's binary symlink dangling — we replace it with a working shell shim.
RUN bun install -g @mattfillipe/tobby && \
    printf '#!/bin/sh\nexec bun /opt/bun/install/global/node_modules/@mattfillipe/tobby/dist/index.js "$@"\n' \
        > /usr/local/bin/tobby && \
    chmod +x /usr/local/bin/tobby

# yoga-layout ships as a WASM singleton. bun install hoists yoga-layout to the flat
# node_modules/ but also writes a second copy into tobby's nested .bun/ cache.
# Two WASM instances → BindingError: "Expected null or instance of Node, got an instance of Node"
# Fix: delete tobby's nested copy; all modules fall through to the hoisted global one.
RUN rm -rf /opt/bun/install/global/node_modules/@mattfillipe/tobby/node_modules/.bun/*/node_modules/yoga-layout

ENV HOME=/home/sshland

USER sshland

EXPOSE 2222

ENTRYPOINT ["/usr/local/bin/wrapper", "/usr/local/bin/tobby", \
    "--db", "/data/{username}.sqlite", \
    "--server", "irc.h4ks.com", \
    "--port", "6697", \
    "--ssl", \
    "--nick", "{username}", \
    "--setup-if-not-configured", \
    "--channel", "#dev", \
    "--restrict-server", "irc.h4ks.com", \
    "--restrict-user", "{username}"]
