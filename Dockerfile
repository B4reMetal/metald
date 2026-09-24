# Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
# Modified 2026 by BareMetal
# SPDX-License-Identifier: GPL-3.0-only

FROM golang:alpine AS build
RUN apk add --no-cache build-base git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /metald ./cmd/metald

FROM alpine:3.21
# python3 + curl for the bundled tools, ffmpeg/yt-dlp for youtube/stt/musicgen,
# bubblewrap for --sandbox. Tools use only the Python standard library.
RUN apk add --no-cache ca-certificates tzdata bash python3 curl ffmpeg yt-dlp bubblewrap \
    && addgroup -S metald && adduser -S -G metald -h /data metald

COPY --from=build /metald /usr/local/bin/metald
COPY --chown=metald:metald examples /app/examples
COPY --chown=metald:metald plugins /app/plugins
COPY docker/entrypoint.sh /usr/local/bin/entrypoint
RUN chmod 0755 /usr/local/bin/entrypoint && mkdir -p /config /plugins /data \
    && chown metald:metald /config /plugins /data

# /config  config.yml: every setting, including tool settings and secrets under env:
# /plugins your own tools: any executable that answers --schema / --execute;
#          shipped tools live in /app/plugins, their shared library on PYTHONPATH
# /data    memories.db, reminders.json, ignores.json, config-overrides.json
VOLUME ["/config", "/plugins", "/data"]
WORKDIR /app
ENV METALD_CONFIG=/config/config.yml \
    METALD_DATADIR=/data \
    METALD_PLUGINLIB=/app/plugins/lib
USER metald
ENTRYPOINT ["entrypoint"]
