# 🫛 اتاق نخودی — Self-Hosted Chat

A complete, modern, self-hosted web chat in **Go (standard library only — no external dependencies)**, built for an Ubuntu server. Mobile-first Persian RTL UI (Vazirmatn), real-time messages over SSE, persistent JSON storage, and a clean CLI management menu.

## What you get

| Piece | File |
|---|---|
| Go backend (auth, sessions, captcha, SSE, JSON store) | `main.go`, `server.go`, `auth.go`, `store.go`, `sse.go` |
| Frontend (login → name + captcha → chat) | `web/index.html`, `web/style.css`, `web/app.js` |
| Management menu | `manage.sh` |
| One-line installer | `install.sh` |
| systemd service | `nokhodi-chat.service` |
| CI/CD (builds the binary on GitHub) | `.github/workflows/release.yml` |
| Config schema | `config.example.json` |

## 1. Publish it (once, on your computer)

```bash
# 1. Repo is sahand363/nokhodi-chat — install.sh and manage.sh already use that name.
git add . && git commit -m "nokhodi chat v1" && git push origin main

# 3. Tag a release — GitHub Actions builds the binary automatically:
git tag v1.0.0
git push origin v1.0.0
# Check Actions → a Release appears with nokhodi-chat-linux-amd64.tar.gz
```

To release an update later: bump the code, then `git tag v1.0.1 && git push origin v1.0.1`.

## 2. Install on Ubuntu (one line, no compilation on the server)

```bash
curl -sSL https://raw.githubusercontent.com/sahand363/nokhodi-chat/main/install.sh | sudo bash
```

It downloads the pre-built binary, installs it to `/opt/nokhodi-chat`, creates the systemd service, starts it, and opens the management menu.

Then open: **http://&lt;server-ip&gt;:44044**

> If port 44044 is blocked: `sudo ufw allow 44044/tcp`

## 3. Management menu

Type `nokhodichat` in any SSH session to open it (or `sudo /opt/nokhodi-chat/manage.sh`):

```bash
nokhodichat
```

```
1) Install / Start Service
2) Update (latest release — keeps messages & config)
3) Set / Change Access Credentials
4) Status
5) Uninstall
0) Exit
```

- **Update** downloads the new binary, keeps a `.bak` rollback copy, restarts the service, and **never touches `config.json` or `data/messages.json`**.
- **Credentials** (option 3) changes the shared global username/password. The password is stored only as a salted SHA-256 hash.
- **Uninstall** stops the service and asks whether to keep `config.json` + `data/`.

## 4. How people use it

1. Open `http://<server-ip>:44044`
2. Enter the **shared global username/password** (default `admin` / `changeme123` — **change it immediately**, menu option 3)
3. Enter **your own display name** (must be unique among people online) and solve a simple maths captcha, e.g. `7 + 5 = ؟`
4. Chat. Messages appear live for everyone.

**Sessions:** after logging in, the browser stays logged in for **7 days** (HttpOnly session cookie, persisted in `data/sessions.json` so a server restart doesn't log people out). The display name is also remembered in `localStorage` and pre-filled next time.

## 5. Config — `config.json`

```json
{
  "port": 44044,
  "host": "0.0.0.0",
  "username": "admin",
  "password_hash": "<salted sha256, never plain text>",
  "salt": "<random hex>",
  "session_days": 7,
  "data_dir": "data",
  "title": "اتاق نخودی"
}
```

- Created automatically on first run if missing.
- Change credentials with `manage.sh` option 3, or directly:
  ```bash
  ./nokhodi-chat passwd -config config.json -user admin -pass 'new-secret'
  sudo systemctl restart nokhodi-chat
  ```
- Data lives in `data/messages.json` (all history, JSON array) and `data/sessions.json`.

## 6. API (for reference)

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/` | GET | — | Chat UI |
| `/health` | GET | — | `{"status":"ok", ...}` |
| `/api/captcha` | GET | — | New maths captcha `{id, question}` |
| `/api/login` | POST | — | Global login, sets session cookie |
| `/api/identity` | POST | session | Display name + captcha answer |
| `/api/me` | GET | session | `{authenticated, has_name, display_name}` |
| `/api/messages` | GET | session+name | Last 200 messages (history) |
| `/api/messages` | POST | session+name | Send a message (emoji supported) |
| `/api/events` | GET | session+name | SSE live feed (`event: message`) |
| `/api/logout` | POST | session | Log out |

## 7. Test locally (no server needed)

```bash
go vet ./...
go test ./... -v
go run . -config config.json
# then open http://localhost:44044  (login: admin / changeme123)
```

## Notes & limits

- **Plain HTTP on purpose:** the room is meant for a private server / LAN. If you expose it to the internet, put it behind TLS (Caddy/Nginx reverse proxy) and a firewall — and change the default password first.
- **Emoji:** full UTF-8 support, in messages and via the built-in emoji picker.
- **Storage:** JSON files, no database server to run. Fine for a small private room (a few hundred people / thousands of messages). If you outgrow it, swap `store.go` for SQLite — the interface (`Add` / `Last`) is the only thing the server uses.
- **Real-time:** SSE (not WebSockets) — simpler, works through most proxies, auto-reconnects in the browser, and needs no external Go libraries.
