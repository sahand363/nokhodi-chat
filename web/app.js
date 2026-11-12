// اتاق نخودی v2 — login (OP/user password) -> name + captcha -> rooms.
// Hidden rooms: not listed, only via ?room=<id>&invite=<token>, still
// need the room password, and the server returns 404 without link+password.
(function () {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const screens = { login: $("screen-login"), identity: $("screen-identity"), chat: $("screen-chat") };
  let me = "";
  let isOP = false;
  let rooms = [];
  let currentRoom = null; // room object
  let lastId = 0;
  let lastDay = "";
  let es = null;
  let pendingRoom = null; // {id, invite} from a hidden link
  let presenceTimer = null;
  let onlineTimer = null;

  function show(name) {
    for (const k in screens) screens[k].hidden = k !== name;
  }
  function showErr(el, msg) {
    el.textContent = msg;
    el.hidden = !msg;
  }
  async function api(path, opts) {
    const res = await fetch(path, opts);
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || ("خطا: " + res.status));
    return data;
  }
  const faTime = (unix) => new Date(unix * 1000).toLocaleTimeString("fa-IR", { hour: "2-digit", minute: "2-digit" });
  const faDay = (unix) => new Date(unix * 1000).toLocaleDateString("fa-IR", { weekday: "long", day: "numeric", month: "long" });
  const ttlText = (s) => ({ 0: "پیام‌ها پاک نمی‌شوند", 600: "پیام‌ها بعد از ۱۰ دقیقه پاک می‌شوند", 3600: "پیام‌ها بعد از ۱ ساعت پاک می‌شوند", 86400: "پیام‌ها بعد از ۲۴ ساعت پاک می‌شوند", 604800: "پیام‌ها بعد از ۷ روز پاک می‌شوند" }[s] || "");

  // --- Captcha ---
  let captchaId = "";
  async function loadCaptcha() {
    try {
      const c = await api("/api/captcha");
      captchaId = c.id;
      $("captcha-question").textContent = c.question;
      $("captcha-answer").value = "";
    } catch (e) {
      $("captcha-question").textContent = "خطا در گرفتن کپچا";
    }
  }
  $("captcha-refresh").addEventListener("click", loadCaptcha);

  // --- Login: one password, OP or user ---
  $("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const btn = e.target.querySelector("button[type=submit]");
    btn.disabled = true;
    showErr($("login-error"), "");
    try {
      const data = await api("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: $("login-pass").value }),
      });
      isOP = !!data.is_op;
      $("identity-role").textContent = isOP ? "👑 OP" : "";
      show("identity");
      const saved = localStorage.getItem("nokhodi_name") || "";
      if (saved) $("display-name").value = saved;
      await loadCaptcha();
      $("display-name").focus();
    } catch (err) {
      showErr($("login-error"), err.message);
    } finally {
      btn.disabled = false;
    }
  });

  // --- Identity ---
  $("identity-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const btn = e.target.querySelector("button[type=submit]");
    btn.disabled = true;
    showErr($("identity-error"), "");
    try {
      const name = $("display-name").value.trim();
      const data = await api("/api/identity", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: name, captcha_id: captchaId, captcha_answer: parseInt($("captcha-answer").value, 10) }),
      });
      me = data.display_name;
      isOP = !!data.is_op;
      localStorage.setItem("nokhodi_name", me);
      await enterChat();
    } catch (err) {
      showErr($("identity-error"), err.message);
      await loadCaptcha();
    } finally {
      btn.disabled = false;
    }
  });

  // --- Rooms ---
  async function loadRooms() {
    const data = await api("/api/rooms");
    rooms = data.rooms || [];
    renderRooms();
    return rooms;
  }

  function renderRooms() {
    const list = $("rooms-list");
    list.innerHTML = "";
    rooms.forEach((r) => {
      const b = document.createElement("button");
      b.className = "room-item" + (currentRoom && currentRoom.id === r.id ? " active" : "");
      b.type = "button";
      b.textContent = "#" + r.name + (r.locked ? " 🔒" : "") + (r.hidden ? " 👁‍🗨 مخفی" : "");
      b.addEventListener("click", () => selectRoom(r.id));
      list.appendChild(b);
    });
    $("op-panel-btn").hidden = !isOP;
  }

  function roomById(id) { return rooms.find((r) => r.id === id); }

  async function selectRoom(id, invite) {
    let room = roomById(id);
    // Hidden room from a link: it is not in the list, fetch it by id+invite.
    if (!room) {
      try {
        const q = invite ? ("?invite=" + encodeURIComponent(invite)) : "";
        room = await api("/api/rooms/" + encodeURIComponent(id) + q);
        rooms.push(room);
        renderRooms();
      } catch (e) {
        alert("اتاق پیدا نشد");
        return;
      }
    }
    // Join: locked rooms need a password, open rooms join with one click.
    try {
      const joined = await api("/api/rooms/" + encodeURIComponent(id) + "/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: "", invite: invite || "" }),
      });
      room = joined;
    } catch (e) {
      // Locked: ask for the room password.
      pendingRoom = { id, invite: invite || "" };
      $("room-pass-text").textContent = "برای ورود به #" + room.name + " رمز آن را وارد کنید";
      $("room-pass-input").value = "";
      showErr($("room-pass-error"), "");
      $("room-pass-modal").hidden = false;
      $("room-pass-input").focus();
      return;
    }
    openRoom(room);
  }

  $("room-pass-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (!pendingRoom) return;
    showErr($("room-pass-error"), "");
    try {
      const joined = await api("/api/rooms/" + encodeURIComponent(pendingRoom.id) + "/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: $("room-pass-input").value, invite: pendingRoom.invite || "" }),
      });
      $("room-pass-modal").hidden = true;
      pendingRoom = null;
      openRoom(joined);
    } catch (err) {
      showErr($("room-pass-error"), err.message);
    }
  });
  $("room-pass-cancel").addEventListener("click", () => {
    $("room-pass-modal").hidden = true;
    pendingRoom = null;
  });

  async function openRoom(room) {
    currentRoom = room;
    $("room-title").textContent = "#" + room.name + (room.locked ? " 🔒" : "") + (room.hidden ? " 👁‍🗨" : "");
    $("room-ttl").textContent = ttlText(room.ttl_seconds);
    $("me-name").textContent = me + (isOP ? " 👑 OP" : "");
    // Update the URL so a hidden room's link can be copied, without reloading.
    const url = new URL(location.href);
    if (room.hidden && room.invite_token) {
      url.searchParams.set("room", room.id);
      url.searchParams.set("invite", room.invite_token);
    } else {
      url.searchParams.delete("room");
      url.searchParams.delete("invite");
    }
    history.replaceState(null, "", url);
    renderRooms();
    $("rooms-panel").classList.remove("open");
    const list = $("messages");
    list.innerHTML = "";
    lastId = 0; lastDay = "";
    try {
      const data = await api("/api/rooms/" + encodeURIComponent(room.id) + "/messages?limit=200");
      if (!data.messages || data.messages.length === 0) {
        list.innerHTML = '<p class="empty">هنوز پیامی نیست — اولین پیام را شما بفرستید 🫛</p>';
      } else {
        data.messages.forEach(addMessage);
      }
    } catch (e) {
      list.innerHTML = '<p class="empty">خطا در گرفتن پیام‌ها — صفحه را تازه کنید</p>';
    }
    connectSSE(room.id);
    startPresence(room.id);
    $("message-input").focus();
  }

  // --- Messages ---
  function addMessage(m) {
    if (m.id <= lastId) return;
    lastId = m.id;
    const day = faDay(m.unix);
    if (day !== lastDay) {
      lastDay = day;
      const sep = document.createElement("div");
      sep.className = "day-sep";
      sep.textContent = day;
      $("messages").appendChild(sep);
    }
    const mine = m.name === me;
    const wrap = document.createElement("div");
    wrap.className = "msg " + (mine ? "me" : "other");
    wrap.dataset.id = m.id;
    const nameEl = document.createElement("p");
    nameEl.className = "name";
    nameEl.textContent = m.name;
    const bubble = document.createElement("div");
    bubble.className = "bubble";
    if (m.text) bubble.textContent = m.text;
    // Uploads: images (jpg/png/webp/gif — GIFs play animated) are shown
    // big in the chat; other files show as a download card.
    if (m.attachment) {
      const a = m.attachment;
      if (a.is_image) {
        wrap.classList.add("has-image");
        const img = document.createElement("img");
        img.className = "chat-img";
        img.src = a.url;
        img.alt = a.file_name;
        img.loading = "lazy";
        img.addEventListener("click", () => window.open(a.url, "_blank"));
        bubble.appendChild(img);
      } else {
        const card = document.createElement("a");
        card.className = "file-card";
        card.href = a.url;
        card.download = a.file_name;
        card.textContent = "📄 " + a.file_name + " — " + fmtSize(a.size);
        bubble.appendChild(card);
      }
    }
    const timeEl = document.createElement("span");
    timeEl.className = "time";
    timeEl.textContent = faTime(m.unix);
    wrap.append(nameEl, bubble, timeEl);
    if (mine || isOP) {
      const del = document.createElement("button");
      del.className = "del-btn";
      del.type = "button";
      del.textContent = "🗑";
      del.title = "حذف پیام";
      del.addEventListener("click", async () => {
        if (!confirm("این پیام حذف شود؟")) return;
        try {
          await api("/api/rooms/" + encodeURIComponent(currentRoom.id) + "/messages/" + m.id, { method: "DELETE" });
          wrap.remove();
        } catch (e) { alert(e.message); }
      });
      wrap.appendChild(del);
    }
    const list = $("messages");
    const empty = list.querySelector(".empty");
    if (empty) empty.remove();
    list.appendChild(wrap);
    list.scrollTop = list.scrollHeight;
  }

  function removeMessage(id) {
    const el = document.querySelector('.msg[data-id="' + id + '"]');
    if (el) el.remove();
  }

  function connectSSE(roomId) {
    if (es) es.close();
    es = new EventSource("/api/rooms/" + encodeURIComponent(roomId) + "/events");
    es.addEventListener("message", (e) => {
      try { const ev = JSON.parse(e.data); if (ev.message) addMessage(ev.message); } catch (_) {}
    });
    es.addEventListener("delete", (e) => {
      try { const ev = JSON.parse(e.data); if (ev.message_id) removeMessage(ev.message_id); } catch (_) {}
    });
    es.addEventListener("prune", () => {
      // Self-destructed messages: reload this room's history.
      if (currentRoom) openRoom(currentRoom);
    });
    es.onerror = () => { /* EventSource retries automatically */ };
  }

  function startPresence(roomId) {
    if (presenceTimer) clearInterval(presenceTimer);
    if (onlineTimer) clearInterval(onlineTimer);
    const ping = () => fetch("/api/rooms/" + encodeURIComponent(roomId) + "/presence", { method: "POST" }).catch(() => {});
    const loadOnline = async () => {
      try {
        const data = await api("/api/rooms/" + encodeURIComponent(roomId) + "/online");
        const box = $("online-list");
        box.innerHTML = "";
        (data.online || []).forEach((name) => {
          const row = document.createElement("div");
          row.className = "online-user";
          row.textContent = (name === me ? "👑 " : "") + name;
          if (isOP && name !== me) {
            const kick = document.createElement("button");
            kick.type = "button";
            kick.textContent = "مسدود";
            kick.addEventListener("click", async () => {
              if (!confirm("«" + name + "» مسدود شود؟")) return;
              try { await api("/api/admin/ban", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ display_name: name }) }); loadOnline(); } catch (e) { alert(e.message); }
            });
            row.appendChild(kick);
          }
          box.appendChild(row);
        });
      } catch (_) {}
    };
    ping(); loadOnline();
    presenceTimer = setInterval(ping, 30000);
    onlineTimer = setInterval(loadOnline, 20000);
  }

  async function sendMessage() {
    const input = $("message-input");
    const text = input.value.trim();
    if (!text || !currentRoom) return;
    // /commands
    if (text === "/help") {
      alert("/join #name — ورود به اتاق\n/list — دیدن اتاق‌ها\n/me کار — * شما کار می‌کنید\n📎 — ارسال عکس یا فایل (حداکثر ۱۰ مگابایت، ۲۰ فایل در روز)");
      input.value = "";
      return;
    }
    if (text === "/list") {
      await loadRooms();
      input.value = "";
      return;
    }
    if (text.startsWith("/join ")) {
      const name = text.slice(6).trim().replace(/^#/, "");
      const room = rooms.find((r) => r.name === name);
      input.value = "";
      if (room) selectRoom(room.id);
      else alert("اتاقی به این نام پیدا نشد");
      return;
    }
    input.value = "";
    try {
      const m = await api("/api/rooms/" + encodeURIComponent(currentRoom.id) + "/messages", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text }),
      });
      addMessage(m);
    } catch (err) {
      alert(err.message);
      input.value = text;
    }
    input.focus();
  }
  $("send-btn").addEventListener("click", sendMessage);
  $("message-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); sendMessage(); }
  });

  // --- Uploads: images and files (max 10 MB per file) ---
  function fmtSize(n) {
    if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MB";
    if (n >= 1024) return Math.round(n / 1024) + " KB";
    return n + " B";
  }
  $("upload-btn").addEventListener("click", () => $("file-input").click());
  $("file-input").addEventListener("change", async (e) => {
    const file = e.target.files && e.target.files[0];
    e.target.value = "";
    if (!file || !currentRoom) return;
    if (file.size > 10 * 1024 * 1024) {
      alert("فایل خیلی بزرگ است — حداکثر ۱۰ مگابایت برای هر فایل");
      return;
    }
    const caption = $("message-input").value.trim();
    const form = new FormData();
    form.append("file", file);
    if (caption) form.append("text", caption);
    try {
      const res = await fetch("/api/rooms/" + encodeURIComponent(currentRoom.id) + "/upload", { method: "POST", body: form });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || "خطا در ارسال فایل");
      $("message-input").value = "";
      addMessage(data);
    } catch (err) {
      alert(err.message);
    }
  });

  $("emoji-btn").addEventListener("click", () => {
    $("emoji-row").hidden = !$("emoji-row").hidden;
  });
  document.querySelectorAll("#emoji-row button").forEach((b) => {
    b.addEventListener("click", () => {
      const input = $("message-input");
      const at = input.selectionStart ?? input.value.length;
      input.value = input.value.slice(0, at) + b.textContent + input.value.slice(at);
      input.focus();
      input.selectionStart = input.selectionEnd = at + b.textContent.length;
    });
  });

  $("rooms-open").addEventListener("click", () => $("rooms-panel").classList.add("open"));
  $("rooms-close").addEventListener("click", () => $("rooms-panel").classList.remove("open"));

  $("logout-btn").addEventListener("click", async () => {
    if (es) es.close();
    try { await api("/api/logout", { method: "POST" }); } catch (_) {}
    localStorage.removeItem("nokhodi_name");
    location.href = location.pathname;
  });

  // --- OP panel ---
  $("op-panel-btn").addEventListener("click", async () => {
    $("op-modal").hidden = false;
    renderOPRooms();
  });
  $("op-close").addEventListener("click", () => { $("op-modal").hidden = true; });

  function renderOPRooms() {
    const box = $("op-rooms-list");
    box.innerHTML = "";
    rooms.forEach((r) => {
      const row = document.createElement("div");
      row.className = "op-room";
      const link = location.origin + "/?room=" + r.id + "&invite=" + (r.invite_token || "");
      row.innerHTML = "<strong>#" + r.name + "</strong> " + (r.locked ? "🔒 " : "") + (r.hidden ? "👁‍🗨 مخفی " : "") + "<br><small>" + ttlText(r.ttl_seconds) + "</small>";
      const copyBtn = document.createElement("button");
      copyBtn.type = "button";
      copyBtn.textContent = "کپی لینک مخفی";
      copyBtn.addEventListener("click", async () => {
        try { await navigator.clipboard.writeText(link); copyBtn.textContent = "کپی شد ✓"; } catch (_) { prompt("لینک اتاق:", link); }
      });
      row.appendChild(copyBtn);
      if (r.id !== "general") {
        const del = document.createElement("button");
        del.type = "button";
        del.textContent = "حذف اتاق";
        del.addEventListener("click", async () => {
          if (!confirm("اتاق #" + r.name + " و همه پیام‌هایش حذف شود؟")) return;
          try {
            await api("/api/rooms/" + encodeURIComponent(r.id), { method: "DELETE" });
            await loadRooms();
            renderOPRooms();
            if (currentRoom && currentRoom.id === r.id) selectRoom("general");
          } catch (e) { alert(e.message); }
        });
        row.appendChild(del);
      }
      box.appendChild(row);
    });
  }

  $("create-room-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    showErr($("create-room-error"), "");
    try {
      const room = await api("/api/rooms", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: $("new-room-name").value.trim(),
          password: $("new-room-pass").value,
          hidden: $("new-room-hidden").checked,
          ttl_seconds: parseInt($("new-room-ttl").value, 10),
        }),
      });
      $("new-room-name").value = "";
      $("new-room-pass").value = "";
      await loadRooms();
      renderOPRooms();
      selectRoom(room.id);
      $("op-modal").hidden = true;
    } catch (err) {
      showErr($("create-room-error"), err.message);
    }
  });

  $("change-pass-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    showErr($("change-pass-error"), "");
    try {
      await api("/api/admin/passwords", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: $("change-pass-role").value, new_password: $("change-pass-new").value }),
      });
      $("change-pass-new").value = "";
      alert("رمز تغییر کرد");
    } catch (err) {
      showErr($("change-pass-error"), err.message);
    }
  });

  $("ban-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      await api("/api/admin/ban", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: $("ban-name").value.trim() }),
      });
      $("ban-name").value = "";
      alert("مسدود شد");
    } catch (err) { alert(err.message); }
  });

  // --- Boot ---
  async function enterChat() {
    $("me-name").textContent = me + (isOP ? " 👑 OP" : "");
    show("chat");
    await loadRooms();
    // Hidden-room link? ?room=<id>&invite=<token>
    const params = new URLSearchParams(location.search);
    const linkRoom = params.get("room");
    const linkInvite = params.get("invite") || "";
    if (linkRoom) {
      pendingRoom = { id: linkRoom, invite: linkInvite };
      await selectRoom(linkRoom, linkInvite);
      return;
    }
    await selectRoom("general");
  }

  (async function boot() {
    try {
      const meData = await api("/api/me");
      if (!meData.authenticated) { show("login"); return; }
      isOP = !!meData.is_op;
      if (!meData.has_name) {
        show("identity");
        $("identity-role").textContent = isOP ? "👑 OP" : "";
        const saved = localStorage.getItem("nokhodi_name") || "";
        if (saved) $("display-name").value = saved;
        await loadCaptcha();
        return;
      }
      me = meData.display_name;
      localStorage.setItem("nokhodi_name", me);
      await enterChat();
    } catch (_) {
      show("login");
    }
  })();
})();
