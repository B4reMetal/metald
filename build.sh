#!/bin/bash -x
# Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
# Modified 2026 by BareMetal
# SPDX-License-Identifier: GPL-3.0-only

if [ -z "$TAG" ]; then
  TAG="metald:dev"
fi

echo "building with tag: $TAG"

docker build . -t "$TAG"