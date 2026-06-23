#!/bin/bash
# install.sh - One-liner server installer for SVENOM
set -e

DOMAIN=${1:-""}
PORT=${2:-443}
HTTP_PORT=${3:-8080}

echo "[*] Downloading SVENOM installer..."
curl -sSL "https://github.com/blackopsbos/svenom/releases/latest/download/svenom" -o /tmp/svenom
chmod +x /tmp/svenom

echo "[*] Running installer..."
sudo /tmp/svenom -i ${DOMAIN:+-d "$DOMAIN"} -port "$PORT" -http "$HTTP_PORT"