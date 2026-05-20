#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Build and install hyper
cd "$REPO_ROOT"
go build -o hyper ./cmd/hyper
sudo install hyper /usr/local/bin/hyper
