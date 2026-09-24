#!/bin/sh
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

# Runs the bot with /config/config.yml, which holds every setting including
# the tools' (env:). Any arguments are passed through to metald.
set -e
if [ ! -f "${METALD_CONFIG:-/config/config.yml}" ]; then
  echo "no config at ${METALD_CONFIG:-/config/config.yml}" >&2
  echo "start from the example: docker cp <container>:/app/examples/chatbot.yml ./config/config.yml" >&2
  exit 1
fi
exec metald "$@"
