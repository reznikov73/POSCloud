/* POSCloud Server — UI */
const $ = (id) => document.getElementById(id);
let seeded = false;      // поля ввода заполняются один раз
let autostartLoaded = false;
let autoRunLoaded = false;
const expandedUsers = new Set();
let currentTab = "tab-server";
let selectedUser = null; // {id, name, enabled}

async function call(method, ...args) {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) return null;
    return await window.go.main.App[method](...args);
  } catch (e) { console.error(method, e); return null; }
}

function fmtSize(b) {
  if (!b) return "0 Б";
  const u = ["Б", "КБ", "МБ", "ГБ"];
  const i = Math.floor(Math.log(b) / Math.log(1024));
  return (b / Math.pow(1024, i)).toFixed(i ? 1 : 0) + " " + u[i];
}
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

/* ---------- закладки ---------- */
function activateTab(id) {
  currentTab = id;
  document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x.dataset.tab === id));
  document.querySelectorAll(".pane").forEach((x) => x.classList.toggle("active", x.id === id));
  if (id === "tab-files") renderTree();
  if (id === "tab-users") refreshUsers();
  if (id === "tab-log") refreshLogs();
}
document.querySelectorAll(".tab").forEach((t) => t.addEventListener("click", () => activateTab(t.dataset.tab)));

/* ---------- журнал ---------- */
function renderLogs(lines) {
  $("logbody").textContent = (lines || []).join("\n");
  $("logbody").scrollTop = $("logbody").scrollHeight;
}
async function refreshLogs() { renderLogs((await call("GetLogs")) || []); }
$("logOpen").addEventListener("click", () => call("OpenLogFile"));
$("logClear").addEventListener("click", async () => { await call("ClearLogs"); renderLogs([]); });
$("logRefresh").addEventListener("click", () => refreshLogs());

/* ---------- состояние сервера ---------- */
function renderServer(st) {
  if (!st) return;
  $("srvPort").textContent = st.port;
  $("srvPortFree").textContent = st.portFree ? "да" : "нет";
  $("srvUsers").textContent = st.users || 0;
  $("srvDir").textContent = st.dataDir || "-";
  $("srvAddr").textContent = st.running ? (st.url || "-") : "запустите сервер, чтобы увидеть адрес";
  $("srvCfgPath").textContent = st.configPath ? ("Файл настроек: " + st.configPath) : "";

  const dot = $("srvDot");
  dot.className = "dot";
  if (st.running) { dot.classList.add("on"); $("srvStatusText").textContent = "работает"; }
  else if (!st.portFree) { dot.classList.add("busy"); $("srvStatusText").textContent = "порт занят другим процессом"; }
  else { dot.classList.add("off"); $("srvStatusText").textContent = "остановлен"; }

  // Одна кнопка меняет назначение: запускает или останавливает сервер.
  serverRunning = !!st.running;
  const tg = $("srvToggle");
  tg.textContent = st.running ? "Остановить" : "Запустить";
  tg.className = st.running ? "btn" : "btn primary";

  if (st.error) { $("srvErr").style.color = "#e5484d"; $("srvErr").textContent = st.error; }
  else { $("srvErr").textContent = ""; }

  if (!seeded) {
    $("srvPortInput").value = st.port;
    $("srvDirInput").value = st.dataDir || "";
    seeded = true;
  }
}

/* ---------- пользователи ---------- */
function renderUsers(users) {
  const tb = $("usrRows");
  if (!users || users.length === 0) { tb.innerHTML = '<tr><td colspan="6" class="empty">нет пользователей - создайте первого</td></tr>'; return; }
  tb.innerHTML = "";
  users.forEach((u) => {
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + escapeHtml(u.name) + "</td><td>" + escapeHtml(u.id) + "</td><td>" +
      u.tokenCount + "</td><td>" + u.fileCount + "</td><td>" +
      (u.enabled ? '<span style="color:#1aae39">активен</span>' : '<span style="color:#c62a2f">отключён</span>') +
      '</td><td><button class="del" data-act="sel">Открыть</button></td>';
    tr.querySelector('[data-act="sel"]').addEventListener("click", () => selectUser(u));
    tb.appendChild(tr);
  });
}

function renderTokens(tokens) {
  const tb = $("tokenRows");
  if (!tokens || tokens.length === 0) { tb.innerHTML = '<tr><td colspan="4" class="empty">нет токенов</td></tr>'; return; }
  tb.innerHTML = "";
  tokens.forEach((t) => {
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + escapeHtml(t.name) + "</td><td>" + escapeHtml(t.createdAt || "-") +
      "</td><td>" + escapeHtml(t.lastSeen || "-") +
      '</td><td><button class="del">Отозвать</button></td>';
    tr.querySelector(".del").addEventListener("click", async () => {
      if (!confirm('Отозвать токен "' + t.name + '"?')) return;
      await call("RevokeToken", selectedUser.id, t.id); loadUserDetails(); refreshUsers();
    });
    tb.appendChild(tr);
  });
}

function selectUser(u) {
  selectedUser = u;
  $("filesUser").textContent = u.name + " (" + u.id + ")";
  $("uToken").disabled = false;
  $("uToggle").disabled = false;
  $("uDelete").disabled = false;
  $("uToggle").textContent = u.enabled ? "Отключить" : "Включить";
  loadUserDetails();
}

async function loadUserDetails() {
  if (!selectedUser) return;
  const tokens = await call("ListTokens", selectedUser.id);
  renderTokens(tokens || []);
}

/* ---------- дерево файлов ---------- */
async function renderTree() {
  const box = $("fileTree");
  const users = (await call("ListUsers")) || [];
  if (users.length === 0) { box.innerHTML = '<div class="empty">нет пользователей</div>'; return; }
  const trs = (await call("GetTransfers")) || [];
  const trMap = {};
  trs.forEach((t) => { trMap[t.name] = t; });
  box.innerHTML = "";
  for (const u of users) {
    const files = (await call("UserFiles", u.id)) || [];
    files.sort((a, b) => {
      const ua = trMap[a.name] ? 0 : 1;
      const ub = trMap[b.name] ? 0 : 1;
      if (ua !== ub) return ua - ub;
      return a.name < b.name ? -1 : (a.name > b.name ? 1 : 0);
    });
    const wrap = document.createElement("div"); wrap.className = "tree-user";
    const head = document.createElement("div"); head.className = "tree-head";
    head.innerHTML = '<span class="name">' + escapeHtml(u.name) + ' <span class="meta">(' + escapeHtml(u.id) + ')</span></span>' +
      '<span class="meta">' + files.length + ' файл(ов)</span>';
    const list = document.createElement("div"); list.className = "tree-files";
    if (expandedUsers.has(u.id)) list.classList.add("open");
    if (files.length === 0) {
      const e = document.createElement("div"); e.className = "tree-file"; e.innerHTML = '<span class="fn meta">пусто</span>';
      list.appendChild(e);
    }
    files.forEach((f) => {
      const t = trMap[f.name];
      const nameHtml = t
        ? escapeHtml(f.name) + '<div class="pbar"><div class="pfill" style="width:' + t.pct + '%"></div></div><div class="pct">' + t.pct + '% ' + (t.dir === "up" ? "↑ приём" : "↓ отдача") + "</div>"
        : escapeHtml(f.name);
      const row = document.createElement("div"); row.className = "tree-file";
      row.innerHTML = '<span class="fn">' + nameHtml + '</span><span class="fs">' + fmtSize(f.size) +
        '</span><span class="fm">' + escapeHtml(f.modified) + '</span><button class="del">Удалить</button>';
      row.querySelector(".del").addEventListener("click", async (ev) => {
        ev.stopPropagation();
        if (!confirm('Удалить "' + f.name + '" у пользователя ' + u.name + '?')) return;
        await call("DeleteUserFile", u.id, f.name); renderTree(); refreshUsers();
      });
      list.appendChild(row);
    });
    head.addEventListener("click", () => {
      if (expandedUsers.has(u.id)) { expandedUsers.delete(u.id); list.classList.remove("open"); }
      else { expandedUsers.add(u.id); list.classList.add("open"); }
    });
    wrap.appendChild(head); wrap.appendChild(list);
    box.appendChild(wrap);
  }
}

/* ---------- действия ---------- */
function form() { return { port: parseInt($("srvPortInput").value, 10) || 8090, dir: $("srvDirInput").value.trim() }; }
function flash(msg, ok) { $("srvErr").style.color = ok ? "#1aae39" : "#e5484d"; $("srvErr").textContent = msg; }

let serverRunning = false;
$("srvToggle").addEventListener("click", async () => {
  if (serverRunning) { renderServer(await call("StopServer")); refresh(); return; }
  const f = form(); renderServer(await call("StartServer", f.port, f.dir)); refresh();
});
// Освободить порт: свой сервер — просто остановить; чужой процесс — показать и,
// после подтверждения, завершить именно его (по PID).
$("srvRelease").addEventListener("click", async () => {
  const f = form();
  const st = await call("GetServerState");
  if (st && st.running) {
    renderServer(await call("StopServer")); refresh();
    flash("сервер остановлен, порт освобождён", true);
    return;
  }
  const h = await call("PortHolder", f.port);
  if (!h || !h.pid) {
    renderServer(await call("GetServerState"));
    flash("порт " + f.port + " свободен", true);
    return;
  }
  if (h.isSelf) {
    flash("порт слушает это же приложение — сначала остановите сервер", false);
    return;
  }
  const info = ["Порт " + f.port + " занят процессом:", (h.name || "неизвестный процесс") + " (PID " + h.pid + ")"];
  if (h.path) info.push(h.path);
  info.push("", "Завершить этот процесс?");
  if (!confirm(info.join("\n"))) { flash("отменено", false); return; }
  const res = await call("ReleasePort", f.port, h.pid);
  renderServer(res); refresh();
  flash(res && res.error ? res.error : "процесс завершён, порт освобождён", !(res && res.error));
});
$("srvSave").addEventListener("click", async () => {
  const f = form(); const st = await call("SaveSettings", f.port, f.dir); renderServer(st);
  if (st && !st.error) flash("настройки сохранены", true);
});
$("srvPick").addEventListener("click", async () => { const d = await call("PickDirectory"); if (d) $("srvDirInput").value = d; });
$("srvAutostart").addEventListener("change", async (e) => {
  await call("SetAutostart", e.target.checked);
  flash(e.target.checked ? "программа будет запускаться при входе в Windows" : "запуск программы при входе выключен", true);
});
$("srvAutoRun").addEventListener("change", async (e) => {
  await call("SetAutoRun", e.target.checked);
  flash(e.target.checked ? "сервер будет стартовать при открытии программы" : "сервер нужно будет запускать вручную", true);
});

$("usrRefresh").addEventListener("click", () => refreshUsers());
$("treeRefresh").addEventListener("click", () => renderTree());

$("usrNew").addEventListener("click", async () => {
  const name = prompt("Имя пользователя (например, «Альберт»):", "");
  if (!name || !name.trim()) return;
  const u = await call("CreateUser", name.trim());
  if (!u || !u.id) { flash("не удалось создать пользователя", false); return; }
  await refreshUsers();
  selectUser(u);
  const dev = prompt("Название первого устройства (например, «Домашний ПК»):", "Домашний ПК");
  if (dev === null) { flash("пользователь создан. Токен можно выдать кнопкой «Новый токен»", true); return; }
  const tok = await call("IssueToken", u.id, (dev || "").trim() || "Устройство");
  if (tok) {
    $("tokenBox").style.display = "block";
    $("tokenValue").textContent = tok;
    $("tokenBox").scrollIntoView({ behavior: "smooth", block: "center" });
    loadUserDetails(); refreshUsers();
  } else { flash("пользователь создан, но токен выдать не удалось", false); }
});

$("uToken").addEventListener("click", async () => {
  if (!selectedUser) return;
  const dev = prompt("Название устройства (например, «Рабочий ПК»):", "Устройство");
  if (dev === null) return;
  const tok = await call("IssueToken", selectedUser.id, dev.trim());
  if (tok) {
    $("tokenBox").style.display = "block";
    $("tokenValue").textContent = tok;
    $("tokenBox").scrollIntoView({ behavior: "smooth", block: "center" });
    loadUserDetails(); refreshUsers();
  }
});

$("uToggle").addEventListener("click", async () => {
  if (!selectedUser) return;
  await call("SetUserEnabled", selectedUser.id, !selectedUser.enabled);
  await refreshUsers();
  selectedUser.enabled = !selectedUser.enabled;
  $("uToggle").textContent = selectedUser.enabled ? "Отключить" : "Включить";
});

$("uDelete").addEventListener("click", async () => {
  if (!selectedUser) return;
  if (!confirm('Удалить пользователя "' + selectedUser.name + '"? Файлы на диске останутся, но доступ пропадёт.')) return;
  await call("DeleteUser", selectedUser.id);
  selectedUser = null;
  $("filesUser").textContent = "не выбран";
  $("uToken").disabled = $("uToggle").disabled = $("uDelete").disabled = true;
  $("tokenRows").innerHTML = '<tr><td colspan="4" class="empty">-</td></tr>';
  refreshUsers();
});

async function refreshUsers() {
  const users = (await call("ListUsers")) || [];
  renderUsers(users);
  return users;
}

async function refresh() {
  const [st, logs] = await Promise.all([call("GetServerState"), call("GetLogs")]);
  renderServer(st); renderLogs(logs);
  $("pulse").className = "pulse" + (st && st.running ? " on" : "");
  if (!autostartLoaded) {
    const au = await call("AutostartEnabled");
    if (typeof au === "boolean") { $("srvAutostart").checked = au; autostartLoaded = true; }
  }
  if (!autoRunLoaded) {
    const ar = await call("AutoRunEnabled");
    if (typeof ar === "boolean") { $("srvAutoRun").checked = ar; autoRunLoaded = true; }
  }
}

window.addEventListener("DOMContentLoaded", () => { refresh(); refreshUsers(); setInterval(refresh, 500); setInterval(() => { if (currentTab === "tab-files") renderTree(); }, 1500); });
