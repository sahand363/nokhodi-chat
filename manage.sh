#!/usr/bin/env bash
# اتاق نخودی — server management menu (TUI)
# Run:  sudo ./manage.sh
#        (or ./manage.sh when testing locally without systemd)
#
# Install dir: /opt/nokhodi-chat
#   binary:    /opt/nokhodi-chat/nokhodi-chat
#   config:    /opt/nokhodi-chat/config.json   (NEVER deleted by update)
#   messages:  /opt/nokhodi-chat/data/room_*.json (NEVER deleted by update)
#   rooms:     /opt/nokhodi-chat/data/rooms.json (NEVER deleted by update)
#   service:   nokhodi-chat.service (systemd)
set -u
# --- Input fix (v2.0.1) ---
# When this menu is launched from a pipe (e.g. `curl ... | sudo bash`
# ending in `exec manage.sh`), stdin is the pipe, not your keyboard.
# `read` then gets EOF instantly, choice is empty, the menu prints
# "Invalid choice.", clears, and loops forever — the flicker every
# few seconds. So: take input from the real terminal (/dev/tty)
# instead, and if there is no terminal at all, exit instead of looping.
if [ ! -t 0 ]; then
  # Test-open /dev/tty in a subshell first: merely existing is not
  # enough — opening it fails when there is no controlling terminal.
  if (exec 0</dev/tty) 2>/dev/null; then
    exec 0</dev/tty
  else
    echo "No terminal input available — run 'nokhodichat' from a normal SSH prompt, not from a pipe."
    exit 1
  fi
fi
REPO="sahand363/nokhodi-chat"
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

installed_version() {
  if [ -x "$BIN" ]; then "$BIN" version 2>/dev/null || echo "unknown"; else echo "not installed"; fi
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
  # Keep the `nokhodichat` shortcut installed, too.
  if [ -f "$tmp/nokhodichat" ]; then cp -f "$tmp/nokhodichat" /usr/local/bin/nokhodichat 2>/dev/null || true; fi
  [ -f /usr/local/bin/nokhodichat ] && chmod +x /usr/local/bin/nokhodichat 2>/dev/null || true
  # Rename the title in an existing config, if it still has the old name.
  [ -f "$CONFIG" ] && sed -i 's/اتاق خواب نخودی/اتاق نخودی/g' "$CONFIG" 2>/dev/null || true
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
    # First run creates config.json with user changeme123 / OP changeme-op123.
    (cd "$INSTALL_DIR" && "$BIN" version >/dev/null 2>&1 || true)
    info "No config yet. Starting once to generate it, then set your passwords:"
    (cd "$INSTALL_DIR" && timeout 3 "$BIN" -config "$CONFIG" >/dev/null 2>&1 || true)
    do_set_credentials user
    do_set_credentials op
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
  ok "Updated. Messages and rooms in $INSTALL_DIR/data/ were left untouched."
  curl -fsS "http://127.0.0.1:$PORT/health" && echo
  pause
}

do_set_credentials() {
  local role="${1:-user}"
  if [ "$role" = "op" ]; then
    echo; info "== Set / Change OP (admin) Password =="
    info "Only you use this password. It makes you the OP in the chat."
  else
    role="user"
    echo; info "== Set / Change User Password (everyone else) =="
    info "Everyone else logs in with this password. Stored as a salted hash in config.json."
  fi
  read -rsp "New $role password (min 6 chars): " pass; echo
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
  "$BIN" passwd -config "$CONFIG" -role "$role" -pass "$pass" || { err "Failed to update credentials."; pause; return; }
  ok "$role password updated."
  if [ "$role" = "user" ]; then ok "Tell your users the new shared login."; fi
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
  echo "Messages:    $(grep -h -c '"id"' "$INSTALL_DIR"/data/room_*.json 2>/dev/null | awk '{s+=$1} END {print s+0}') stored"
  echo "Rooms:       $([ -f "$INSTALL_DIR/data/rooms.json" ] && grep -c '"id"' "$INSTALL_DIR/data/rooms.json" || echo 0)"
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
  # The `nokhodichat` shortcut goes away with the program, either way.
  rm -f /usr/local/bin/nokhodichat
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
  echo -e "${GREEN}║   🫛  اتاق نخودی — Management    ║${NC}"
  echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
  echo -e "  URL: ${CYAN}http://$(server_ip):$PORT${NC}"
  echo -e "  Dir: $INSTALL_DIR"
  echo -e "  Version: $(installed_version)"
  echo
  echo "  1) Install / Start Service"
  echo "  2) Update (latest release — keeps messages, rooms & config)"
  echo "  3) Set User Password (everyone else)"
  echo "  4) Set OP Password (only you — the admin)"
  echo "  5) Status"
  echo "  6) Uninstall"
  echo "  0) Exit"
  echo
  if ! read -rp "Choose [0-6]: " choice; then
    echo; echo "Input closed — exiting menu."
    exit 0
  fi
  case "$choice" in
    1) do_install_start ;;
    2) do_update ;;
    3) do_set_credentials user ;;
    4) do_set_credentials op ;;
    5) do_status ;;
    6) do_uninstall ;;
    0) echo "Bye 👋"; exit 0 ;;
    *) warn "Invalid choice."; sleep 1 ;;
  esac
done
