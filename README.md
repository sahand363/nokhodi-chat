# 🫛 اتاق نخودی — Self-Hosted Chat (v2)

A complete, modern, self-hosted web chat in **Go (standard library only — no external dependencies)**, built for an Ubuntu server. Mobile-first Persian RTL UI (Vazirmatn), real-time messages over SSE, persistent JSON storage, and a clean CLI management menu.

## v2 — mIRC-style rooms, with one OP

- **Rooms:** `#general` by default, plus rooms the OP creates — `#music`, `#games`, …
- **Two entry passwords:** an **OP password** (only Mark) and a **user password** (everyone else). The password you log in with decides your role. Login is password-only, then your display name + maths captcha.
- **Only the OP can:** create / delete / rename rooms, set or change a room password, set a room's self-destruct timer, delete any message, kick / ban a name, and change the OP and user passwords (in the app's 👑 OP panel, or in the `nokhodichat` menu).
- **Room passwords:** a room can be open, or locked with its own password.
- **Hidden rooms:** the OP can hide a room from the list. A hidden room is only reachable via its secret link (`/?room=<id>&invite=<token>`), it still needs the room password, and the server returns **404 — no name, no messages, no members — to anyone without link + password**. The OP sees hidden rooms with a 👁‍🗨 badge and can copy the secret link from the OP panel.
- **Self-destruct:** each room has a timer — never, 10 minutes, 1 hour, 24 hours, or 7 days. Expired messages are deleted by the server for everyone, every 30 seconds.
- **Online users:** who is in the current room, live. The OP can ban a name from there.
- **Commands:** `/join #name`, `/list`, `/me does something`, `/help`. Edit/delete is via the 🗑 button (your own messages, or any message if you're OP).

## What you get

| Piece | File |
|---|---|
| Go backend (OP/user auth, rooms, TTL, SSE, JSON store) | `main.go`, `server.go`, `auth.go`, `rooms.go`, `store.go`, `sse.go` |
| Frontend (login → name + captcha → rooms) | `web/index.html`, `web/style.css`, `web/app.js` |
| Management menu | `manage.sh` (`nokhodichat` in SSH) |
| One-line installer | `install.sh` |
| systemd service | `nokhodi-chat.service` |
| CI/CD (builds the binary on GitHub) | `.github/workflows/release.yml` |
| Config schema | `config.example.json` |

## 1. Publish it (once)

```bash
git add . && git commit -m "nokhodi chat v2" && git push origin main
git tag v2.0.0
git push origin v2.0.0
# Check Actions → a Release appears with nokhodi-chat-linux-amd64.tar.gz
```

## 2. Install / update on Ubuntu (one line)

```bash
curl -sSL https://raw.githubusercontent.com/sahand363/nokhodi-chat/main/install.sh | sudo bash
```

It downloads the pre-built binary, installs it to `/opt/nokhodi-chat`, creates the systemd service, installs the `nokhodichat` command, starts it, and opens the menu.

Then open: **http://&lt;server-ip&gt;:44044**

> If port 44044 is blocked: `sudo ufw allow 44044/tcp`

For later updates, just type `nokhodichat` and choose **2) Update** — it keeps `config.json`, `data/rooms.json` and all room messages.

## 3. Management menu

```bash
nokhodichat
```

```
🫛  اتاق نخودی — Management
  URL: http://<server-ip>:44044
  Dir: /opt/nokhodi-chat
  Version: nokhodi-chat 2.0.1

1) Install / Start Service
2) Update (latest release — keeps messages, rooms & config)
3) Set User Password (everyone else)
4) Set OP Password (only you — the admin)
5) Status
6) Uninstall
0) Exit
```

The menu header shows the installed version, so you can see at a glance whether you are on v2. Type `nokhodichat` in any SSH session to open it. If the menu ever prints "Invalid choice." without you typing anything, it was started from a pipe (stdin is not your keyboard) — v2.0.1 reads from the real terminal instead and exits cleanly instead of looping. Uninstall without the menu:

```bash
sudo systemctl stop nokhodi-chat; sudo systemctl disable nokhodi-chat
sudo rm -f /etc/systemd/system/nokhodi-chat.service /usr/local/bin/nokhodichat
sudo systemctl daemon-reload
sudo rm -rf /opt/nokhodi-chat   # removes everything, including messages and config
```

Defaults on a fresh install: **user `changeme123`, OP `changeme-op123`** — change both at once (options 3 and 4). An existing v1 install keeps its old password as the user password, and gets OP `changeme-op123` — change that first.

## 4. How people use it

1. Open `http://<server-ip>:44044`
2. Enter a password: the **user password** to chat, the **OP password** to chat as 👑 OP
3. Enter **your own display name** and solve a maths captcha
4. Pick a room from the list, or open a hidden room's secret link and enter that room's password
5. Chat. Messages appear live for everyone in that room.

**Sessions:** after logging in, the browser stays logged in for **7 days** (HttpOnly session cookie, persisted in `data/sessions.json`). The display name is also remembered in `localStorage`.

**Migration from v1:** your old messages move into `#general` automatically (`data/messages.json` → `data/room_general.json`, the old file is kept as `messages.json.v1.bak`). Your old password becomes the user password.

## 5. Config — `config.json`

```json
{
  "port": 44044,
  "host": "0.0.0.0",
  "user_password_hash": "<salted sha256, never plain text>",
  "user_salt": "<random hex>",
  "admin_password_hash": "<salted sha256, never plain text>",
  "admin_salt": "<random hex>",
  "session_days": 7,
  "data_dir": "data",
  "title": "اتاق نخودی"
}
```

- Created automatically on first run if missing.
- Change passwords with `nokhodichat` options 3/4, in the OP panel, or directly:
  ```bash
  ./nokhodi-chat passwd -role user -pass 'new-user-secret'
  ./nokhodi-chat passwd -role op -pass 'new-op-secret'
  sudo systemctl restart nokhodi-chat
  ```
- Data lives in `data/rooms.json` (room list), `data/room_<id>.json` (each room's history), `data/sessions.json`, `data/bans.json`.

## 6. API (for reference)

| Endpoint | Method | Who | Purpose |
|---|---|---|---|
| `/` | GET | — | Chat UI |
| `/health` | GET | — | `{"status":"ok", ...}` |
| `/api/captcha` | GET | — | New maths captcha `{id, question}` |
| `/api/login` | POST | — | Password only; returns `role` (`op` / `user`) |
| `/api/identity` | POST | session | Display name + captcha answer |
| `/api/me` | GET | session | `{authenticated, has_name, display_name, is_op}` |
| `/api/rooms` | GET | session | Visible rooms only (hidden rooms are omitted) |
| `/api/rooms` | POST | OP | Create a room `{name, password, hidden, ttl_seconds}` |
| `/api/rooms/{id}` | GET | session | Room info — hidden rooms return 404 without link |
| `/api/rooms/{id}` | PATCH/DELETE | OP | Edit / delete a room |
| `/api/rooms/{id}/join` | POST | session | Enter a room `{password, invite}` |
| `/api/rooms/{id}/messages` | GET/POST | joined | Room history / send a message |
| `/api/rooms/{id}/messages/{mid}` | DELETE | own or OP | Delete a message |
| `/api/rooms/{id}/events` | GET | joined | SSE live feed for that room |
| `/api/rooms/{id}/online` | GET | joined | Who is online in the room |
| `/api/admin/passwords` | POST | OP | Change OP or user password |
| `/api/admin/ban` | POST | OP | Ban a display name (and log them out) |
| `/api/logout` | POST | session | Log out |

## 7. Test locally (no server needed)

```bash
go vet ./...
go test ./... -v
go run . -config config.json
# then open http://localhost:44044
# user password: changeme123 — OP password: changeme-op123
```

## Notes & limits

- **Plain HTTP on purpose:** the room is meant for a private server / LAN. If you expose it to the internet, put it behind TLS (Caddy/Nginx reverse proxy) and a firewall — and change both default passwords first.
- **Emoji:** full UTF-8 support, in messages and via the built-in emoji picker.
- **Storage:** JSON files, no database server to run. Fine for a small private room.
- **Real-time:** SSE (not WebSockets) — simpler, works through most proxies, auto-reconnects, and needs no external Go libraries.
- **Self-destruct timing:** the timer starts when a message is *sent*, and applies to new messages in that room. Changing a room's timer does not change messages already sent.
- **Hidden rooms are not secret by obscurity alone:** the link contains a random token, and the room password is still required. Without both, the server returns 404 and leaks nothing.
