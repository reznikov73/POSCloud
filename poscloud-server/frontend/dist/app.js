/* POSCloud Server — UI */
const $ = (id) => document.getElementById(id);
let seeded = false;
let autostartLoaded = false;
let autoRunLoaded = false;
let currentTab = "tab-server";
let selectedUser = null;       // пользователь во вкладке «Пользователи»
let serverRunning = false;

/* состояние вкладки «Файлы» */
let filesUserID = null;
let filesUserName = "";
let selectedFile = null;       // {userID, name}
let historyContext = null;     // {userID, name} — name="" означает корзину
let usersSignature = "";

const PAGE_META = {
  "tab-server": { title: "Сервер", sub: "состояние и параметры хранения" },
  "tab-users": { title: "Пользователи", sub: "доступы, токены и устройства" },
  "tab-files": { title: "Файлы", sub: "файлы пользователей и история версий" },
  "tab-conflicts": { title: "Конфликты", sub: "конфликтные копии и их разрешение" },
  "tab-log": { title: "Журнал", sub: "события сервера" },
  "tab-maint": { title: "Обслуживание", sub: "резервные копии и восстановление" },
};

async function call(method, ...args) {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) return null;
    return await window.go.main.App[method](...args);
  } catch (e) {
    console.error(method, e);
    return null;
  }
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
function fmtDate(s) {
  if (!s) return "-";
  const m = String(s).match(/^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/);
  return m ? (m[3] + "." + m[2] + " " + m[4] + ":" + m[5]) : s;
}
function plural(n, one, few, many) {
  const a = Math.abs(n) % 100, b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b === 1) return one;
  if (b >= 2 && b <= 4) return few;
  return many;
}
function fmtFiles(n) { return n + " " + plural(n, "файл", "файла", "файлов"); }
function fmtVersions(n) { return n + " " + plural(n, "версия", "версии", "версий"); }

/* ---------- закладки ---------- */
function activateTab(id) {
  if (!PAGE_META[id]) return;
  currentTab = id;
  document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x.dataset.tab === id));
  document.querySelectorAll(".pane").forEach((x) => x.classList.toggle("active", x.id === id));
  $("pageTitle").textContent = PAGE_META[id].title;
  $("pageSub").textContent = PAGE_META[id].sub;
  if (id === "tab-files") renderTree();
  if (id === "tab-conflicts") renderConflicts();
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

/* ---------- обслуживание ---------- */
$("backupCreate").addEventListener("click", async () => {
  $("backupStatus").textContent = "Создаю резервную копию…";
  const res = await call("CreateBackup");
  if (!res) { $("backupStatus").textContent = "Не удалось создать копию."; return; }
  if (res.error) { $("backupStatus").textContent = res.error; return; }
  $("backupStatus").textContent = "Копия сохранена: " + (res.path || "");
});
$("backupRestore").addEventListener("click", async () => {
  if (!confirm("Восстановить настройки и доступы из резервной копии? Текущие файлы будут сохранены в .restore-backup-*.")) return;
  $("restoreStatus").textContent = "Восстанавливаю…";
  const res = await call("RestoreBackup");
  if (!res) { $("restoreStatus").textContent = "Не удалось восстановить копию."; return; }
  if (res.error) { $("restoreStatus").textContent = res.error; return; }
  seeded = false;
  renderServer(await call("GetServerState"));
  refreshUsers();
  $("restoreStatus").textContent = "Восстановлено. Можно запускать сервер." + (res.safetyPath ? " Текущие файлы: " + res.safetyPath : "");
});

/* ---------- состояние сервера ---------- */
function renderServer(st) {
  if (!st) return;
  $("srvPort").textContent = st.port;
  $("srvPortFree").textContent = st.portFree ? "да" : "нет";
  $("srvUsers").textContent = st.users || 0;
  $("srvDir").textContent = st.dataDir || "-";
  $("srvFingerprint").textContent = st.fingerprint || "-";
  $("srvCfgPath").textContent = st.configPath ? ("Файл настроек: " + st.configPath) : "";

  const running = !!st.running;
  const url = st.url || "";
  $("srvAddr").textContent = url || "—";
  $("srvAddr").title = url || "";
  $("srvCopyAddr").disabled = !url;
  $("srvPairBtn").disabled = !running || !(st.users > 0);
  $("backupRestore").disabled = running;
  $("backupRestore").title = running ? "Сначала остановите сервер" : "";
  $("srvTlsPill").style.display = st.tls ? "" : "none";

  const dot = $("srvDot");
  dot.className = "dot";
  if (running) { dot.classList.add("on"); $("srvStatusText").textContent = "работает"; }
  else if (!st.portFree) { dot.classList.add("busy"); $("srvStatusText").textContent = "порт занят"; }
  else { dot.classList.add("off"); $("srvStatusText").textContent = "остановлен"; }

  serverRunning = running;
  const tg = $("srvToggle");
  tg.textContent = running ? "Остановить" : "Запустить";
  tg.className = running ? "btn" : "btn primary";

  if (st.error) { $("srvErr").style.color = "#e5484d"; $("srvErr").textContent = st.error; }
  else { $("srvErr").textContent = ""; }

  if (!seeded) {
    $("srvPortInput").value = st.port;
    $("srvQuotaInput").value = st.quotaGB || 0;
    $("srvVersionsDays").value = (st.versionsDays !== undefined ? st.versionsDays : 30);
    $("srvVersionsCount").value = (st.versionsCount !== undefined ? st.versionsCount : 10);
    $("srvTrashDays").value = (st.trashDays !== undefined ? st.trashDays : 30);
    $("srvOrphanDays").value = (st.orphanDays !== undefined ? st.orphanDays : 0);
    $("srvDirInput").value = st.dataDir || "";
    $("srvTLS").checked = !!st.tls;
    seeded = true;
  }
}

/* ---------- пользователи ---------- */
function renderUsers(users) {
  const tb = $("usrRows");
  if (!users || users.length === 0) {
    tb.innerHTML = '<tr><td colspan="7" class="empty">нет пользователей - создайте первого</td></tr>';
    return;
  }
  tb.innerHTML = "";
  users.forEach((u) => {
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + escapeHtml(u.name) + "</td><td>" + escapeHtml(u.id) + "</td><td>" +
      u.tokenCount + "</td><td>" + u.fileCount + "</td><td>" + fmtSize(u.usedBytes || 0) + "</td><td>" +
      (u.enabled ? '<span style="color:#1aae39">активен</span>' : '<span style="color:#c62a2f">отключён</span>') +
      '</td><td><button class="del" data-act="sel">Открыть</button></td>';
    tr.querySelector('[data-act="sel"]').addEventListener("click", () => selectUser(u));
    tb.appendChild(tr);
  });
}

function showTokenBox(tok) {
  $("tokenBox").classList.add("show");
  $("tokenValue").textContent = tok;
  $("tokenBox").scrollIntoView({ behavior: "smooth", block: "center" });
}

function tokenButton(text, cls, onClick) {
  const b = document.createElement("button");
  b.className = cls;
  b.textContent = text;
  b.addEventListener("click", onClick);
  return b;
}

function renderTokens(tokens) {
  const tb = $("tokenRows");
  if (!tokens || tokens.length === 0) {
    tb.innerHTML = '<tr><td colspan="5" class="empty">нет токенов</td></tr>';
    return;
  }
  tb.innerHTML = "";
  tokens.forEach((t) => {
    const tr = document.createElement("tr");
    const name = document.createElement("td");
    name.textContent = t.name;
    tr.appendChild(name);

    const tokTd = document.createElement("td");
    tokTd.className = "token-cell";
    if (t.available && t.token) {
      const val = document.createElement("span");
      val.className = "token-value";
      val.textContent = "••••••••";
      const eye = tokenButton("Показать", "btn small ghost", () => {
        const shown = val.dataset.shown === "1";
        val.textContent = shown ? "••••••••" : t.token;
        val.dataset.shown = shown ? "0" : "1";
        eye.textContent = shown ? "Показать" : "Скрыть";
      });
      const copy = tokenButton("Копировать", "btn small", async () => {
        await call("CopyToClipboard", t.token);
        flash("Токен скопирован", true);
      });
      tokTd.append(val, eye, copy);
    } else {
      const val = document.createElement("span");
      val.className = "token-missing";
      val.textContent = "недоступен";
      val.title = "Токен выдан до обновления: в реестре хранится только хэш";
      const re = tokenButton("Перевыпустить", "btn small", async () => {
        if (!confirm('Перевыпустить токен "' + t.name + '"? Старый токен перестанет работать.')) return;
        const tok = await call("ReissueToken", selectedUser.id, t.id);
        if (tok) {
          showTokenBox(tok);
          loadUserDetails();
          refreshUsers();
        } else {
          flash("Не удалось перевыпустить токен", false);
        }
      });
      tokTd.append(val, re);
    }
    tr.appendChild(tokTd);

    const created = document.createElement("td");
    created.textContent = t.createdAt || "-";
    tr.appendChild(created);

    const seen = document.createElement("td");
    seen.textContent = t.lastSeen || "-";
    tr.appendChild(seen);

    const act = document.createElement("td");
    act.appendChild(tokenButton("Имя", "btn small ghost", async () => {
      const name = prompt("Новое имя устройства:", t.name);
      if (name === null) return;
      if (!name.trim()) { flash("Имя устройства не может быть пустым", false); return; }
      await call("RenameToken", selectedUser.id, t.id, name.trim());
      loadUserDetails();
      refreshUsers();
    }));
    act.appendChild(tokenButton("Отозвать", "del", async () => {
      if (!confirm('Отозвать токен "' + t.name + '"?')) return;
      await call("RevokeToken", selectedUser.id, t.id);
      loadUserDetails();
      refreshUsers();
    }));
    tr.appendChild(act);
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

/* ---------- файлы ---------- */
function updateFileUserSelect(users) {
  const sel = $("fileUserSelect");
  if (!users || users.length === 0) {
    filesUserID = null;
    filesUserName = "";
    sel.disabled = true;
    sel.innerHTML = '<option value="">нет пользователей</option>';
    usersSignature = "";
    return;
  }
  const sig = users.map((u) => u.id + ":" + u.name).join("|");
  if (sig !== usersSignature) {
    sel.innerHTML = "";
    users.forEach((u) => {
      const o = document.createElement("option");
      o.value = u.id;
      o.textContent = u.name;
      sel.appendChild(o);
    });
    usersSignature = sig;
  }
  sel.disabled = false;
  if (!filesUserID || !users.some((u) => u.id === filesUserID)) {
    filesUserID = users[0].id;
    filesUserName = users[0].name;
  } else {
    filesUserName = (users.find((u) => u.id === filesUserID) || {}).name || filesUserName;
  }
  sel.value = filesUserID;
}

function markSelectedFile() {
  document.querySelectorAll(".file-row").forEach((row) => {
    row.classList.toggle("selected", !!selectedFile && row.dataset.name === selectedFile.name);
  });
}

function updateFileRow(row, f, transfer) {
  row.dataset.name = f.name;
  row.classList.toggle("has-transfer", !!transfer);
  const name = row.querySelector(".file-name");
  if (name) { name.textContent = f.name; name.title = f.name; }
  const size = row.querySelector(".file-size");
  if (size) size.textContent = fmtSize(f.size);
  const mod = row.querySelector(".file-mod");
  if (mod) { mod.textContent = fmtDate(f.modified); mod.title = f.modified || ""; }
  const pct = transfer && typeof transfer.pct === "number" ? Math.max(0, Math.min(100, transfer.pct)) : 0;
  row.style.setProperty("--pct", pct + "%");
  row.title = transfer ? (pct + "% " + (transfer.dir === "up" ? "приём" : "отдача")) : "";
}

function buildFileRow(f) {
  const tr = document.createElement("tr");
  tr.className = "file-row";
  tr.dataset.name = f.name;
  tr.innerHTML = '<td class="file-name-cell"><div class="file-name"></div></td>' +
    '<td class="file-size"></td><td class="file-mod"></td>' +
    '<td><button class="btn small ghost" data-act="history">История</button><button class="del" data-act="del">Удалить</button></td>';
  tr.querySelector('[data-act="history"]').addEventListener("click", (ev) => {
    ev.stopPropagation();
    showHistory(filesUserID, f.name);
  });
  tr.querySelector('[data-act="del"]').addEventListener("click", async (ev) => {
    ev.stopPropagation();
    if (!confirm('Удалить "' + f.name + '" у пользователя ' + filesUserName + '?')) return;
    await call("DeleteUserFile", filesUserID, f.name);
    if (selectedFile && selectedFile.name === f.name) clearHistory();
    renderTree();
    refreshUsers();
  });
  tr.addEventListener("click", (ev) => {
    if (ev.target.closest("button")) return;
    showHistory(filesUserID, f.name);
  });
  return tr;
}

function renderFiles(files, transfers) {
  const box = $("fileRows");
  const scroll = $("fileScroll");
  const scrollTop = scroll ? scroll.scrollTop : 0;
  const list = (files || []).slice().sort((a, b) => a.name.localeCompare(b.name, "ru", { sensitivity: "base" }));
  const trMap = {};
  (transfers || []).forEach((t) => { trMap[t.name] = t; });

  if (list.length === 0) {
    box.innerHTML = '<tr class="empty-row"><td colspan="4" class="empty">файлов нет</td></tr>';
    $("filesCount").textContent = "0 файлов";
    $("filesSub").textContent = filesUserName ? (filesUserName + " · 0 файлов") : "файлов нет";
    clearHistory();
    if (scroll) scroll.scrollTop = 0;
    return;
  }

  const existing = new Map();
  box.querySelectorAll("tr[data-name]").forEach((tr) => existing.set(tr.dataset.name, tr));
  const names = new Set(list.map((f) => f.name));
  existing.forEach((tr, name) => {
    if (!names.has(name)) { tr.remove(); existing.delete(name); }
  });
  box.querySelectorAll(".empty-row").forEach((tr) => tr.remove());

  let prev = null;
  list.forEach((f) => {
    let row = existing.get(f.name);
    if (!row) {
      row = buildFileRow(f);
      existing.set(f.name, row);
    }
    updateFileRow(row, f, trMap[f.name]);
    const expected = prev ? prev.nextSibling : box.firstChild;
    if (row !== expected) box.insertBefore(row, expected);
    prev = row;
  });

  const total = list.reduce((sum, f) => sum + (f.size || 0), 0);
  $("filesCount").textContent = fmtFiles(list.length) + " · " + fmtSize(total);
  $("filesSub").textContent = (filesUserName || "—") + " · " + fmtFiles(list.length);
  markSelectedFile();
  if (scroll) scroll.scrollTop = scrollTop;
  if (selectedFile && selectedFile.userID === filesUserID && !names.has(selectedFile.name)) clearHistory();
}

async function renderTree() {
  if (document.hidden || await call("WindowHidden")) return;
  const users = (await call("ListUsers")) || [];
  updateFileUserSelect(users);
  if (users.length === 0) {
    $("fileRows").innerHTML = '<tr class="empty-row"><td colspan="4" class="empty">нет пользователей</td></tr>';
    $("filesCount").textContent = "0 файлов";
    $("filesSub").textContent = "нет пользователей";
    clearHistory();
    return;
  }
  const files = (await call("UserFiles", filesUserID)) || [];
  const transfers = (await call("GetTransfers")) || [];
  renderFiles(files, transfers);
}

function renderHistoryRows(items, isTrash) {
  const tb = $("historyRows");
  tb.innerHTML = "";
  if (!items || items.length === 0) {
    tb.innerHTML = '<tr><td colspan="4" class="empty">' + (isTrash ? "корзина пуста" : "нет версий") + '</td></tr>';
    return;
  }
  items.forEach((it) => {
    const tr = document.createElement("tr");
    const label = it.kind === "trash" ? "корзина" : ("v" + it.version);
    tr.innerHTML = "<td>" + escapeHtml(label) + "</td><td>" + fmtSize(it.size) + "</td><td title=\"" +
      escapeHtml(it.createdAt || "") + "\">" + escapeHtml(fmtDate(it.createdAt)) +
      '</td><td><button class="btn small">Восстановить</button></td>';
    tr.querySelector("button").addEventListener("click", async () => {
      await call(isTrash ? "RestoreTrash" : "RestoreHistory", historyContext.userID, it.id);
      await renderTree();
      if (historyContext) showHistory(historyContext.userID, historyContext.name);
      refreshUsers();
    });
    tb.appendChild(tr);
  });
}

async function showHistory(userID, name) {
  if (!userID) return;
  const isTrash = !name;
  historyContext = { userID, name };
  selectedFile = isTrash ? null : { userID, name };
  markSelectedFile();
  const items = isTrash ? (await call("Trash", userID)) : (await call("History", userID, name));
  $("historyTitle").textContent = isTrash ? ("Корзина · " + filesUserName) : name;
  $("historyMeta").textContent = isTrash ? "удалённые файлы" : fmtVersions((items || []).length);
  renderHistoryRows(items || [], isTrash);
}

function clearHistory() {
  selectedFile = null;
  historyContext = null;
  markSelectedFile();
  $("historyTitle").textContent = "—";
  $("historyMeta").textContent = "Выберите файл в списке";
  $("historyRows").innerHTML = '<tr><td colspan="4" class="empty">История появится после выбора файла</td></tr>';
}

/* ---------- конфликты ---------- */
async function renderConflicts() {
  const items = (await call("Conflicts")) || [];
  const tb = $("confRows");
  tb.innerHTML = "";
  if (items.length === 0) {
    tb.innerHTML = '<tr><td colspan="5" class="empty">конфликтов нет</td></tr>';
    return;
  }
  items.forEach((c) => {
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + escapeHtml(c.userName || c.userId) + "</td><td>" + escapeHtml(c.name) +
      (c.original ? '<div class="meta">оригинал: ' + escapeHtml(c.original) + '</div>' : '') +
      "</td><td>" + fmtSize(c.size) + "</td><td>" + escapeHtml(c.modified) +
      '</td><td><button class="btn small" data-act="server">Серверная</button>' +
      '<button class="btn small" data-act="conflict">Конфликтная</button>' +
      '<button class="del">Удалить</button></td>';
    tr.querySelector('[data-act="server"]').addEventListener("click", async () => {
      if (!confirm('Оставить серверную версию и удалить конфликтную копию "' + c.name + '"?')) return;
      await call("ResolveConflict", c.userId, c.name, "server");
      renderConflicts(); renderTree();
    });
    tr.querySelector('[data-act="conflict"]').addEventListener("click", async () => {
      if (!confirm('Заменить оригинал конфликтной копией "' + c.name + '"?')) return;
      await call("ResolveConflict", c.userId, c.name, "conflict");
      renderConflicts(); renderTree();
    });
    tr.querySelector(".del").addEventListener("click", async () => {
      if (!confirm('Удалить конфликтную копию "' + c.name + '"?')) return;
      await call("DeleteUserFile", c.userId, c.name);
      renderConflicts(); renderTree();
    });
    tb.appendChild(tr);
  });
}

/* ---------- действия ---------- */
function form() { return {
  port: parseInt($("srvPortInput").value, 10) || 8090,
  dir: $("srvDirInput").value.trim(),
  tls: $("srvTLS").checked,
  quota: parseInt($("srvQuotaInput").value, 10) || 0,
  versionsDays: parseInt($("srvVersionsDays").value, 10) || 0,
  versionsCount: parseInt($("srvVersionsCount").value, 10) || 0,
  trashDays: parseInt($("srvTrashDays").value, 10) || 0,
  orphanDays: parseInt($("srvOrphanDays").value, 10) || 0,
}; }
function flash(msg, ok) { $("srvErr").style.color = ok ? "#1aae39" : "#e5484d"; $("srvErr").textContent = msg; }

$("srvToggle").addEventListener("click", async () => {
  if (serverRunning) { renderServer(await call("StopServer")); refresh(); return; }
  const f = form();
  renderServer(await call("StartServer", f.port, f.dir));
  refresh();
});

$("srvRelease").addEventListener("click", async () => {
  const f = form();
  const st = await call("GetServerState");
  if (st && st.running) {
    renderServer(await call("StopServer"));
    refresh();
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
  renderServer(res);
  refresh();
  flash(res && res.error ? res.error : "процесс завершён, порт освобождён", !(res && res.error));
});

$("srvCopyAddr").addEventListener("click", async () => {
  const st = await call("GetServerState");
  if (!st || !st.running || !st.url) { flash("сервер не запущен — адрес недоступен", false); return; }
  await call("CopyToClipboard", st.url);
  flash("адрес скопирован", true);
});

$("srvCopyFP").addEventListener("click", async () => {
  const st = await call("GetServerState");
  if (!st || !st.fingerprint) { flash("отпечаток ещё не создан — включите HTTPS и сохраните настройки", false); return; }
  await call("CopyToClipboard", st.fingerprint);
  flash("отпечаток скопирован", true);
});

function closePairModal() {
  $("pairModal").hidden = true;
  $("pairError").textContent = "";
  $("pairResult").hidden = true;
  $("pairCode").value = "";
}

async function openPairModal() {
  $("pairModal").hidden = false;
  $("pairError").textContent = "";
  $("pairError").style.color = "var(--crit)";
  $("pairResult").hidden = true;
  $("pairCode").value = "";
  const users = (await call("ListUsers")) || [];
  const enabled = users.filter((u) => u && u.enabled);
  const sel = $("pairUser");
  sel.innerHTML = "";
  if (enabled.length === 0) {
    const opt = document.createElement("option");
    opt.value = "";
    opt.textContent = "Нет активных пользователей";
    sel.appendChild(opt);
    $("pairCreate").disabled = true;
    $("pairError").textContent = "Сначала создайте пользователя во вкладке «Пользователи».";
    return;
  }
  enabled.forEach((u) => {
    const opt = document.createElement("option");
    opt.value = u.id;
    opt.textContent = u.name + " (" + u.id + ")";
    sel.appendChild(opt);
  });
  $("pairCreate").disabled = false;
}

$("srvPairBtn").addEventListener("click", openPairModal);
$("pairClose").addEventListener("click", closePairModal);
$("pairModal").addEventListener("click", (e) => { if (e.target.id === "pairModal") closePairModal(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && !$("pairModal").hidden) closePairModal(); });
$("pairCreate").addEventListener("click", async () => {
  const uid = $("pairUser").value;
  if (!uid) { $("pairError").textContent = "Выберите пользователя."; return; }
  $("pairCreate").disabled = true;
  const res = await call("CreateConnectionCode", uid, $("pairDeviceName").value.trim());
  $("pairCreate").disabled = false;
  if (!res) { $("pairError").textContent = "Не удалось создать код."; return; }
  if (res.error) { $("pairError").style.color = "var(--crit)"; $("pairError").textContent = res.error; return; }
  $("pairError").textContent = "";
  $("pairCode").value = res.code || "";
  $("pairResult").hidden = false;
  $("pairExpiry").textContent = "Действует до " + (res.expiresAt || "—") + " (одноразовый)";
  $("pairCode").focus();
  $("pairCode").select();
});
$("pairCopy").addEventListener("click", async () => {
  const code = $("pairCode").value;
  if (!code) return;
  await call("CopyToClipboard", code);
  $("pairExpiry").textContent = "Код скопирован. " + $("pairExpiry").textContent;
});
$("pairSaveFile").addEventListener("click", async () => {
  const code = $("pairCode").value;
  if (!code) return;
  const res = await call("ExportConnectionCode", code);
  if (!res) { $("pairError").textContent = "Не удалось сохранить файл."; return; }
  if (res.error) {
    if (res.error !== "сохранение отменено") { $("pairError").style.color = "var(--crit)"; $("pairError").textContent = res.error; }
    return;
  }
  $("pairError").style.color = "var(--ok)";
  $("pairError").textContent = "Файл сохранён: " + (res.path || "");
});

$("srvSave").addEventListener("click", async () => {
  const f = form();
  const st = await call("SaveSettings", f.port, f.dir, f.tls, f.quota, f.versionsDays, f.versionsCount, f.trashDays, f.orphanDays);
  renderServer(st);
  if (st && !st.error) flash("настройки сохранены", true);
});
$("srvPick").addEventListener("click", async () => {
  const d = await call("PickDirectory");
  if (d) $("srvDirInput").value = d;
});
$("srvAutostart").addEventListener("change", async (e) => {
  await call("SetAutostart", e.target.checked);
  flash(e.target.checked ? "программа будет запускаться при входе в Windows" : "запуск программы при входе выключен", true);
});
$("srvAutoRun").addEventListener("change", async (e) => {
  await call("SetAutoRun", e.target.checked);
  flash(e.target.checked ? "сервер будет стартовать при открытии программы" : "сервер нужно будет запускать вручную", true);
});

/* ---------- файлы: обработчики ---------- */
$("treeRefresh").addEventListener("click", () => renderTree());
$("fileUserSelect").addEventListener("change", (e) => {
  filesUserID = e.target.value || null;
  filesUserName = e.target.options[e.target.selectedIndex].textContent;
  selectedFile = null;
  historyContext = null;
  clearHistory();
  renderTree();
});
$("filesTrash").addEventListener("click", () => {
  if (!filesUserID) { flash("нет выбранного пользователя", false); return; }
  showHistory(filesUserID, "");
});
$("historyClose").addEventListener("click", () => clearHistory());

/* ---------- пользователи: действия ---------- */
$("usrRefresh").addEventListener("click", () => refreshUsers());
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
    showTokenBox(tok);
    loadUserDetails(); refreshUsers();
  } else { flash("пользователь создан, но токен выдать не удалось", false); }
});

$("uToken").addEventListener("click", async () => {
  if (!selectedUser) return;
  const dev = prompt("Название устройства (например, «Рабочий ПК»):", "Устройство");
  if (dev === null) return;
  const tok = await call("IssueToken", selectedUser.id, dev.trim());
  if (tok) {
    showTokenBox(tok);
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
  $("tokenRows").innerHTML = '<tr><td colspan="5" class="empty">-</td></tr>';
  refreshUsers();
});

async function refreshUsers() {
  const users = (await call("ListUsers")) || [];
  renderUsers(users);
  return users;
}

async function refresh() {
  if (document.hidden) return;
  if (await call("WindowHidden")) return;
  const [st, logs] = await Promise.all([call("GetServerState"), call("GetLogs")]);
  renderServer(st);
  renderLogs(logs);
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

window.addEventListener("DOMContentLoaded", () => {
  refresh();
  refreshUsers();
  setInterval(() => { if (!document.hidden) refresh(); }, 1500);
  setInterval(() => { if (!document.hidden && currentTab === "tab-files") renderTree(); }, 3000);
  setInterval(() => { if (!document.hidden && currentTab === "tab-conflicts") renderConflicts(); }, 3000);
});
document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
