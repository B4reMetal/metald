#!/bin/sh
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

# Loads /config/.env into the environment (tool credentials), then runs the
# bot. Any arguments are passed through to metald.
set -e
if [ ! -f "${METALD_CONFIG:-/config/config.yml}" ]; then
  echo "no config at ${METALD_CONFIG:-/config/config.yml}" >&2
  echo "start from the example: docker cp <container>:/app/examples/chatbot.yml ./config/config.yml" >&2
  echo "or:                    docker run --rm --entrypoint cat metald:dev /app/examples/chatbot.yml > config/config.yml" >&2
  exit 1
fi
if [ -f /config/.env ]; then
  set -a
  . /config/.env
  set +a
fi
exec metald "$@"
