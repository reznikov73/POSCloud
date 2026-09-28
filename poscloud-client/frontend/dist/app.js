/* POSCloud Client — UI */
const $ = (id) => document.getElementById(id);
let seeded = false;
let ignoreLoaded = false;
let autostartLoaded = false;
let autoRunLoaded = false;
let shotsLoaded = false;

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
  document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x.dataset.tab === id));
  document.querySelectorAll(".pane").forEach((x) => x.classList.toggle("active", x.id === id));
  if (id === "tab-log") refreshLogs();
  if (id === "tab-files") refreshFiles();
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

/* ---------- состояние клиента ---------- */
function renderClient(st) {
  if (!st) return;
  $("cliUrl").textContent = st.serverURL || "-";
  $("cliDir").textContent = st.localDir || "-";
  $("cliInt").textContent = st.interval + " с";
  $("cliLast").textContent = st.lastSync || "-";
  $("cliCount").textContent = st.count;
  $("cliOnline").textContent = st.running ? (st.online ? "есть" : "нет") : "-";
  $("cliCfgPath").textContent = st.configPath ? ("Файл настроек: " + st.configPath) : "";

  const dot = $("cliDot");
  dot.className = "dot";
  if (st.running && st.online) { dot.classList.add("on"); $("cliStatusText").textContent = "работает"; }
  else if (st.running) { dot.classList.add("busy"); $("cliStatusText").textContent = "работает, но нет связи с сервером"; }
  else { dot.classList.add("off"); $("cliStatusText").textContent = "остановлен"; }

  // Одна кнопка меняет назначение: запускает или останавливает синхронизацию.
  clientRunning = !!st.running;
  const tg = $("cliToggle");
  tg.textContent = st.running ? "Остановить" : "Запустить";
  tg.className = st.running ? "btn" : "btn primary";

  if (st.error) { $("cliErr").style.color = "#e5484d"; $("cliErr").textContent = st.error; }
  else if ($("cliErr").dataset.sticky !== "1") { $("cliErr").textContent = ""; }

  if (!seeded) {
    $("cliHostInput").value = st.host;
    $("cliPortInput").value = st.port;
    $("cliIntInput").value = st.interval;
    $("cliTokenInput").value = st.token || "";
    $("cliDirInput").value = st.localDir || "";
    seeded = true;
  }
}

function renderFiles(files, trs) {
  const tb = $("cliFiles");
  const trMap = {};
  (trs || []).forEach((t) => { trMap[t.name] = t; });
  const list = (files || []).slice();
  const have = new Set(list.map((f) => f.name));
  (trs || []).forEach((t) => { if (!have.has(t.name)) list.push({ name: t.name, size: -1, modified: "", pending: true }); });
  list.sort((a, b) => {
    const ua = trMap[a.name] ? 0 : 1;
    const ub = trMap[b.name] ? 0 : 1;
    if (ua !== ub) return ua - ub;
    return a.name < b.name ? -1 : (a.name > b.name ? 1 : 0);
  });
  if (list.length === 0) { tb.innerHTML = '<tr><td colspan="4" class="empty">пусто</td></tr>'; return; }
  tb.innerHTML = "";
  list.forEach((f) => {
    const t = trMap[f.name];
    const nameCell = t
      ? escapeHtml(f.name) + '<div class="pbar"><div class="pfill" style="width:' + t.pct + '%"></div></div><div class="pct">' + t.pct + '% ' + (t.dir === "up" ? "↑ выгрузка" : "↓ загрузка") + "</div>"
      : escapeHtml(f.name);
    const sizeCell = f.pending ? "—" : fmtSize(f.size);
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + nameCell + "</td><td>" + sizeCell + "</td><td>" +
      escapeHtml(f.modified) + '</td><td><button class="del">Удалить</button></td>';
    tr.querySelector(".del").addEventListener("click", async () => {
      if (!confirm('Удалить "' + f.name + '"?')) return;
      await call("DeleteLocalFile", f.name); refresh();
    });
    tb.appendChild(tr);
  });
}

function form() {
  return {
    host: $("cliHostInput").value.trim(),
    port: parseInt($("cliPortInput").value, 10) || 8090,
    dir: $("cliDirInput").value.trim(),
    itv: parseInt($("cliIntInput").value, 10) || 3,
    token: $("cliTokenInput").value.trim(),
  };
}
function flash(msg, ok) {
  $("cliErr").dataset.sticky = "1";
  $("cliErr").style.color = ok ? "#1aae39" : "#e5484d";
  $("cliErr").textContent = msg;
}

let clientRunning = false;
$("cliToggle").addEventListener("click", async () => {
  if (clientRunning) { renderClient(await call("StopSync")); return; }
  const f = form();
  const st = await call("StartSync", f.host, f.port, f.dir, f.itv, f.token);
  $("cliErr").dataset.sticky = "";
  renderClient(st);
});
$("cliSave").addEventListener("click", async () => {
  const f = form();
  const st = await call("SaveSettings", f.host, f.port, f.dir, f.itv, f.token);
  renderClient(st);
  flash("настройки сохранены", true);
});
$("cliSync").addEventListener("click", async () => { await call("SyncNow"); });
$("cliIgnoreSave").addEventListener("click", async () => {
  await call("SaveIgnore", $("cliIgnoreInput").value);
  flash("исключения сохранены", true);
});

/* ---------- исключения: наборы, выбор из папки, импорт/экспорт, предпросмотр ---------- */
let ignorePresets = [];
let pickerRel = "";
let pickerSel = new Set();
let pickerItems = [];

function ignoreText() { return $("cliIgnoreInput").value; }

async function refreshIgnoreStats() {
  const st = await call("IgnorePreview", ignoreText());
  const el = $("cliIgnoreStats");
  if (!st || !st.total) { el.textContent = ""; return; }
  el.textContent = "Под исключения попадёт " + st.matched + " из " + st.total + " файлов (" +
    fmtSize(st.matchedBytes) + " из " + fmtSize(st.totalBytes) + ")";
}

async function renderIgnorePresets() {
  const box = $("cliPresets");
  if (!ignorePresets.length) { box.innerHTML = ""; return; }
  const applied = new Set((await call("AppliedIgnorePresets", ignoreText())) || []);
  box.innerHTML = "";
  ignorePresets.forEach((p) => {
    const lab = document.createElement("label");
    lab.className = "preset";
    lab.title = "Добавит строки:\n" + (p.patterns || []).join("\n");
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.checked = applied.has(p.id);
    cb.addEventListener("change", async (e) => {
      const next = await call("ApplyIgnorePreset", ignoreText(), p.id, e.target.checked);
      if (typeof next === "string") { $("cliIgnoreInput").value = next; }
      await renderIgnorePresets();
      await refreshIgnoreStats();
    });
    const sp = document.createElement("span");
    sp.textContent = p.title;
    lab.appendChild(cb);
    lab.appendChild(sp);
    box.appendChild(lab);
  });
}

async function renderPicker() {
  pickerItems = (await call("ListSyncEntries", pickerRel)) || [];
  $("cliPickerPath").textContent = pickerRel ? pickerRel : "корень папки синхронизации";
  const box = $("cliPickerList");
  box.innerHTML = "";
  if (!pickerItems.length) { box.innerHTML = '<div class="empty">пусто</div>'; return; }
  pickerItems.forEach((it) => {
    const row = document.createElement("div");
    row.className = "picker-row";
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.checked = pickerSel.has(it.path);
    cb.addEventListener("change", () => {
      if (cb.checked) { pickerSel.add(it.path); } else { pickerSel.delete(it.path); }
    });
    const nm = document.createElement("span");
    nm.className = "picker-name" + (it.isDir ? " dir" : "");
    nm.textContent = (it.isDir ? "▸ " : "") + it.name;
    row.appendChild(cb);
    row.appendChild(nm);
    if (it.isDir) {
      nm.title = "открыть папку";
      nm.addEventListener("click", async () => { pickerRel = it.path; await renderPicker(); });
    } else {
      const sz = document.createElement("span");
      sz.className = "picker-size";
      sz.textContent = fmtSize(it.size);
      row.appendChild(sz);
    }
    box.appendChild(row);
  });
}

$("cliIgnorePick").addEventListener("click", async () => {
  pickerRel = "";
  pickerSel = new Set();
  $("cliIgnorePicker").hidden = false;
  await renderPicker();
});
$("cliPickerClose").addEventListener("click", () => { $("cliIgnorePicker").hidden = true; });
$("cliPickerUp").addEventListener("click", async () => {
  if (!pickerRel) return;
  const parts = pickerRel.split("/");
  parts.pop();
  pickerRel = parts.join("/");
  await renderPicker();
});
$("cliPickerAdd").addEventListener("click", async () => {
  if (!pickerSel.size) { flash("ничего не выбрано", false); return; }
  const patterns = [];
  Array.from(pickerSel).sort().forEach((p) => {
    const it = pickerItems.find((x) => x.path === p);
    // каталог исключаем вместе с содержимым
    patterns.push(it && it.isDir ? p + "/**" : p);
  });
  const next = await call("AddIgnorePatterns", ignoreText(), patterns);
  if (typeof next === "string") { $("cliIgnoreInput").value = next; }
  $("cliIgnorePicker").hidden = true;
  pickerSel = new Set();
  await renderIgnorePresets();
  await refreshIgnoreStats();
  flash("добавлено шаблонов: " + patterns.length, true);
});

$("cliIgnoreImport").addEventListener("click", async () => {
  const t = await call("ImportIgnore");
  if (typeof t === "string" && t) {
    $("cliIgnoreInput").value = t;
    await renderIgnorePresets();
    await refreshIgnoreStats();
    flash("список загружен", true);
  }
});
$("cliIgnoreExport").addEventListener("click", async () => {
  const p = await call("ExportIgnore", ignoreText());
  if (p) flash("экспортировано: " + p, true);
});
$("cliIgnoreClear").addEventListener("click", async () => {
  $("cliIgnoreInput").value = "";
  await renderIgnorePresets();
  await refreshIgnoreStats();
});
$("cliIgnoreCheck").addEventListener("click", async () => { await refreshIgnoreStats(); });

/* ---------- снимки экрана ---------- */
let shotCapture = null;   // начатый выбор области
let shotSelRect = null;   // выделение в долях от снимка
let shotDragFrom = null;  // начало протяжки

async function refreshShotState() {
  const st = await call("ShotsInfo");
  if (!st) return;
  $("shotSync").checked = !!st.sync;
  $("shotInfo").textContent = "Папка: " + st.dir + " · снимков: " + st.count;
  $("shotKeys").textContent = "Горячие клавиши: " + (st.hotkeys || "не назначены");
}

function shotImageRect() { return $("shotImage").getBoundingClientRect(); }

function drawShotSelection() {
  const box = $("shotSel");
  if (!shotSelRect) { box.hidden = true; return; }
  const r = shotImageRect();
  box.hidden = false;
  box.style.left = (r.left + shotSelRect.x * r.width) + "px";
  box.style.top = (r.top + shotSelRect.y * r.height) + "px";
  box.style.width = (shotSelRect.w * r.width) + "px";
  box.style.height = (shotSelRect.h * r.height) + "px";
}

function openShotOverlay(cap) {
  shotCapture = cap;
  shotSelRect = null;
  shotDragFrom = null;
  $("shotImage").src = cap.preview;
  $("shotSel").hidden = true;
  $("shotApply").disabled = true;
  $("shotHint").textContent = "Выделите область мышью (" + cap.width + "×" + cap.height + ")";
  $("shotOverlay").hidden = false;
}

function closeShotOverlay() {
  shotCapture = null;
  shotSelRect = null;
  shotDragFrom = null;
  $("shotOverlay").hidden = true;
}

$("shotStage").addEventListener("mousedown", (e) => {
  if (!shotCapture) return;
  const r = shotImageRect();
  if (r.width <= 0 || r.height <= 0) return;
  if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) return;
  shotDragFrom = { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height };
  shotSelRect = { x: shotDragFrom.x, y: shotDragFrom.y, w: 0, h: 0 };
  drawShotSelection();
});
window.addEventListener("mousemove", (e) => {
  if (!shotDragFrom || !shotCapture) return;
  const r = shotImageRect();
  const cx = Math.min(Math.max((e.clientX - r.left) / r.width, 0), 1);
  const cy = Math.min(Math.max((e.clientY - r.top) / r.height, 0), 1);
  shotSelRect = {
    x: Math.min(shotDragFrom.x, cx),
    y: Math.min(shotDragFrom.y, cy),
    w: Math.abs(cx - shotDragFrom.x),
    h: Math.abs(cy - shotDragFrom.y),
  };
  $("shotApply").disabled = !(shotSelRect.w > 0.01 && shotSelRect.h > 0.01);
  drawShotSelection();
});
window.addEventListener("mouseup", () => { shotDragFrom = null; });

$("shotApply").addEventListener("click", async () => {
  if (!shotCapture || !shotSelRect) return;
  const path = await call("FinishRegionCapture", shotCapture.id,
    shotSelRect.x, shotSelRect.y, shotSelRect.w, shotSelRect.h);
  closeShotOverlay();
  flash(path ? ("снимок сохранён: " + path) : "не удалось сохранить снимок", !!path);
  await refreshShotState();
});

$("shotCancel").addEventListener("click", async () => {
  if (shotCapture) await call("CancelRegionCapture", shotCapture.id);
  closeShotOverlay();
});

$("shotFull").addEventListener("click", async () => {
  const path = await call("CaptureFullScreen");
  flash(path ? ("снимок сохранён: " + path) : "не удалось сделать снимок", !!path);
  await refreshShotState();
});

$("shotRegion").addEventListener("click", async () => {
  const cap = await call("StartRegionCapture");
  if (cap && cap.id) await openShotOverlay(cap);
});

$("shotWindow").addEventListener("click", async () => {
  const path = await call("CaptureActiveWindow");
  flash(path ? ("снимок сохранён: " + path) : "не удалось сделать снимок активного окна", !!path);
  await refreshShotState();
});

$("shotOpen").addEventListener("click", async () => { await call("OpenShotsFolder"); });

$("shotSync").addEventListener("change", async (e) => {
  await call("SetScreenshotSync", e.target.checked);
  await refreshShotState();
});

/* ---------- настраиваемые горячие клавиши ---------- */
const hotkeyRows = [
  { key: "region", title: "Снимок области" },
  { key: "window", title: "Снимок активного окна" },
  { key: "full", title: "Снимок всего экрана" },
];
let hotkeyCfg = { region: "", window: "", full: "" };
let hotkeyStates = [];
let hotkeyLoaded = false;

// comboFromEvent превращает нажатие в строку вида «Ctrl+Shift+1».
// null — ждём основную клавишу, пустая строка — очистить сочетание.
function comboFromEvent(e) {
  if (e.key === "Delete" || e.key === "Backspace") return "";
  const mods = [];
  if (e.ctrlKey) mods.push("Ctrl");
  if (e.altKey) mods.push("Alt");
  if (e.shiftKey) mods.push("Shift");
  const k = e.key;
  let main = "";
  if (/^[a-zA-Z]$/.test(k)) main = k.toUpperCase();
  else if (/^[0-9]$/.test(k)) main = k;
  else if (/^F([1-9]|1[0-9]|2[0-4])$/.test(k)) main = k.toUpperCase();
  else return null;
  if (!mods.length) return null;
  return mods.join("+") + "+" + main;
}

function hotkeyStateText() {
  if (!hotkeyStates.length) return "";
  return "Состояние: " + hotkeyStates.map((s) => s.title + " — " + (s.ok ? s.combo : (s.error || "не назначено"))).join("; ");
}

function renderHotkeys() {
  const box = $("hotkeyRows");
  box.innerHTML = "";
  hotkeyRows.forEach((row) => {
    const wrap = document.createElement("div");
    wrap.className = "hotkey-row";
    const title = document.createElement("span");
    title.className = "hotkey-title";
    title.textContent = row.title;
    const input = document.createElement("input");
    input.type = "text";
    input.className = "hotkey-input";
    input.value = hotkeyCfg[row.key] || "";
    input.placeholder = "не задано";
    input.readOnly = true; // вводим не текст, а само нажатие
    input.addEventListener("focus", () => call("SuspendHotkeys"));
    input.addEventListener("blur", () => call("ResumeHotkeys"));
    input.addEventListener("keydown", async (e) => {
      e.preventDefault();
      const combo = comboFromEvent(e);
      if (combo === null) return;
      input.value = combo;
      hotkeyCfg[row.key] = combo;
      await saveHotkeys();
    });
    const status = document.createElement("span");
    const st = hotkeyStates.find((s) => s.key === row.key);
    status.className = "hotkey-status " + (st && st.ok ? "ok" : "bad");
    status.textContent = st ? (st.ok ? "назначено" : (st.error || "")) : "";
    wrap.appendChild(title);
    wrap.appendChild(input);
    wrap.appendChild(status);
    box.appendChild(wrap);
  });
  $("hotkeyHint").textContent = hotkeyStateText();
}

async function saveHotkeys() {
  const states = await call("SetHotkeys", hotkeyCfg);
  if (Array.isArray(states)) hotkeyStates = states;
  renderHotkeys();
}

async function loadHotkeys() {
  const cfg = await call("Hotkeys");
  if (cfg) hotkeyCfg = { region: cfg.region || "", window: cfg.window || "", full: cfg.full || "" };
  const states = await call("HotkeyStates");
  if (Array.isArray(states)) hotkeyStates = states;
  renderHotkeys();
}

$("hotkeyReset").addEventListener("click", async () => {
  hotkeyCfg = { region: "Ctrl+Shift+1", window: "Ctrl+Shift+2", full: "Ctrl+Shift+3" };
  await saveHotkeys();
});
$("cliAutostart").addEventListener("change", async (e) => {
  await call("SetAutostart", e.target.checked);
  flash(e.target.checked ? "программа будет запускаться при входе в Windows" : "запуск программы при входе выключен", true);
});
$("cliAutoRun").addEventListener("change", async (e) => {
  await call("SetAutoRun", e.target.checked);
  flash(e.target.checked ? "синхронизация будет начинаться при открытии программы" : "синхронизацию нужно будет запускать вручную", true);
});
$("cliPick").addEventListener("click", async () => { const d = await call("PickDirectory"); if (d) $("cliDirInput").value = d; });
$("cliTest").addEventListener("click", async () => {
  const f = form();
  const msg = await call("TestConnection", f.host, f.port, f.token);
  flash(msg || "Ошибка проверки", !!(msg && msg.indexOf("OK") === 0));
});

async function refreshFiles() { renderFiles((await call("LocalFiles")) || []); }

async function refresh() {
  const [st, files, logs, trs] = await Promise.all([call("GetClientState"), call("LocalFiles"), call("GetLogs"), call("GetTransfers")]);
  renderClient(st); renderFiles(files, trs || []); renderLogs(logs);
  $("pulse").className = "pulse" + (st && st.running ? " on" : "");
  if (!ignoreLoaded) {
    const ig = await call("GetIgnore");
    if (typeof ig === "string") {
      $("cliIgnoreInput").value = ig;
      ignoreLoaded = true;
      ignorePresets = (await call("IgnorePresets")) || [];
      await renderIgnorePresets();
      await refreshIgnoreStats();
    }
  }
  if (!autostartLoaded) {
    const au = await call("AutostartEnabled");
    if (typeof au === "boolean") { $("cliAutostart").checked = au; autostartLoaded = true; }
  }
  if (!autoRunLoaded) {
    const ar = await call("AutoRunEnabled");
    if (typeof ar === "boolean") { $("cliAutoRun").checked = ar; autoRunLoaded = true; }
  }
  if (!shotsLoaded) {
    await refreshShotState();
    shotsLoaded = true;
  }
  if (!hotkeyLoaded) {
    await loadHotkeys();
    hotkeyLoaded = true;
  }
  // выбор области, начатый из меню трея
  if ($("shotOverlay").hidden) {
    const cap = await call("PendingRegionCapture");
    if (cap && cap.id) await openShotOverlay(cap);
  }
}

window.addEventListener("DOMContentLoaded", () => { refresh(); setInterval(refresh, 500); });
