# Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
# Modified 2026 by BareMetal
# SPDX-License-Identifier: GPL-3.0-only

# The Go stage runs on the build machine and cross-compiles (no cgo: the
# SQLite driver is pure Go), so multi-arch builds need no emulated compiler.
FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETOS TARGETARCH
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /metald ./cmd/metald

FROM alpine:3.21
# python3 + curl + jq for the bundled tools, ffmpeg for youtube/stt/musicgen,
# bubblewrap for --sandbox. Tools use only the Python standard library.
RUN apk add --no-cache ca-certificates tzdata bash python3 curl jq ffmpeg bubblewrap \
    && addgroup -S metald && adduser -S -H -G metald -h /config/data/home metald
# yt-dlp breaks whenever YouTube changes, so take the latest release rather than
# the distro's, verified against its published checksums. The weekly CI rebuild
# keeps it current.
RUN cd /tmp && base=https://github.com/yt-dlp/yt-dlp/releases/latest/download \
    && curl -fsSL -o yt-dlp "$base/yt-dlp" && curl -fsSL -o SUMS "$base/SHA2-256SUMS" \
    && grep ' yt-dlp$' SUMS | sha256sum -c - \
    && install -m 0755 yt-dlp /usr/local/bin/yt-dlp && rm -f yt-dlp SUMS

# Everything in the image is root-owned and read-only to the bot; it writes only
# to the /config mount. Bytecode is compiled here so Python never writes it.
COPY --from=build /metald /usr/local/bin/metald
COPY examples /app/examples
COPY plugins /app/plugins
COPY docker/entrypoint.sh /usr/local/bin/entrypoint
RUN chmod 0755 /usr/local/bin/entrypoint \
    && python3 -m compileall -q /app/plugins \
    && mkdir -p /config && chown metald:metald /config \
    && ln -s /config/plugins /app/custom-plugins

# One volume: /config/config.yml, /config/plugins/ (your own plugins) and
# /config/data/ (memories, reminders, ignores, overrides, logs, temp files).
VOLUME ["/config"]
WORKDIR /app
ENV METALD_CONFIG=/config/config.yml \
    METALD_DATADIR=/config/data \
    METALD_PLUGINLIB=/app/plugins/lib \
    PYTHONPATH=/app/plugins/lib \
    PYTHONDONTWRITEBYTECODE=1 \
    METALD_TOOL_LOG=/config/data/tool-errors.log \
    TMPDIR=/config/data/tmp \
    XDG_CACHE_HOME=/config/data/cache \
    HOME=/config/data/home
USER metald
ENTRYPOINT ["entrypoint"]
