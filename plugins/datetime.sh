#!/bin/bash
# Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
# Modified 2026 by BareMetal
# SPDX-License-Identifier: GPL-3.0-only

set -e

# Check if --schema argument is provided
if [[ "$1" == "--schema" ]]; then
  # Output a JSON schema to describe the tool
  # shellcheck disable=SC2016
  cat <<EOF
{
  "title": "get_current_date_with_format",
  "description": "provides the current time and date in UTC, in the specified unix date command format",
  "type": "object",
  "properties": {
    "format": {
      "type": "string",
      "description": "The format for the date. use unix date command format (e.g., +%Y-%m-%d %H:%M:%S). always include the leading + sign."
    }
  },
  "required": ["format"],
  "additionalProperties": false
}
EOF
  exit 0
fi

if [[ "$1" == "--execute" ]]; then
  # Ensure jq is available
  if ! command -v jq &> /dev/null; then
    echo "Error: jq is not installed." >&2
    exit 1
  fi

  # Extract format field from JSON
  format=$(jq -r '.format' <<< "$2")

  # Sanitize the format string
  if [[ "$format" =~ [^a-zA-Z0-9%+:/\ \-] ]]; then
    echo "Error: Invalid characters in format string." >&2
    exit 1
  fi

  # -u for UTC; -- to prevent option parsing
  date_output=$(date -u -- "$format")
  echo "$date_output"
  exit 0
fi


# if no arguments, show usage
# shellcheck disable=SC2140
echo "Usage: currentdate.sh [--schema | --execute '{"format": "+%Y-%m-%d %H:%M:%S"}']"

