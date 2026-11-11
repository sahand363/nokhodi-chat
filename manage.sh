#!/usr/bin/env bash
# اتاق خواب نخودی — server management menu (TUI)
# Run:  sudo ./manage.sh
#        (or ./manage.sh when testing locally without systemd)
#
# Install dir: /opt/nokhodi-chat
#   binary:    /opt/nokhodi-chat/nokhodi-chat
#   config:    /opt/nokhodi-chat/config.json   (NEVER deleted by update)
#   messages:  /opt/nokhodi-chat/data/messages.json (NEVER deleted by update)
#   service:   nokhodi-chat.service (systemd)
set -u
REPO="sahand363/nokhodi-chat"   # <-- CHANGE THIS to your GitHub repo
INSTALL_DIR="/opt/nokhodi-chat"
BIN="$INSTALL_DIR/nokhodi-chat"
CONFIG="$INSTALL_DIR/config.json"
SERVICE="nokhodi-chat"
PORT="44044"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}$*${NC}"; }
ok()    { echo -e "${GREEN}$*${NC}"; }
warn()  { echo -e "${YELLOW}$*${NC}"; }
err()   { echo -e "${RED}$*${NC}"; }
pause() { read -rp "Press Enter to return to the menu…" _; }

server_ip() {
  hostname -I 2>/dev/null | awk '{print $1}'
}

has_systemd() { command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; }

latest_release_url() {
  # Prints the browser_download_url of the linux-amd64 tarball of the latest release.
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    grep -o '"browser_download_url": *"[^"]*linux-amd64\.tar\.gz"' | head -1 |
    sed 's/.*"browser_download_url": *"//; s/"$//'
}

download_binary() {
  local url tmp
  url="$(latest_release_url)"
  [ -n "$url" ] || { err "Could not find a release for $REPO — did you push a tag (v1.0.0) yet?"; return 1; }
  info "Downloading $url"
  tmp="$(mktemp -d)"
  curl -fsSL -o "$tmp/release.tar.gz" "$url" || { err "Download failed."; rm -rf "$tmp"; return 1; }
  tar -xzf "$tmp/release.tar.gz" -C "$tmp" || { err "Extract failed."; rm -rf "$tmp"; return 1; }
  mkdir -p "$INSTALL_DIR"
  # Keep a rollback copy of the old binary.
  [ -f "$BIN" ] && cp -f "$BIN" "$BIN.bak"
  cp -f "$tmp/nokhodi-chat" "$BIN"
  chmod +x "$BIN"
  # Ship the fresh manage.sh / service file alongside, if the release has them.
  [ -f "$tmp/manage.sh" ] && cp -f "$tmp/manage.sh" "$INSTALL_DIR/manage.sh" && chmod +x "$INSTALL_DIR/manage.sh"
  [ -f "$tmp/nokhodi-chat.service" ] && cp -f "$tmp/nokhodi-chat.service" "/etc/systemd/system/nokhodi-chat.service" 2>/dev/null || true
  rm -rf "$tmp"
  ok "Binary updated: $($BIN version 2>/dev/null || echo ok)"
}

do_install_start() {
  echo; info "== Install / Start Service =="
  if [ ! -f "$BIN" ]; then
    info "No binary yet — downloading the latest release…"
    download_binary || { pause; return; }
  fi
  mkdir -p "$INSTALL_DIR/data"
  if [ ! -f "$CONFIG" ]; then
    # First run creates config.json with admin / changeme123 — force a change now.
    (cd "$INSTALL_DIR" && "$BIN" version >/dev/null 2>&1 || true)
    info "No config yet. Starting once to generate it, then set your credentials:"
    (cd "$INSTALL_DIR" && timeout 3 "$BIN" -config "$CONFIG" >/dev/null 2>&1 || true)
    do_set_credentials
  fi
  if has_systemd; then
    [ -f /etc/systemd/system/nokhodi-chat.service ] || { cp -f "$(dirname "$0")/nokhodi-chat.service" /etc/systemd/system/nokhodi-chat.service 2>/dev/null || cp -f "$INSTALL_DIR/nokhodi-chat.service" /etc/systemd/system/nokhodi-chat.service; }
    systemctl daemon-reload
    systemctl enable --now "$SERVICE"
    systemctl status "$SERVICE" --no-pager | head -20
  else
    warn "No systemd here — starting in the background with nohup."
    pkill -f "$BIN" 2>/dev/null || true
    (cd "$INSTALL_DIR" && nohup "$BIN" -config "$CONFIG" >>"$INSTALL_DIR/chat.log" 2>&1 &)
    sleep 1
  fi
  ok "Chat is up at http://$(server_ip):$PORT"
  ok "Health: $(curl -fsS "http://127.0.0.1:$PORT/health" 2>/dev/null || echo 'check the log')"
  pause
}

do_update() {
  echo; info "== Update (keeps config.json + data/ — messages are NOT wiped) =="
  [ -f "$BIN" ] || { err "Nothing installed yet — use option 1 first."; pause; return; }
  warn "Current: $($BIN version)"
  download_binary || { pause; return; }
  if has_systemd; then
    systemctl restart "$SERVICE"
    systemctl status "$SERVICE" --no-pager | head -12
  else
    pkill -f "$BIN" 2>/dev/null || true
    (cd "$INSTALL_DIR" && nohup "$BIN" -config "$CONFIG" >>"$INSTALL_DIR/chat.log" 2>&1 &)
    sleep 1
  fi
  ok "Updated. Messages in $INSTALL_DIR/data/messages.json were left untouched."
  curl -fsS "http://127.0.0.1:$PORT/health" && echo
  pause
}

do_set_credentials() {
  echo; info "== Set / Change Access Credentials =="
  info "These are the SHARED global login for everyone (stored as a salted hash in config.json)."
  read -rp "New shared username [admin]: " user
  user="${user:-admin}"
  read -rsp "New shared password (min 6 chars): " pass; echo
  read -rsp "Repeat password: " pass2; echo
  [ "$pass" = "$pass2" ] || { err "Passwords do not match."; pause; return; }
  [ "${#pass}" -ge 6 ] || { err "Password must be at least 6 characters."; pause; return; }
  if [ ! -f "$BIN" ]; then
    err "Binary not found at $BIN — install first (option 1)."
    pause; return
  fi
  # Create a config first if none exists (binary creates the default one).
  if [ ! -f "$CONFIG" ]; then
    mkdir -p "$INSTALL_DIR"
    (cd "$INSTALL_DIR" && timeout 3 "$BIN" -config "$CONFIG" >/dev/null 2>&1 || true)
  fi
  "$BIN" passwd -config "$CONFIG" -user "$user" -pass "$pass" || { err "Failed to update credentials."; pause; return; }
  ok "Credentials updated. Tell your users the new shared login."
  if has_systemd && systemctl is-active --quiet "$SERVICE"; then
    info "Restarting the service so the new login takes effect…"
    systemctl restart "$SERVICE"
  fi
  pause
}

do_status() {
  echo; info "== Status =="
  echo "Install dir: $INSTALL_DIR"
  echo "Binary:      $([ -f "$BIN" ] && "$BIN" version || echo 'not installed')"
  echo "Config:      $([ -f "$CONFIG" ] && echo exists || echo missing)"
  echo "Messages:    $([ -f "$INSTALL_DIR/data/messages.json" ] && grep -c '"id"' "$INSTALL_DIR/data/messages.json" || echo 0) stored"
  echo "URL:         http://$(server_ip):$PORT"
  if has_systemd; then systemctl status "$SERVICE" --no-pager | head -20
  else curl -fsS "http://127.0.0.1:$PORT/health" || echo "not running"; echo; fi
  pause
}

do_uninstall() {
  echo; info "== Uninstall =="
  warn "This stops the service and removes the program files."
  read -rp "Keep messages + config (data/ and config.json)? [Y/n]: " keep
  keep="${keep:-Y}"
  if has_systemd; then
    systemctl stop "$SERVICE" 2>/dev/null || true
    systemctl disable "$SERVICE" 2>/dev/null || true
    rm -f /etc/systemd/system/nokhodi-chat.service
    systemctl daemon-reload
  else
    pkill -f "$BIN" 2>/dev/null || true
  fi
  if [[ "$keep" =~ ^[Yy]$ ]]; then
    mkdir -p "$INSTALL_DIR/keep"
    [ -f "$CONFIG" ] && cp -f "$CONFIG" "$INSTALL_DIR/keep-config.json.bak"
    rm -f "$BIN" "$BIN.bak"
    ok "Removed the program. Kept: config.json and data/ in $INSTALL_DIR"
  else
    rm -rf "$INSTALL_DIR"
    ok "Removed everything, including messages and config."
  fi
  pause
}

while true; do
  clear
  echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║   🫛  اتاق خواب نخودی — Management    ║${NC}"
  echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
  echo -e "  URL: ${CYAN}http://$(server_ip):$PORT${NC}"
  echo -e "  Dir: $INSTALL_DIR"
  echo
  echo "  1) Install / Start Service"
  echo "  2) Update (latest release — keeps messages & config)"
  echo "  3) Set / Change Access Credentials"
  echo "  4) Status"
  echo "  5) Uninstall"
  echo "  0) Exit"
  echo
  read -rp "Choose [0-5]: " choice
  case "$choice" in
    1) do_install_start ;;
    2) do_update ;;
    3) do_set_credentials ;;
    4) do_status ;;
    5) do_uninstall ;;
    0) echo "Bye 👋"; exit 0 ;;
    *) warn "Invalid choice."; sleep 1 ;;
  esac
done
