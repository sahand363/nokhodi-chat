// اتاق خواب نخودی — frontend: login -> display name + captcha -> chat (SSE).
(function () {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const screens = { login: $("screen-login"), identity: $("screen-identity"), chat: $("screen-chat") };
  let me = "";
  let lastId = 0;
  let lastDay = "";
  let es = null;

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

  // --- Login (shared global credentials) ---
  $("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const btn = e.target.querySelector("button[type=submit]");
    btn.disabled = true;
    showErr($("login-error"), "");
    try {
      await api("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: $("login-user").value.trim(), password: $("login-pass").value }),
      });
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

  // --- Identity (own display name + maths captcha) ---
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
        body: JSON.stringify({
          display_name: name,
          captcha_id: captchaId,
          captcha_answer: parseInt($("captcha-answer").value, 10),
        }),
      });
      me = data.display_name;
      localStorage.setItem("nokhodi_name", me);
      await enterChat();
    } catch (err) {
      showErr($("identity-error"), err.message);
      await loadCaptcha();
    } finally {
      btn.disabled = false;
    }
  });

  // --- Chat ---
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
    const nameEl = document.createElement("p");
    nameEl.className = "name";
    nameEl.textContent = m.name;
    const bubble = document.createElement("div");
    bubble.className = "bubble";
    bubble.textContent = m.text; // textContent: no HTML injection from other users
    const timeEl = document.createElement("span");
    timeEl.className = "time";
    timeEl.textContent = faTime(m.unix);
    wrap.append(nameEl, bubble, timeEl);
    const list = $("messages");
    const empty = list.querySelector(".empty");
    if (empty) empty.remove();
    list.appendChild(wrap);
    list.scrollTop = list.scrollHeight;
  }

  async function enterChat() {
    $("me-name").textContent = me;
    show("chat");
    const list = $("messages");
    list.innerHTML = "";
    lastId = 0; lastDay = "";
    try {
      const data = await api("/api/messages?limit=200");
      if (!data.messages || data.messages.length === 0) {
        list.innerHTML = '<p class="empty">هنوز پیامی نیست — اولین پیام را شما بفرستید 🫛</p>';
      } else {
        data.messages.forEach(addMessage);
      }
    } catch (e) {
      list.innerHTML = '<p class="empty">خطا در گرفتن پیام‌ها — صفحه را تازه کنید</p>';
    }
    connectSSE();
    $("message-input").focus();
  }

  function connectSSE() {
    if (es) es.close();
    es = new EventSource("/api/events");
    es.addEventListener("message", (e) => {
      try { addMessage(JSON.parse(e.data)); } catch (_) {}
    });
    es.onerror = () => { /* EventSource retries automatically */ };
  }

  async function sendMessage() {
    const input = $("message-input");
    const text = input.value.trim();
    if (!text) return;
    input.value = "";
    try {
      const m = await api("/api/messages", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text }),
      });
      addMessage(m); // SSE will also deliver it; addMessage dedupes by id
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

  // Emoji picker: inserts the emoji at the cursor.
  $("emoji-btn").addEventListener("click", () => {
    const row = $("emoji-row");
    row.hidden = !row.hidden;
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

  $("logout-btn").addEventListener("click", async () => {
    if (es) es.close();
    try { await api("/api/logout", { method: "POST" }); } catch (_) {}
    localStorage.removeItem("nokhodi_name");
    location.reload();
  });

  // --- Boot: remember the session for 7 days, remember the name ---
  (async function boot() {
    try {
      const meData = await api("/api/me");
      if (!meData.authenticated) { show("login"); return; }
      if (!meData.has_name) {
        show("identity");
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
