#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PRESET="${1:-default}"

export GOPROXY=off
export GOSUMDB=off

echo "==> Configuring & building loupedeck-commander via CMake preset '${PRESET}'..."
cmake --preset "${PRESET}"
cmake --build --preset "${PRESET}"

if [[ "${PRESET}" != "race" ]]; then
    echo "==> Packaging loupedeck-commander via CPack preset '${PRESET}'..."
    cpack --preset "${PRESET}"
fi

if [[ -x "$SCRIPT_DIR/loupedeck-commander" ]]; then
    echo "✅ Built & packaged: $SCRIPT_DIR/loupedeck-commander ($("$SCRIPT_DIR/loupedeck-commander" --version))"
fi
