#!/usr/bin/env bash
# 🎯T1062: host-side permit checker, not a separate-UID security broker.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./cmd/restartauth "$@"
