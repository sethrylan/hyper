#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEMO_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/hyper-demo.XXXXXX")"
cleanup() {
  rm -rf "$DEMO_TMP_DIR"
}
trap cleanup EXIT

cd "$REPO_ROOT"
mkdir -p "$DEMO_TMP_DIR/bin"
go build -o "$DEMO_TMP_DIR/bin/hyper" ./cmd/hyper
PATH="$DEMO_TMP_DIR/bin:$PATH" vhs docs/demo.tape
