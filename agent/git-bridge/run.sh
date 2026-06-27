#!/usr/bin/env bash
# Wrapper for launchd — sets up NODE_PATH and runs the server.
cd "$(dirname "$0")"
export NODE_PATH="$(pwd)/node_modules"
exec /opt/homebrew/bin/node --import tsx/esm src/server.ts
