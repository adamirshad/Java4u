"use strict";

// Goose desktop UI. All real work happens in the Go process; this page only shows
// state and sends requests to it.

const $ = (id) => document.getElementById(id);

let data = null;          // from /api/data
let selected = null;      // selected instance name (shared by Play and Instances)
let selectedAccount = ""; // selected account username
let editorFor = null;     // instance name the editor form was filled for
let logEpoch = -1;
let logNext = 0;
let lastState = {};
let shownWallpaper = null;

// ------------------------------------------------------------ helpers

function el(tag, props = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (k in e) e[k] = v;
    else e.setAttribute(k, v);
  }
  for (const c of children) e.append(c);
  return e;
}

function show(node, visible) { node.classList.toggle("hidden", !visible); }

function setStatus(msg) { $("status").textContent = msg; }

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
  const res = await fetch(path, opts);
  let out = {};
  try { out = await res.json(); } catch { /* empty reply */ }
  if (!res.ok) throw new Error(out.error || res.statusText);
  return out;
}

async function run(fn) {
  try { await fn(); } catch (e) { setStatus(e.message); }
}

function fillSelect(select, items, value) {
  select.replaceChildren(...items.map(([v, label]) => el("option", { value: v, textContent: label })));
  if (value !== undefined && items.some(([v]) => v === value)) select.value = value;
}

// ------------------------------------------------------------ data refresh

async function refresh() {
  data = await api("/api/data");
  const names = data.instances.map((i) => i.name);
  if (!names.includes(selected)) {
    selected = names.includes(data.settings.last_instance) ? data.settings.last_instance : (names[0] ?? null);
  }
  const users = data.accounts.map((a) => a.username);
  if (!users.includes(selectedAccount)) selectedAccount = users[0] ?? "";
  render();
}

function render() {
  if (!data) return;
  renderBackground();
  renderPlay();
  renderInstances();
  renderAccounts();
  renderAppearance();
}

function renderBackground() {
  const s = data.settings;
  const want = s.wallpaper || "";
  if (want !== shownWallpaper) {
    $("bg").style.backgroundImage = want ? `url("/wallpapers/${encodeURIComponent(want)}")` : "none";
    shownWallpaper = want;
  }
  $("dim").style.opacity = want ? String(Math.min(Math.max(s.wallpaper_dim, 0), 0.9)) : "0";
}

// ------------------------------------------------------------ Play

function renderPlay() {
  const has = data.instances.length > 0;
  show($("play-empty"), !has);
  show($("play-form"), has);
  fillSelect($("play-instance"), data.instances.map((i) => [i.name, `${i.name} (${i.version})`]), selected);

  const offline = $("play-offline").checked;
  show($("play-offline-name"), offline);
  if (!$("play-offline-name").value) $("play-offline-name").value = data.settings.offline_username || "Player";
  show($("play-no-account"), !offline && data.accounts.length === 0);
  show($("play-account-wrap"), !offline && data.accounts.length > 0);
  fillSelect($("play-account"), data.accounts.map((a) => [a.username, a.username]), selectedAccount);
  renderPlayButton();
}

function renderPlayButton() {
  const st = lastState;
  const offline = $("play-offline").checked;
  const can = !st.busy && selected && (offline || selectedAccount);
  $("play").disabled = !can;
  $("play").textContent = st.running ? "Running..." : st.busy ? "Preparing..." : "Play";
}

$("play-instance").addEventListener("change", (e) => { selected = e.target.value; editorFor = null; renderInstances(); renderPlayButton(); });
$("play-account").addEventListener("change", (e) => { selectedAccount = e.target.value; renderPlayButton(); });
$("play-offline").addEventListener("change", renderPlay);

$("play").addEventListener("click", () => run(async () => {
  const offline = $("play-offline").checked;
  await api("/api/play", {
    instance: selected,
    account: selectedAccount,
    offline,
    offline_username: $("play-offline-name").value.trim(),
  });
  lastState.busy = true;
  renderPlayButton();
  poll();
}));

function appendLog(lines, reset) {
  const log = $("log");
  const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 30;
  if (reset) log.textContent = "";
  if (lines.length) {
    log.append(lines.join("\n") + "\n");
    // Keep the page light: trim the oldest text past ~3000 lines.
    while (log.childNodes.length > 200) log.firstChild.remove();
  }
  if (atBottom || reset) log.scrollTop = log.scrollHeight;
}

// ------------------------------------------------------------ Instances

function versionChoices() {
  return data.versions
    .filter((v) => v.kind === "release" || (data.settings.show_snapshots && v.kind === "snapshot"))
    .map((v) => [v.id, v.id]);
}

function renderInstances() {
  const choices = versionChoices();
  const vs = $("new-version");
  const prev = vs.value;
  if (choices.length) fillSelect(vs, choices, prev || choices[0][0]);
  else fillSelect(vs, [["", data.versions_error ? "versions unavailable" : "loading versions..."]]);
  $("show-snapshots").checked = data.settings.show_snapshots;
  updateCreateButton();

  $("instance-list").replaceChildren(...data.instances.map((i) => el("li", {},
    el("button", {
      class: "item" + (i.name === selected ? " selected" : ""),
      textContent: `${i.name} (${i.version})`,
      onclick: () => { selected = i.name; editorFor = null; hideConfirm(); render(); },
    }))));

  const inst = data.instances.find((i) => i.name === selected);
  show($("editor-empty"), !inst);
  show($("editor-form"), !!inst);
  if (!inst || editorFor === inst.name) return;

  editorFor = inst.name;
  $("editor-title").textContent = `${inst.name}  -  Minecraft ${inst.version}`;
  const mem = $("memory");
  mem.max = String(Math.max(data.memory.max, inst.memory_mb));
  mem.value = String(inst.memory_mb);
  $("memory-out").textContent = `${inst.memory_mb} MB`;
  fillSelect($("profile"), data.profiles.map((p) => [p.id, p.label]), inst.profile);
  renderProfileDesc();
  $("jvm-args").value = (inst.jvm_args || []).join(" ");
  $("java-path").value = inst.java_path || "";
}

function updateCreateButton() {
  $("create").disabled = !$("new-name").value.trim() || !$("new-version").value;
}

function renderProfileDesc() {
  const p = data.profiles.find((p) => p.id === $("profile").value);
  $("profile-desc").textContent = p ? p.description : "";
}

$("new-name").addEventListener("input", updateCreateButton);
$("new-version").addEventListener("change", updateCreateButton);
$("show-snapshots").addEventListener("change", (e) => run(async () => {
  data.settings = await api("/api/settings", { show_snapshots: e.target.checked });
  renderInstances();
}));
$("create").addEventListener("click", () => run(async () => {
  const inst = await api("/api/instances", { name: $("new-name").value.trim(), version: $("new-version").value });
  $("new-name").value = "";
  selected = inst.name;
  editorFor = null;
  await refresh();
}));

$("memory").addEventListener("input", (e) => { $("memory-out").textContent = `${e.target.value} MB`; });
$("memory-auto").addEventListener("click", () => {
  $("memory").value = String(data.memory.recommended);
  $("memory-out").textContent = `${data.memory.recommended} MB`;
});
$("profile").addEventListener("change", renderProfileDesc);

$("save").addEventListener("click", () => run(async () => {
  await api("/api/instances/save", {
    name: selected,
    memory_mb: Number($("memory").value),
    profile: $("profile").value,
    jvm_args: $("jvm-args").value,
    java_path: $("java-path").value,
  });
  editorFor = null;
  await refresh();
}));

function hideConfirm() { show($("confirm-delete"), false); show($("delete"), true); }
$("delete").addEventListener("click", () => { show($("confirm-delete"), true); show($("delete"), false); });
$("delete-no").addEventListener("click", hideConfirm);
$("delete-yes").addEventListener("click", () => run(async () => {
  hideConfirm();
  await api("/api/instances/delete", { name: selected });
  editorFor = null;
  await refresh();
}));

// ------------------------------------------------------------ Accounts

function renderAccounts() {
  const list = $("account-list");
  if (data.accounts.length === 0) {
    list.replaceChildren(el("li", { class: "muted", textContent: "No accounts saved yet." }));
  } else {
    list.replaceChildren(...data.accounts.map((a) => el("li", {},
      el("span", { textContent: a.username }),
      el("button", {
        textContent: "Remove",
        onclick: () => run(async () => { await api("/api/accounts/remove", { username: a.username }); await refresh(); }),
      }))));
  }
}

$("copy-code").addEventListener("click", () => {
  navigator.clipboard.writeText($("device-user-code").textContent).then(
    () => { $("copy-code").textContent = "Copied"; setTimeout(() => { $("copy-code").textContent = "Copy code"; }, 1500); },
    () => setStatus("Could not copy; select the code and copy it yourself."));
});

$("login").addEventListener("click", () => run(async () => {
  await api("/api/login", { client_id: $("client-id").value });
  poll();
}));

function renderDeviceCode(dc) {
  show($("device-code"), !!dc);
  if (!dc) return;
  const a = $("device-uri");
  if (a.textContent !== dc.uri) {
    a.textContent = dc.uri;
    a.href = /^https:\/\//.test(dc.uri) ? dc.uri : "#";
  }
  $("device-user-code").textContent = dc.code;
}

// ------------------------------------------------------------ Appearance

function renderAppearance() {
  const s = data.settings;
  $("no-wallpaper").disabled = !s.wallpaper;
  if (document.activeElement !== $("dim-slider")) $("dim-slider").value = String(s.wallpaper_dim);
  $("wallpaper-count").textContent = `Your wallpapers (${data.wallpapers.length})`;
  $("wallpaper-list").replaceChildren(...data.wallpapers.map((name) => {
    const active = name === s.wallpaper;
    const shown = name.replace(/\.jpg$/, "");
    return el("li", {},
      el("span", { textContent: active ? `${shown}  (in use)` : shown }),
      el("button", { textContent: "Use", disabled: active, onclick: () => setWallpaper(name) }),
      el("button", {
        textContent: "Delete",
        onclick: () => run(async () => { await api("/api/wallpapers/delete", { name }); await refresh(); }),
      }));
  }));
}

function setWallpaper(name) {
  return run(async () => {
    data.settings = await api("/api/settings", { wallpaper: name });
    render();
  });
}

async function uploadWallpaper(file) {
  setStatus("Importing wallpaper...");
  const form = new FormData();
  form.append("file", file);
  await run(async () => {
    const res = await fetch("/api/wallpapers", { method: "POST", body: form });
    const out = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(out.error || res.statusText);
    await refresh();
    setStatus("Wallpaper added.");
  });
}

$("wallpaper-file").addEventListener("change", (e) => {
  const f = e.target.files[0];
  e.target.value = "";
  if (f) uploadWallpaper(f);
});
$("no-wallpaper").addEventListener("click", () => setWallpaper(""));
$("dim-slider").addEventListener("input", (e) => {
  data.settings.wallpaper_dim = Number(e.target.value);
  renderBackground();
});
// Save once the slider is released.
$("dim-slider").addEventListener("change", (e) => run(async () => {
  data.settings = await api("/api/settings", { wallpaper_dim: Number(e.target.value) });
}));

// Drag and drop an image anywhere on the window to use it as wallpaper.
let dragDepth = 0;
const hasFiles = (e) => e.dataTransfer && [...e.dataTransfer.types].includes("Files");
window.addEventListener("dragenter", (e) => { if (hasFiles(e)) { dragDepth++; show($("drop"), true); } });
window.addEventListener("dragleave", () => { if (--dragDepth <= 0) { dragDepth = 0; show($("drop"), false); } });
window.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
window.addEventListener("drop", (e) => {
  e.preventDefault();
  dragDepth = 0;
  show($("drop"), false);
  const f = e.dataTransfer.files[0];
  if (f) uploadWallpaper(f);
});

// ------------------------------------------------------------ tabs, quit, polling

document.querySelectorAll(".tab").forEach((b) => b.addEventListener("click", () => {
  document.querySelectorAll(".tab").forEach((t) => t.classList.toggle("active", t === b));
  document.querySelectorAll("main > section").forEach((s) => show(s, s.id === `tab-${b.dataset.tab}`));
}));

$("quit").addEventListener("click", () => {
  if (lastState.running && !confirm("Minecraft keeps running after Goose closes. Close Goose?")) return;
  if (lastState.busy && !lastState.running && !confirm("A download is still in progress. Close Goose anyway?")) return;
  api("/api/quit", {}).finally(() => {
    document.body.replaceChildren(el("p", { class: "panel", textContent: "Goose has closed. You can close this window." }));
    window.close();
  });
});

let pollTimer = null;
async function poll() {
  clearTimeout(pollTimer);
  let next = 1000;
  try {
    const st = await api(`/api/state?epoch=${logEpoch}&since=${logNext}`);
    const loginFinished = lastState.logging_in && !st.logging_in;
    lastState = st;
    appendLog(st.log, st.log_reset);
    logEpoch = st.log_epoch;
    logNext = st.log_next;
    setStatus(st.status);
    renderDeviceCode(st.device_code);
    $("login").disabled = st.logging_in;

    const p = st.progress;
    show($("progress"), !!p);
    if (p) {
      $("progress-bar").value = p.total === 0 ? 1 : p.done / p.total;
      $("progress-text").textContent = `${p.stage}  ${p.done}/${p.total}`;
    }
    renderPlayButton();
    if (loginFinished) await refresh();
    if (st.busy || st.logging_in) next = 400;
    if (data && data.versions.length === 0 && !data.versions_error) await refresh();
  } catch (e) {
    setStatus("Lost connection to Goose. Is it still running?");
    next = 3000;
  }
  pollTimer = setTimeout(poll, next);
}

run(refresh).then(poll);
