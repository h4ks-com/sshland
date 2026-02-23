# Force amd64: @opentui/core-linux-arm64 ships a libopentui.so that is missing
# the setCursorStyle symbol required by tobby. The x64 build works correctly.
FROM --platform=linux/amd64 golang:1.23-alpine AS go-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /build/wrapper-bin ./wrapper/

FROM --platform=linux/amd64 oven/bun:alpine

# Install bun packages to /opt/bun (world-readable) so the sshland user can
# access them at runtime. BUN_INSTALL must be set before any bun install call.
ENV BUN_INSTALL=/opt/bun

# Create non-root user and install tobby globally into /opt/bun.
RUN addgroup -S sshland && adduser -S -G sshland sshland && \
    mkdir -p /home/sshland /data /proxy_key && \
    chown sshland:sshland /home/sshland /data /proxy_key

COPY --from=go-builder /build/wrapper-bin /usr/local/bin/wrapper

# tobby's dist/index.js dynamically loads libopentui.so from @opentui/core-linux-x64.
# Its lockfile pins @opentui/core@0.1.75 — that is the only published version whose .so
# exports all required symbols (setCursorStyle, bufferDrawGrayscaleBuffer, etc.).
# bun resolves ^0.1.75 to a newer broken release, so we override after install.
# The published package also omits bin/tobby.js (only dist/index.js exists), leaving
# bun's binary symlink dangling — we replace it with a working shell shim.
RUN bun install -g @mattfillipe/tobby && \
    cd /opt/bun/install/global && \
    bun add @opentui/core@0.1.75 @opentui/core-linux-x64@0.1.75 && \
    printf '#!/bin/sh\nexec bun /opt/bun/install/global/node_modules/@mattfillipe/tobby/dist/index.js "$@"\n' \
        > /usr/local/bin/tobby && \
    chmod +x /usr/local/bin/tobby

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
    "--channel", "#dev"]
