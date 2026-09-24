#!/bin/sh
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

# Prepares the /config volume and runs the bot. On first start with an empty
# volume it writes the example config and stops so it can be edited.
set -e
cfg="${METALD_CONFIG:-/config/config.yml}"
dir="$(dirname "$cfg")"
if ! mkdir -p "$dir/plugins" "${METALD_DATADIR:-$dir/data}" "$TMPDIR" "$XDG_CACHE_HOME" "$HOME" 2>/dev/null; then
  echo "cannot write to $dir: make the mounted folder writable by uid $(id -u)" >&2
  exit 1
fi
if [ ! -f "$cfg" ]; then
  cp /app/examples/chatbot.yml "$cfg"
  echo "wrote an example config to $cfg - edit it (server, channel, model, tools) and start again" >&2
  exit 1
fi
exec metald "$@"
