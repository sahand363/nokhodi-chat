#!/usr/bin/env bash
# اتاق خواب نخودی — one-line installer for Ubuntu
#
#   curl -sSL https://raw.githubusercontent.com/YOUR_GITHUB_USERNAME/nokhodi-chat/main/install.sh | sudo bash
#
# What it does (no compilation on the server):
#   1. Downloads the pre-built linux/amd64 binary from the latest GitHub release
#   2. Installs it to /opt/nokhodi-chat, creates config.json + data/
#   3. Installs + starts the systemd service on port 44044
#   4. Opens the management menu (./manage.sh) so you can set the shared login
set -euo pipefail
REPO="YOUR_GITHUB_USERNAME/nokhodi-chat"   # <-- CHANGE THIS to your GitHub repo
INSTALL_DIR="/opt/nokhodi-chat"
PORT="44044"

echo "🫛 Installing اتاق خواب نخودی …"
[ "$(id -u)" = "0" ] || { echo "Please run as root / with sudo."; exit 1; }
command -v curl >/dev/null || { apt-get update -qq && apt-get install -y -qq curl ca-certificates; }

URL="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
  grep -o '"browser_download_url": *"[^"]*linux-amd64\.tar\.gz"' | head -1 |
  sed 's/.*"browser_download_url": *"//; s/"$//')"
[ -n "$URL" ] || { echo "ERROR: no release found for $REPO — push a tag like v1.0.0 first."; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL -o "$TMP/release.tar.gz" "$URL"
tar -xzf "$TMP/release.tar.gz" -C "$TMP"
mkdir -p "$INSTALL_DIR/data"
cp -f "$TMP/nokhodi-chat" "$INSTALL_DIR/nokhodi-chat"
chmod +x "$INSTALL_DIR/nokhodi-chat"
[ -f "$TMP/manage.sh" ] && cp -f "$TMP/manage.sh" "$INSTALL_DIR/manage.sh" && chmod +x "$INSTALL_DIR/manage.sh"
[ -f "$TMP/nokhodi-chat.service" ] && cp -f "$TMP/nokhodi-chat.service" /etc/systemd/system/nokhodi-chat.service

# First run generates config.json (default admin / changeme123) if missing.
if [ ! -f "$INSTALL_DIR/config.json" ]; then
  (cd "$INSTALL_DIR" && timeout 3 ./nokhodi-chat -config config.json >/dev/null 2>&1 || true)
fi

systemctl daemon-reload
systemctl enable --now nokhodi-chat
sleep 1
IP="$(hostname -I | awk '{print $1}')"
echo
echo "✅ Done! Open:  http://$IP:$PORT"
echo "   Default login: admin / changeme123  — change it now in the menu (option 3)."
echo "   Manage later with:  sudo /opt/nokhodi-chat/manage.sh"
echo
exec "$INSTALL_DIR/manage.sh"
