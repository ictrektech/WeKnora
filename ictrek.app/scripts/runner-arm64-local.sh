#!/usr/bin/env bash
set -euo pipefail

# One-shot runner: build the pull package first (renders dist/staging/.env from
# the Feishu release table and validates templates), then embed ARM64 image
# archives into a non-pull package.

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

echo "=== [1/2] pull-mode package (Feishu render + validation) ==="
bash scripts/package.sh

echo "=== [2/2] embedded arm64 (non-pull) package ==="
bash scripts/package.sh --image-source local --platform arm64

echo "=== ALL DONE ==="
ls -lh dist/hybrag_*_arm64.tar
