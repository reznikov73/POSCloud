/* POSCloud Client — UI */
const $ = (id) => document.getElementById(id);
let seeded = false;
let ignoreLoaded = false;
let autostartLoaded = false;
let autoRunLoaded = false;
let shotsLoaded = false;
let editAfterLoaded = false;
let syncLoaded = false;

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
  if (id === "tab-sync") renderSync();
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
  else if (st.running) { dot.classList.add("busy"); $("cliStatusText").textContent = st.error === "токен отозван" ? "токен отозван" : "работает, но нет связи с сервером"; }
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
    $("cliTLS").checked = !!st.tls;
    $("cliFingerprintInput").value = st.fingerprint || "";
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
    tls: $("cliTLS").checked,
    fingerprint: $("cliFingerprintInput").value.trim(),
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
  const st = await call("StartSync", f.host, f.port, f.dir, f.itv, f.token, f.tls, f.fingerprint);
  $("cliErr").dataset.sticky = "";
  renderClient(st);
});
$("cliSave").addEventListener("click", async () => {
  const f = form();
  const st = await call("SaveSettings", f.host, f.port, f.dir, f.itv, f.token, f.tls, f.fingerprint);
  renderClient(st);
  flash("настройки сохранены", true);
});
$("cliPaste").addEventListener("click", async () => {
  const text = await call("ReadClipboard");
  if (!text || !String(text).trim()) { flash("Буфер обмена пуст", false); return; }
  $("cliPairCode").value = String(text).trim();
  flash("Код вставлен — нажмите «Подключить»", true);
});
$("cliPairFile").addEventListener("click", async () => {
  const text = await call("PickConnectionCodeFile");
  if (!text) { flash("Файл не выбран или пуст", false); return; }
  $("cliPairCode").value = String(text).trim();
  flash("Код загружен — нажмите «Подключить»", true);
});
$("cliPair").addEventListener("click", async () => {
  const code = $("cliPairCode").value.trim();
  if (!code) { flash("Вставьте код подключения", false); return; }
  $("cliPair").disabled = true;
  const st = await call("PairConnection", code);
  $("cliPair").disabled = false;
  if (!st) { flash("Не удалось выполнить подключение", false); return; }
  seeded = false;
  renderClient(st);
  if (st.error) { flash(st.error, false); return; }
  $("cliPairCode").value = "";
  flash("Подключено: " + (st.serverURL || ""), true);
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

/* ---------- синхронизация: папки сервера и правила ---------- */
const folderModes = [
  { id: "both", title: "В обе стороны" },
  { id: "up", title: "Только отправлять" },
  { id: "down", title: "Только получать" },
  { id: "off", title: "Не синхронизировать" },
];
function syncStatusOf(n) {
  if (n.onlyRule) return { text: "Правило без папки", cls: "offf" };
  if (n.new) return { text: "Новая — выберите режим", cls: "pend" };
  if (n.mode === "off") return { text: n.local ? "Есть на этом ПК" : "Не синхронизируется", cls: "offf" };
  if (n.local) return { text: "Есть на этом ПК", cls: "okk" };
  if (n.mode === "both" || n.mode === "down") return { text: "Будет скачана", cls: "" };
  return { text: "Нет на этом ПК", cls: "offf" };
}

async function renderSync() {
  const box = $("syncList");
  if (!box) return;
  const free = await call("RulesFreeEnabled");
  if ($("rulesFree")) $("rulesFree").checked = !!free;
  if (free) {
    box.innerHTML = '<div class="empty">режим «без правил»: синхронизируется всё в обе стороны</div>';
    return;
  }
  const tree = await call("ServerFolders");
  const rows = Array.isArray(tree) ? tree : [];
  box.innerHTML = "";
  if (!rows.length) {
    box.innerHTML = '<div class="empty">' + (Array.isArray(tree) ? "папок пока нет" : "не удалось получить список с сервера (нет связи?)") + "</div>";
    return;
  }
  rows.forEach((n) => {
    const row = document.createElement("div");
    row.className = "rule-row";
    const nm = document.createElement("span");
    nm.className = "rule-path";
    nm.textContent = "▸ " + n.path;
    nm.title = "всё внутри папки синхронизируется по её режиму";
    const meta = document.createElement("span");
    meta.className = "sync-meta";
    if (n.onlyRule) {
      meta.textContent = "правило";
    } else if (n.files > 0) {
      meta.textContent = n.files + " ф. · " + fmtSize(n.bytes);
    } else if (n.local && n.locFiles > 0) {
      meta.textContent = "здесь: " + n.locFiles + " ф. · " + fmtSize(n.locBytes);
    } else {
      meta.textContent = "папка";
    }
    const stt = syncStatusOf(n);
    const badge = document.createElement("span");
    badge.className = "sync-status " + stt.cls;
    badge.textContent = stt.text;
    row.appendChild(nm);
    row.appendChild(meta);
    row.appendChild(badge);
    const sel = document.createElement("select");
    sel.className = "rule-mode";
    folderModes.forEach((m) => {
      const o = document.createElement("option");
      o.value = m.id;
      o.textContent = m.title;
      if (m.id === (n.rule || n.mode)) o.selected = true;
      sel.appendChild(o);
    });
    sel.addEventListener("change", async () => {
      const val = sel.value;
      const wasOff = !n.rule || n.mode === "off";
      const big = 50 * 1024 * 1024;
      if (wasOff && (val === "both" || val === "down") && !n.local && n.bytes >= big) {
        if (!confirm("Скачать папку «" + n.path + "»?\n\nФайлов: " + n.files + " (" + fmtSize(n.bytes) + ").")) { await renderSync(); return; }
      }
      if (wasOff && (val === "both" || val === "up") && n.local && n.locBytes >= big) {
        if (!confirm("Отправить папку «" + n.path + "»?\n\nФайлов: " + n.locFiles + " (" + fmtSize(n.locBytes) + ").")) { await renderSync(); return; }
      }
      await call("AddFolderRule", n.path, val);
      if (val === "off" && n.local && !n.onlyRule) {
        if (confirm("Удалить локальную копию папки «" + n.path + "», чтобы освободить место?\n\nДанные на сервере останутся.")) {
          await call("ClearLocalCopy", n.path);
        }
      }
      flash("режим папки сохранён", true);
      await renderSync();
    });
    row.appendChild(sel);
    if (n.local && n.mode === "off") {
      const clr = document.createElement("button");
      clr.className = "del";
      clr.textContent = "Освободить место";
      clr.title = "удалить локальные файлы папки — данные на сервере останутся";
      clr.addEventListener("click", async () => {
        if (!confirm("Удалить локальные файлы папки «" + n.path + "» с этого компьютера?\n\nДанные на сервере останутся.")) return;
        await call("ClearLocalCopy", n.path);
        flash("локальные файлы папки удалены", true);
        await renderSync();
      });
      row.appendChild(clr);
    }
    if (n.onlyRule) {
      const rm = document.createElement("button");
      rm.className = "del";
      rm.textContent = "Убрать правило";
      rm.title = "убрать сохранённое правило для папки";
      rm.addEventListener("click", async () => {
        await call("RemoveFolderRule", n.path);
        flash("правило убрано", true);
        await renderSync();
      });
      row.appendChild(rm);
    } else {
      const del = document.createElement("button");
      del.className = "del";
      del.textContent = "Удалить с сервера";
      del.title = "удалить папку на сервере и её локальную копию здесь";
      del.addEventListener("click", async () => {
        if (!confirm("Удалить папку «" + n.path + "» с сервера?\n\nНа сервере: " + n.files + " ф. (" + fmtSize(n.bytes) + ").\nФайлы исчезнут и с других устройств, локальная копия на этом компьютере тоже будет удалена.")) return;
        const num = await call("DeleteServerFolder", n.path);
        if (num === null) {
          flash("не удалось удалить папку — смотрите журнал", false);
        } else {
          flash("папка «" + n.path + "» удалена с сервера (файлов: " + num + ")", true);
        }
        await renderSync();
      });
      row.appendChild(del);
    }
    box.appendChild(row);
  });
}

$("syncRefresh").addEventListener("click", async () => {
  await call("ResyncServer");
  flash("запрошен список с сервера и синхронизация", true);
  await renderSync();
});
$("rulesFree").addEventListener("change", async (e) => {
  await call("SetRulesFree", e.target.checked);
  flash(e.target.checked ? "режим без правил: синхронизируется всё в обе стороны" : "действуют правила папок", true);
  await renderSync();
});

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
  $("shotEdit").disabled = true;
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
  const okSel = shotSelRect.w > 0.01 && shotSelRect.h > 0.01;
  $("shotApply").disabled = !okSel;
  $("shotEdit").disabled = !okSel;
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
  if (path) flash("снимок сохранён: " + path, true);
  await refreshShotState();
});

$("shotRegion").addEventListener("click", async () => {
  const cap = await call("StartRegionCapture");
  if (cap && cap.id) await openShotOverlay(cap);
});

$("shotWindow").addEventListener("click", async () => {
  const path = await call("CaptureActiveWindow");
  if (path) flash("снимок сохранён: " + path, true);
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

/* ---------- редактор снимков ---------- */
let editCapture = null;
let editTool = "arrow";
let editUndoStack = [];
let editDragFrom = null;
let editBase = null;

function editCanvas() { return $("editCanvas"); }
function editCtx() { return editCanvas().getContext("2d"); }
function editColor() { return $("editColor").value || "#ff3b30"; }
function editWidth() { return parseInt($("editWidth").value, 10) || 4; }

function setEditTool(tool) {
  editTool = tool;
  [["editArrow", "arrow"], ["editBlur", "blur"], ["editText", "text"], ["editCrop", "crop"]].forEach(([id, t]) => {
    $(id).classList.toggle("primary", t === tool);
  });
}

// editCanvasPoint переводит координаты мыши в пиксели снимка:
// холст показывается уменьшенным, а рисуем мы всегда в полном размере.
function editCanvasPoint(e) {
  const c = editCanvas();
  const r = c.getBoundingClientRect();
  const kx = r.width ? c.width / r.width : 1;
  const ky = r.height ? c.height / r.height : 1;
  return { x: (e.clientX - r.left) * kx, y: (e.clientY - r.top) * ky };
}

let editOriginal = null; // снимок без правок: { w, h, data } — для кнопки «Вернуть исходный»

function updateEditButtons() {
  $("editUndo").disabled = editUndoStack.length === 0;
}

// undoEditStep снимает последнюю правку — тот же путь, что и у кнопки.
function undoEditStep() {
  const prev = editUndoStack.pop();
  if (prev) {
    const c = editCanvas();
    if (c.width !== prev.w || c.height !== prev.h) {
      c.width = prev.w;
      c.height = prev.h;
    }
    editCtx().putImageData(prev.data, 0, 0);
    $("editHint").textContent = "Шаг отменён";
  } else {
    $("editHint").textContent = "Отменять больше нечего";
  }
  updateEditButtons();
}

function editPushUndo() {
  const c = editCanvas();
  try {
    editUndoStack.push({ w: c.width, h: c.height, data: editCtx().getImageData(0, 0, c.width, c.height) });
  } catch (e) { /* очень большой снимок — просто без отмены шагов */ }
  if (editUndoStack.length > 6) editUndoStack.shift();
  updateEditButtons();
}

function drawEditorArrow(ctx, x0, y0, x1, y1) {
  const w = editWidth();
  const a = Math.atan2(y1 - y0, x1 - x0);
  const head = Math.max(14, w * 4);
  ctx.strokeStyle = editColor();
  ctx.fillStyle = ctx.strokeStyle;
  ctx.lineWidth = w;
  ctx.lineCap = "round";
  ctx.beginPath();
  ctx.moveTo(x0, y0);
  ctx.lineTo(x1 - Math.cos(a) * head * 0.7, y1 - Math.sin(a) * head * 0.7);
  ctx.stroke();
  ctx.beginPath();
  ctx.moveTo(x1, y1);
  ctx.lineTo(x1 - Math.cos(a - 0.45) * head, y1 - Math.sin(a - 0.45) * head);
  ctx.lineTo(x1 - Math.cos(a + 0.45) * head, y1 - Math.sin(a + 0.45) * head);
  ctx.closePath();
  ctx.fill();
}

function drawEditorBlurBox(ctx, x0, y0, x1, y1) {
  ctx.save();
  ctx.strokeStyle = editColor();
  ctx.lineWidth = 1;
  ctx.setLineDash([6, 4]);
  ctx.strokeRect(Math.min(x0, x1), Math.min(y0, y1), Math.abs(x1 - x0), Math.abs(y1 - y0));
  ctx.restore();
}

function drawEditorCropBox(ctx, x0, y0, x1, y1) {
  ctx.save();
  ctx.strokeStyle = "#ffffff";
  ctx.lineWidth = 1.5;
  ctx.setLineDash([6, 4]);
  ctx.strokeRect(Math.min(x0, x1), Math.min(y0, y1), Math.abs(x1 - x0), Math.abs(y1 - y0));
  ctx.restore();
}

function applyEditorCrop(x0, y0, x1, y1) {
  const c = editCanvas();
  const x = Math.max(0, Math.min(c.width - 1, Math.floor(Math.min(x0, x1))));
  const y = Math.max(0, Math.min(c.height - 1, Math.floor(Math.min(y0, y1))));
  const w = Math.min(c.width - x, Math.max(0, Math.floor(Math.abs(x1 - x0))));
  const h = Math.min(c.height - y, Math.max(0, Math.floor(Math.abs(y1 - y0))));
  if (w < 4 || h < 4) {
    $("editHint").textContent = "Область для обрезки слишком мала";
    return;
  }
  editPushUndo();
  const tmp = document.createElement("canvas");
  tmp.width = w;
  tmp.height = h;
  tmp.getContext("2d").drawImage(c, x, y, w, h, 0, 0, w, h);
  c.width = w;
  c.height = h;
  editCtx().drawImage(tmp, 0, 0);
  setEditTool("arrow");
  $("editHint").textContent = "Обрезано: " + w + "×" + h + ". Ctrl+Z — отменить";
}

function applyEditorBlur(ctx, x0, y0, x1, y1) {
  const x = Math.floor(Math.min(x0, x1));
  const y = Math.floor(Math.min(y0, y1));
  const w = Math.floor(Math.abs(x1 - x0));
  const h = Math.floor(Math.abs(y1 - y0));
  if (w < 3 || h < 3) return;
  const cell = Math.max(6, Math.round(editWidth() * 2.5));
  const tmp = document.createElement("canvas");
  tmp.width = Math.max(1, Math.round(w / cell));
  tmp.height = Math.max(1, Math.round(h / cell));
  const tctx = tmp.getContext("2d");
  tctx.imageSmoothingEnabled = false;
  tctx.drawImage(editCanvas(), x, y, w, h, 0, 0, tmp.width, tmp.height);
  ctx.imageSmoothingEnabled = false;
  ctx.drawImage(tmp, 0, 0, tmp.width, tmp.height, x, y, w, h);
  ctx.imageSmoothingEnabled = true;
}

function drawEditorText(ctx, x, y, text) {
  const size = 12 + editWidth() * 4;
  ctx.save();
  ctx.font = size + "px 'Segoe UI', Arial, sans-serif";
  ctx.textBaseline = "top";
  ctx.shadowColor = "rgba(0,0,0,.75)";
  ctx.shadowBlur = 4;
  ctx.fillStyle = editColor();
  ctx.fillText(text, x, y);
  ctx.restore();
}

async function openEditor(cap) {
  editCapture = cap;
  editUndoStack = [];
  editDragFrom = null;
  editBase = null;
  $("shotOverlay").hidden = true;

  const c = editCanvas();
  c.width = cap.width;
  c.height = cap.height;
  try {
    const img = new Image();
    await new Promise((res, rej) => {
      img.onload = res;
      img.onerror = rej;
      img.src = cap.preview;
    });
    const ctx = editCtx();
    ctx.drawImage(img, 0, 0, c.width, c.height);
    editOriginal = { w: c.width, h: c.height, data: ctx.getImageData(0, 0, c.width, c.height) };
  } catch (e) {
    editOriginal = null;
    flash("не удалось открыть снимок в редакторе", false);
  }
  setEditTool("arrow");
  updateEditButtons();
  $("editHint").textContent = "Инструмент: стрелка — протяните от начала к концу";
  $("editOverlay").hidden = false;
}

function closeEditor() {
  editCapture = null;
  editUndoStack = [];
  editDragFrom = null;
  editBase = null;
  editOriginal = null;
  updateEditButtons();
  $("editOverlay").hidden = true;
}

$("editArrow").addEventListener("click", () => {
  setEditTool("arrow");
  $("editHint").textContent = "Инструмент: стрелка — протяните от начала к концу";
});
$("editBlur").addEventListener("click", () => {
  setEditTool("blur");
  $("editHint").textContent = "Инструмент: размытие — выделите прямоугольник";
});
$("editText").addEventListener("click", () => {
  setEditTool("text");
  $("editHint").textContent = "Инструмент: текст — впишите подпись и щёлкните по снимку";
});
$("editCrop").addEventListener("click", () => {
  setEditTool("crop");
  $("editHint").textContent = "Обрезать: выделите мышью область — она сразу применится";
});

$("editCanvas").addEventListener("mousedown", (e) => {
  if (!editCapture) return;
  const p = editCanvasPoint(e);
  if (editTool === "text") {
    const text = $("editTextValue").value.trim();
    if (!text) {
      $("editHint").textContent = "Сначала впишите текст подписи в поле сверху";
      return;
    }
    editPushUndo();
    drawEditorText(editCtx(), p.x, p.y, text);
    return;
  }
  if (editTool === "crop") {
    editBase = editCtx().getImageData(0, 0, editCanvas().width, editCanvas().height);
    editDragFrom = p;
    return;
  }
  editPushUndo();
  editBase = editCtx().getImageData(0, 0, editCanvas().width, editCanvas().height);
  editDragFrom = p;
});

window.addEventListener("mousemove", (e) => {
  if (!editCapture || !editDragFrom || !editBase) return;
  const p = editCanvasPoint(e);
  const ctx = editCtx();
  ctx.putImageData(editBase, 0, 0);
  if (editTool === "arrow") drawEditorArrow(ctx, editDragFrom.x, editDragFrom.y, p.x, p.y);
  else if (editTool === "blur") drawEditorBlurBox(ctx, editDragFrom.x, editDragFrom.y, p.x, p.y);
  else if (editTool === "crop") drawEditorCropBox(ctx, editDragFrom.x, editDragFrom.y, p.x, p.y);
});

window.addEventListener("mouseup", (e) => {
  if (!editCapture || !editDragFrom || !editBase) return;
  const p = editCanvasPoint(e);
  const ctx = editCtx();
  ctx.putImageData(editBase, 0, 0);
  if (editTool === "crop") applyEditorCrop(editDragFrom.x, editDragFrom.y, p.x, p.y);
  else if (editTool === "arrow") drawEditorArrow(ctx, editDragFrom.x, editDragFrom.y, p.x, p.y);
  else if (editTool === "blur") applyEditorBlur(ctx, editDragFrom.x, editDragFrom.y, p.x, p.y);
  editDragFrom = null;
  editBase = null;
});

$("editUndo").addEventListener("click", undoEditStep);

$("editReset").addEventListener("click", () => {
  if (!editOriginal) return;
  editPushUndo(); // чтобы и сброс можно было отменить
  const c = editCanvas();
  if (c.width !== editOriginal.w || c.height !== editOriginal.h) {
    c.width = editOriginal.w;
    c.height = editOriginal.h;
  }
  editCtx().putImageData(editOriginal.data, 0, 0);
  $("editHint").textContent = "Все правки сняты — их можно вернуть кнопкой «Отменить шаг»";
});

// Ctrl+Z работает и в русской раскладке (там это клавиша «я»).
window.addEventListener("keydown", (e) => {
  if (!editCapture) return;
  const key = (e.key || "").toLowerCase();
  if ((e.ctrlKey || e.metaKey) && (key === "z" || key === "я")) {
    e.preventDefault();
    undoEditStep();
  }
});

$("editSave").addEventListener("click", async () => {
  if (!editCapture) return;
  const dataURL = editCanvas().toDataURL("image/png");
  const path = await call("SaveEditedShot", editCapture.id, dataURL);
  closeEditor();
  flash(path ? ("снимок сохранён: " + path) : "не удалось сохранить снимок", !!path);
  await refreshShotState();
});

$("editCancel").addEventListener("click", async () => {
  if (editCapture) await call("CancelRegionCapture", editCapture.id);
  closeEditor();
});

$("shotEdit").addEventListener("click", async () => {
  if (!shotCapture || !shotSelRect) return;
  const cap = await call("BeginRegionEdit", shotCapture.id,
    shotSelRect.x, shotSelRect.y, shotSelRect.w, shotSelRect.h);
  if (cap && cap.id) await openEditor(cap);
});

$("editAfterShot").addEventListener("change", async (e) => {
  await call("SetEditAfterShot", e.target.checked);
  flash(e.target.checked ? "редактор будет открываться после снимка" : "снимок будет сохраняться сразу", true);
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
  const msg = await call("TestConnection", f.host, f.port, f.token, f.tls, f.fingerprint);
  flash(msg || "Ошибка проверки", !!(msg && msg.indexOf("OK") === 0));
});
$("cliRecheck").addEventListener("click", async () => {
  const st = await call("RecheckToken");
  renderClient(st);
  flash("проверка токена выполнена", true);
});

async function refreshFiles() { renderFiles((await call("LocalFiles")) || []); }

async function refresh() {
  if (document.hidden) return;
  if (await call("WindowHidden")) return;
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
  if (!editAfterLoaded) {
    const ea = await call("EditAfterShotEnabled");
    if (typeof ea === "boolean") { $("editAfterShot").checked = ea; editAfterLoaded = true; }
  }
  if (!syncLoaded) {
    const rf = await call("RulesFreeEnabled");
    if (typeof rf === "boolean") { $("rulesFree").checked = rf; syncLoaded = true; }
  }
  // захват, начатый из трея или горячей клавишей: выбор области или редактор
  if ($("shotOverlay").hidden && $("editOverlay").hidden) {
    const cap = await call("PendingRegionCapture");
    if (cap && cap.id) {
      if (cap.mode === "edit") await openEditor(cap);
      else await openShotOverlay(cap);
    }
  }
}

window.addEventListener("DOMContentLoaded", () => {
  refresh();
  setInterval(() => { if (!document.hidden) refresh(); }, 1000);
});
document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
