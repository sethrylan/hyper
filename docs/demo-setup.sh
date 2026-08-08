#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEMO_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/hyper-demo.XXXXXX")"
DEMO_DISCARD="$REPO_ROOT/docs/demo-discard.gif"
DEMO_FRAMES=(
  "$REPO_ROOT/docs/demo-frame-01.png"
  "$REPO_ROOT/docs/demo-frame-02.png"
  "$REPO_ROOT/docs/demo-frame-03.png"
  "$REPO_ROOT/docs/demo-frame-04.png"
  "$REPO_ROOT/docs/demo-frame-05.png"
  "$REPO_ROOT/docs/demo-frame-06.png"
  "$REPO_ROOT/docs/demo-frame-07.png"
  "$REPO_ROOT/docs/demo-frame-08.png"
)
cleanup() {
  rm -rf "$DEMO_TMP_DIR"
  rm -f "$DEMO_DISCARD"
  rm -f "${DEMO_FRAMES[@]}"
}
trap cleanup EXIT

cd "$REPO_ROOT"
mkdir -p "$DEMO_TMP_DIR/bin"
go build -o "$DEMO_TMP_DIR/bin/hyper" ./cmd/hyper
PATH="$DEMO_TMP_DIR/bin:$PATH" vhs docs/demo.tape
go run ./internal/demo/render
