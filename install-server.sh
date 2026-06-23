#!/bin/bash
set -e

DOMAIN=""
PORT="443"
HTTP_PORT="8080"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--domain) DOMAIN="$2"; shift 2;;
    -port|--port) PORT="$2"; shift 2;;
    -http|--http-port) HTTP_PORT="$2"; shift 2;;
    *) shift;;
  esac
done

if [[ -z "$DOMAIN" ]]; then
  echo "Usage: $0 -d <domain> [--port 443] [--http-port 8080]"
  exit 1
fi

echo "[*] Downloading SVENOM server binary..."
curl -fsSL "https://github.com/blackopsbos/svenom/releases/latest/download/svenom" -o /tmp/svenom
chmod +x /tmp/svenom

echo "[*] Running installer..."
sudo /tmp/svenom -i -d "$DOMAIN" -port "$PORT" -http "$HTTP_PORT"