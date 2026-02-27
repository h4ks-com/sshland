# Build the wrapper natively and cross-compile to amd64 — avoids QEMU crashes
# when building on arm64 hosts. The final stage is amd64 because @opentui/core-linux-arm64
# is missing symbols required by tobby; the x64 build works correctly.
FROM golang:1.23-alpine AS go-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN GOARCH=amd64 GOOS=linux go build -o /build/wrapper-bin ./wrapper/

FROM --platform=linux/amd64 oven/bun:alpine

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
# This step is fast and lives in a separate layer so it can be iterated without
# re-downloading the 595-package tobby install above.
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
