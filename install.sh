#!/bin/bash
# install-attacker.sh - Instal vghost untuk attacker (sekali saja)
set -e

echo "[*] Downloading vghost binary..."
curl -fsSL "https://github.com/blackopsbos/svenom/releases/latest/download/vghost" -o /usr/local/bin/vghost
chmod +x /usr/local/bin/vghost
echo "[+] vghost terinstal. Gunakan: vghost --Sk <secret> -d <domain> --connect"