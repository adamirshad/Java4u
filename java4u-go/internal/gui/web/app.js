"use strict";

// Java4U desktop UI. All real work happens in the Go process; this page shows state
// and sends requests to it.

const $ = (id) => document.getElementById(id);
const SVGNS = "http://www.w3.org/2000/svg";

let data = null;          // from /api/data
let selected = null;      // selected instance name
let account = "";         // selected Microsoft account username
let editorFor = null;     // instance the Versions editor was filled for
let logEpoch = -1;
let logNext = 0;
let lastState = {};
let news = [];
let slide = 0;
let slideTimer = null;

// ------------------------------------------------------------ helpers

function el(tag, props = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (k in e) e[k] = v;
    else e.setAttribute(k, v);
  }
  for (const c of children) if (c != null) e.append(c);
  return e;
}
const show = (node, visible) => node.classList.toggle("hidden", !visible);

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

let statusOverride = "";
function setStatus(msg, kind = "") {
  statusOverride = msg;
  $("status").textContent = msg;
  $("status-icon").className = "status-ok" + (kind ? " " + kind : "");
  $("status-icon").textContent = kind === "err" ? "!" : kind === "busy" ? "…" : "✓";
}
async function run(fn) {
  try { await fn(); } catch (e) { setStatus(e.message, "err"); }
}
function fillSelect(select, items, value) {
  select.replaceChildren(...items.map(([v, label]) => el("option", { value: v, textContent: label })));
  if (value !== undefined && items.some(([v]) => String(v) === String(value))) select.value = value;
}
function img(src, cls = "") {
  if (!src) return el("div", { class: "noimg " + cls });
  return el("img", { src, alt: "", loading: "lazy", class: cls, onerror: (e) => e.target.replaceWith(el("div", { class: "noimg " + cls })) });
}
function compact(n) {
  return Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(n);
}
function niceDate(s) {
  const d = new Date(s + "T00:00:00");
  return isNaN(d) ? s : d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}
const current = () => data?.instances.find((i) => i.name === selected) || null;

// ------------------------------------------------------------ pixel art

const GOOSE = [
  "................",
  ".........WWWW...",
  "........WWWWWW..",
  "........WWKWWWOO",
  "........WWWWWWOo",
  ".........WWWW...",
  "..........WWW...",
  "..........WWW...",
  "..W.......WWW...",
  ".WWW....WWWWW...",
  "WWWGGWWWWWWWW...",
  "WWGGGGWWWWWWG...",
  ".WWGGGWWWWWGG...",
  "..WWWWWWWWGG....",
  "....GWWWWGG.....",
  ".....O...O......",
  ".....O...O......",
  "....OO..OO......",
];
const GOOSE_HEAD = [
  "....WWWW....",
  "...WWWWWW...",
  "...WWKWWWOO.",
  "...WWWWWWOOo",
  "....WWWW....",
  ".....WWW....",
  ".....WWW....",
  "....WWWWW...",
  "...WWWWWWW..",
  "...GWWWWWG..",
];
const PALETTE = {
  W: "#f6f6f1", G: "#c7c9c1", K: "#151515", O: "#f39a1e", o: "#c96a0c",
  g: "#62c23c", h: "#4b9e2e", j: "#3a7f22", d: "#8b5a34", e: "#6c4424", f: "#a7703f",
  s: "#8d9690", t: "#5e6762", u: "#2b322e", y: "#b9c2bc",
};

// Deterministic speckle so blocks look the same every time.
function speckle(x, y, n) { return ((x * 73856093) ^ (y * 19349663)) % n; }

function grassRows(w, h) {
  const rows = [];
  for (let y = 0; y < h; y++) {
    let r = "";
    for (let x = 0; x < w; x++) {
      const k = Math.abs(speckle(x + 3, y + 5, 7));
      if (y < 3 || (y === 3 && k < 4)) r += k < 2 ? "h" : k === 2 ? "j" : "g";
      else r += k < 2 ? "e" : k === 2 ? "f" : "d";
    }
    rows.push(r);
  }
  return rows;
}
const CHIP = [
  "...s.s.s.s.s....",
  "..tttttttttttt..",
  "s.tuuuuuuuuuut.s",
  "..tuyyyyyyyyut..",
  "s.tuyuuuuuuyut.s",
  "..tuyussssuyut..",
  "s.tuyussssuyut.s",
  "..tuyussssuyut..",
  "s.tuyussssuyut.s",
  "..tuyuuuuuuyut..",
  "s.tuyyyyyyyyut.s",
  "..tuuuuuuuuuut..",
  "..tttttttttttt..",
  "...s.s.s.s.s....",
];

function pixelSVG(rows, pad = 0) {
  const h = rows.length, w = rows[0].length;
  const svg = document.createElementNS(SVGNS, "svg");
  svg.setAttribute("viewBox", `${-pad} ${-pad} ${w + pad * 2} ${h + pad * 2}`);
  svg.setAttribute("class", "pixel");
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const c = PALETTE[rows[y][x]];
      if (!c) continue;
      const r = document.createElementNS(SVGNS, "rect");
      r.setAttribute("x", x); r.setAttribute("y", y);
      r.setAttribute("width", 1.02); r.setAttribute("height", 1.02);
      r.setAttribute("fill", c);
      svg.append(r);
    }
  }
  return svg;
}

function gooseOnBlock() {
  const block = grassRows(16, 7);
  return pixelSVG([...GOOSE.map((r) => r), ...block]);
}

function gooseScene() {
  // A goose standing on a grass ledge with a few floating green pixels behind it.
  const w = 28, h = 30;
  const rows = Array.from({ length: h }, () => Array(w).fill("."));
  for (let i = 0; i < 18; i++) {
    const x = Math.abs(speckle(i, 11, w)), y = Math.abs(speckle(17, i, 14));
    rows[y][x] = i % 3 ? "j" : "h";
  }
  GOOSE.forEach((r, y) => [...r].forEach((c, x) => { if (c !== ".") rows[y + 5][x + 6] = c; }));
  grassRows(w, 7).forEach((r, y) => [...r].forEach((c, x) => { rows[y + 23][x] = c; }));
  return pixelSVG(rows.map((r) => r.join("")));
}

function drawArt() {
  const makers = {
    goose: () => pixelSVG(GOOSE, 1),
    "goose-head": () => pixelSVG(GOOSE_HEAD, 1),
    "goose-on-block": gooseOnBlock,
    "goose-scene": gooseScene,
    grass: () => pixelSVG(grassRows(16, 16), 1),
    chip: () => pixelSVG(CHIP, 1),
  };
  document.querySelectorAll("[data-pixel]").forEach((n) => n.replaceChildren(makers[n.dataset.pixel]()));
}

// ------------------------------------------------------------ icons

const ICONS = {
  home: "M3 11 12 3l9 8v10h-6v-6H9v6H3z",
  versions: "M12 2 3 7v10l9 5 9-5V7zm0 2.3 6.6 3.7L12 11.7 5.4 8zM5 9.7l6 3.4v6.6l-6-3.3zm8 10v-6.6l6-3.4v6.7z",
  mods: "M10 3a2 2 0 0 1 4 0v2h4a1 1 0 0 1 1 1v4h-2a2 2 0 0 0 0 4h2v4a1 1 0 0 1-1 1h-4v-2a2 2 0 0 0-4 0v2H6a1 1 0 0 1-1-1v-4H3a2 2 0 0 1 0-4h2V6a1 1 0 0 1 1-1h4z",
  modpacks: "M12 2 2 7l10 5 10-5zM4 10.5 2 11.5l10 5 10-5-2-1-8 4zm0 4L2 15.5l10 5 10-5-2-1-8 4z",
  account: "M12 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10zm0 2c-5 0-9 2.5-9 6v2h18v-2c0-3.5-4-6-9-6z",
  settings: "M19.4 13a7.5 7.5 0 0 0 0-2l2.1-1.6-2-3.5-2.5 1a7.5 7.5 0 0 0-1.7-1L14 3h-4l-.4 2.9a7.5 7.5 0 0 0-1.7 1l-2.5-1-2 3.5L4.6 11a7.5 7.5 0 0 0 0 2l-2.1 1.6 2 3.5 2.5-1a7.5 7.5 0 0 0 1.7 1L10 21h4l.4-2.9a7.5 7.5 0 0 0 1.7-1l2.5 1 2-3.5zM12 15.5a3.5 3.5 0 1 1 0-7 3.5 3.5 0 0 1 0 7z",
  about: "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 15h-2v-6h2zm0-8h-2V7h2z",
  news: "M4 2h12l4 4v16H4zm3 7v2h10V9zm0 4v2h10v-2zm0 4v2h7v-2zM7 5v2h6V5z",
};
function drawIcons() {
  document.querySelectorAll("[data-icon]").forEach((n) => {
    const svg = document.createElementNS(SVGNS, "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    const p = document.createElementNS(SVGNS, "path");
    p.setAttribute("d", ICONS[n.dataset.icon]);
    p.setAttribute("fill", n.classList.contains("phicon") ? "#45d651" : "#aab3ad");
    p.setAttribute("fill-rule", "evenodd");
    svg.append(p);
    n.replaceChildren(svg);
  });
}

// ------------------------------------------------------------ navigation

function go(page) {
  document.querySelectorAll(".nav").forEach((b) => b.classList.toggle("active", b.dataset.page === page));
  document.querySelectorAll("main > .page").forEach((s) => show(s, s.id === `page-${page}`));
  $("main").scrollTop = 0;
  if (page === "mods" && !$("mods-results").childElementCount) searchBrowse("mod");
  if (page === "modpacks" && !$("packs-results").childElementCount) searchBrowse("modpack");
}
document.querySelectorAll(".nav").forEach((b) => b.addEventListener("click", () => go(b.dataset.page)));
document.querySelectorAll("[data-goto]").forEach((a) => a.addEventListener("click", () => go(a.dataset.goto)));

// ------------------------------------------------------------ data

async function refresh() {
  data = await api("/api/data");
  const names = data.instances.map((i) => i.name);
  if (!names.includes(selected)) {
    selected = names.includes(data.settings.last_instance) ? data.settings.last_instance : (names[0] ?? null);
  }
  const users = data.accounts.map((a) => a.username);
  if (!users.includes(account)) {
    account = users.includes(data.settings.last_account) ? data.settings.last_account : (users[0] ?? "");
  }
  render();
}

function render() {
  if (!data) return;
  renderHeader();
  renderQuick();
  renderLaunch();
  renderVersions();
  renderAccounts();
  renderSettings();
  renderSlides();
  $("about-version").textContent = "v" + data.app_version;
  $("about-dir").textContent = data.data_dir;
  $("footer-version").textContent = "Java4U v" + data.app_version;
}

const offline = () => !!data?.settings.play_offline;

// ------------------------------------------------------------ header and account menu

function avatarFor(acct) {
  const box = el("div", { class: "avatar" });
  if (acct?.uuid) {
    box.append(el("img", {
      src: `/api/avatar/${acct.uuid}`, alt: "",
      onerror: (e) => e.target.replaceWith(pixelSVG(GOOSE_HEAD, 1)),
    }));
  } else {
    box.append(pixelSVG(GOOSE_HEAD, 1));
  }
  return box;
}

function renderHeader() {
  const acct = data.accounts.find((a) => a.username === account);
  let name, state, dot;
  if (offline()) {
    name = data.settings.offline_username || "Player";
    state = "Offline mode"; dot = "warn";
  } else if (acct) {
    name = acct.username; state = "Online"; dot = "on";
  } else {
    name = "Not signed in"; state = "Sign in to play"; dot = "";
  }
  $("chip-avatar").replaceWith(Object.assign(avatarFor(offline() ? null : acct), { id: "chip-avatar" }));
  $("chip-name").textContent = name;
  $("chip-state").textContent = state;
  $("chip-dot").className = "dot " + dot;
}

function openMenu() {
  const menu = $("account-menu");
  const items = data.accounts.map((a) => el("button", {
    onclick: (e) => { e.stopPropagation(); chooseAccount(a.username); },
  }, avatarFor(a), el("span", { textContent: a.username }),
     !offline() && a.username === account ? el("span", { class: "check-mark", textContent: "✓" }) : null));
  menu.replaceChildren(
    ...items,
    items.length ? el("hr") : null,
    el("button", { textContent: "+ Add a Microsoft account", onclick: (e) => { e.stopPropagation(); closeMenu(); go("account"); } }),
    el("button", { textContent: offline() ? "Turn off offline mode" : "Offline mode (testing)...", onclick: (e) => {
      e.stopPropagation(); closeMenu();
      if (offline()) saveSettings({ play_offline: false }); else go("settings");
    } }),
  );
  show(menu, true);
}
function closeMenu() { show($("account-menu"), false); }
$("account-chip").addEventListener("click", () => ($("account-menu").classList.contains("hidden") ? openMenu() : closeMenu()));
document.addEventListener("click", (e) => { if (!$("account-chip").contains(e.target)) closeMenu(); });

function chooseAccount(name) {
  account = name;
  closeMenu();
  saveSettings({ last_account: name, play_offline: false });
}

async function saveSettings(patch) {
  await run(async () => {
    data.settings = await api("/api/settings", patch);
    render();
  });
}

// ------------------------------------------------------------ quick settings

function ramChoices(inst) {
  const maxGB = Math.max(2, Math.floor(data.memory.max / 1024));
  const opts = [];
  for (let gb = 1; gb <= maxGB; gb++) opts.push([gb * 1024, `${gb} GB`]);
  if (inst && !opts.some(([mb]) => mb === inst.memory_mb)) {
    opts.push([inst.memory_mb, `${(inst.memory_mb / 1024).toFixed(1)} GB`]);
    opts.sort((a, b) => a[0] - b[0]);
  }
  return opts;
}

function renderQuick() {
  const inst = current();
  fillSelect($("sel-instance"), data.instances.map((i) => [i.name, `Minecraft ${i.version}  (${i.name})`]), selected);
  $("q-version").textContent = inst ? `Minecraft ${inst.version}${inst.loader === "chunky" ? " · Chunky" : ""}` : "No versions yet";
  fillSelect($("sel-profile"), data.profiles.map((p) => [p.id, p.label]), inst?.profile);
  $("q-profile").textContent = inst ? (data.profiles.find((p) => p.id === inst.profile)?.label ?? inst.profile) : "-";
  const ram = ramChoices(inst);
  fillSelect($("sel-ram"), ram, inst?.memory_mb);
  $("q-ram").textContent = inst ? (ram.find(([mb]) => mb === inst.memory_mb)?.[1] ?? `${inst.memory_mb} MB`) : "-";
  for (const id of ["sel-profile", "sel-ram"]) $(id).disabled = !inst;
}

function selectInstance(name) {
  selected = name;
  editorFor = null;
  render();
  loadHomeMods();
  loadInstalled();
  api("/api/settings", { last_instance: name }).catch(() => {});
}

async function saveInstance(patch) {
  const inst = current();
  if (!inst) return;
  await run(async () => {
    await api("/api/instances/save", {
      name: inst.name,
      memory_mb: inst.memory_mb,
      profile: inst.profile,
      jvm_args: (inst.jvm_args || []).join(" "),
      java_path: inst.java_path || "",
      loader: inst.loader || "",
      ...patch,
    });
    editorFor = null;
    await refresh();
  });
}

$("sel-instance").addEventListener("change", (e) => selectInstance(e.target.value));
$("sel-profile").addEventListener("change", (e) => saveInstance({ profile: e.target.value }));
$("sel-ram").addEventListener("change", (e) => saveInstance({ memory_mb: Number(e.target.value) }));
$("more-options").addEventListener("click", () => { editorFor = null; go("versions"); renderVersions(); });

// ------------------------------------------------------------ launch

function launchMode() {
  if (!data || !current()) return "noinstance";
  if (!offline() && !account) return "noaccount";
  return "ok";
}

function renderLaunch() {
  const st = lastState;
  const btn = $("launch");
  btn.classList.toggle("running", !!st.running);
  const mode = launchMode();
  btn.disabled = !!st.busy;
  $("launch-text").textContent =
    st.running ? "Playing..." :
    st.busy ? "Preparing..." :
    mode === "noinstance" ? "Add a Version" :
    mode === "noaccount" ? "Sign In to Play" : "Launch Game";
}

$("launch").addEventListener("click", () => {
  const mode = launchMode();
  if (mode === "noinstance") return go("versions");
  if (mode === "noaccount") return go("account");
  run(async () => {
    await api("/api/play", {
      instance: selected,
      account,
      offline: offline(),
      offline_username: data.settings.offline_username || "Player",
    });
    lastState.busy = true;
    renderLaunch();
    setStatus("Preparing Minecraft...", "busy");
    poll();
  });
});

function appendLog(lines, reset) {
  const log = $("log");
  const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 30;
  if (reset) log.textContent = "";
  if (lines.length) {
    log.append(lines.join("\n") + "\n");
    while (log.childNodes.length > 200) log.firstChild.remove();
  }
  if (atBottom || reset) log.scrollTop = log.scrollHeight;
}
$("toggle-log").addEventListener("click", () => show($("drawer"), $("drawer").classList.contains("hidden")));
$("drawer-close").addEventListener("click", () => show($("drawer"), false));

// ------------------------------------------------------------ hero carousel

function slides() {
  const own = data?.settings.wallpaper;
  const first = own ? { bg: `/wallpapers/${encodeURIComponent(own)}`, dim: data.settings.wallpaper_dim } : { bg: "/hero.svg", dim: 0 };
  return [first, ...news.slice(0, 3).filter((n) => n.image).map((n) => ({ bg: n.image, dim: 0.35, news: n }))];
}

function renderSlides() {
  const all = slides();
  slide = Math.min(slide, all.length - 1);
  const s = all[slide];
  const bg = $("hero-bg");
  const url = `url("${s.bg}")`;
  if (bg.style.backgroundImage !== url) bg.style.backgroundImage = url;
  bg.style.setProperty("--dim", `rgba(0,0,0,${Math.min(Math.max(s.dim || 0, 0), 0.9)})`);
  const link = $("hero-news-link");
  show(link, !!s.news);
  if (s.news) { link.textContent = s.news.title + "  →"; link.href = s.news.link; }
  $("hero-dots").replaceChildren(...all.map((_, i) => el("button", {
    class: i === slide ? "on" : "", "aria-label": `Slide ${i + 1}`, onclick: () => setSlide(i),
  })));
  for (const id of ["hero-prev", "hero-next"]) show($(id), all.length > 1);
}
function setSlide(i) {
  const n = slides().length;
  slide = ((i % n) + n) % n;
  renderSlides();
  clearInterval(slideTimer);
  slideTimer = setInterval(() => setSlide(slide + 1), 9000);
}
$("hero-prev").addEventListener("click", () => setSlide(slide - 1));
$("hero-next").addEventListener("click", () => setSlide(slide + 1));

// ------------------------------------------------------------ home content

async function loadNews() {
  try {
    news = await api("/api/news");
  } catch (e) {
    $("home-news").replaceChildren(el("p", { class: "muted", textContent: "News is unavailable right now." }));
    return;
  }
  $("home-news").replaceChildren(...news.slice(0, 3).map((n) => el("a", { class: "news-item", href: n.link, target: "_blank", rel: "noopener noreferrer" },
    img(n.thumb || n.image),
    el("div", {},
      el("div", { class: "news-title", textContent: n.title }),
      el("div", { class: "news-text", textContent: n.text }),
      el("div", { class: "news-date", textContent: niceDate(n.date) })))));
  if (!news.length) $("home-news").replaceChildren(el("p", { class: "muted", textContent: "No news right now." }));
  renderSlides();
  setSlide(slide);
}

function viewButton(url, label = "View") {
  return el("a", { class: "btn primary small", href: url, target: "_blank", rel: "noopener noreferrer", textContent: label });
}

// ------------------------------------------------------------ installing mods (Chunky)

let installed = { instance: null, mods: [] };
const installedProjects = () => new Set(installed.mods.map((m) => m.project_id).filter(Boolean));

async function loadInstalled() {
  if (!selected) {
    installed = { instance: null, mods: [] };
  } else {
    try {
      installed = await api(`/api/mods?instance=${encodeURIComponent(selected)}`);
    } catch {
      installed = { instance: selected, mods: [] };
    }
  }
  renderInstalled();
  refreshInstallButtons();
}

/** An Install button for a Modrinth project, plus a small link to its page. */
function installControls(p) {
  const btn = el("button", { class: "btn primary small", "data-project": p.project_id || p.slug });
  btn.addEventListener("click", () => installMod(p, btn));
  const box = el("span", { class: "install-box" }, btn, " ", el("a", {
    class: "view-link", href: p.url, target: "_blank", rel: "noopener noreferrer", textContent: "page",
  }));
  setInstallState(btn);
  return box;
}

function setInstallState(btn) {
  const done = installedProjects().has(btn.dataset.project);
  btn.classList.toggle("done", done);
  btn.classList.toggle("primary", !done);
  if (!btn.dataset.busy) btn.textContent = done ? "Installed ✓" : "Install";
  btn.disabled = done || !!btn.dataset.busy || !selected;
}

function refreshInstallButtons() {
  document.querySelectorAll("button[data-project]").forEach(setInstallState);
  const inst = current();
  $("mods-target").textContent = inst ? `${inst.name} (Minecraft ${inst.version})` : "no version selected";
}

async function installMod(p, btn) {
  const inst = current();
  if (!inst) return go("versions");
  if (inst.loader !== "chunky" && !confirm(`Mods run on Chunky. Switch "${inst.name}" to Chunky and install ${p.title}?`)) return;
  btn.dataset.busy = "1";
  btn.textContent = "Installing...";
  btn.disabled = true;
  setStatus(`Installing ${p.title}...`, "busy");
  try {
    const out = await api("/api/mods/install", { instance: inst.name, project: p.project_id || p.slug });
    setStatus(out.message);
    if (out.switched) { editorFor = null; await refresh(); }
  } catch (e) {
    setStatus(`Could not install ${p.title}: ${e.message}`, "err");
  } finally {
    delete btn.dataset.busy;
    await loadInstalled();
  }
}

function renderInstalled() {
  const list = $("installed-list");
  const inst = current();
  if (!inst) {
    list.replaceChildren();
    return;
  }
  if (!installed.mods.length) {
    list.replaceChildren(el("li", { class: "muted", textContent: inst.loader === "chunky"
      ? "No mods yet. Use Browse mods to add some."
      : "Pick Chunky as the mod loader (or install a mod) to add mods to this version." }));
    return;
  }
  list.replaceChildren(...installed.mods.map((m) => el("li", { class: m.enabled ? "" : "off" },
    el("input", { type: "checkbox", checked: m.enabled, title: m.enabled ? "Turn off" : "Turn on",
      onchange: (e) => run(async () => {
        await api("/api/mods/toggle", { instance: inst.name, file: m.file, enabled: e.target.checked });
        await loadInstalled();
      }) }),
    el("div", { class: "mod-text" },
      el("strong", { textContent: m.name }), " ",
      el("small", { textContent: `${m.version || ""}  ·  ${m.file}` })),
    el("button", { class: "btn", textContent: "Remove", onclick: () => run(async () => {
      if (!confirm(`Remove ${m.name}?`)) return;
      await api("/api/mods/remove", { instance: inst.name, file: m.file });
      await loadInstalled();
    }) }))));
}

async function browse(type, q = "", limit = 20, version = "") {
  const params = new URLSearchParams({ type, q, limit: String(limit) });
  if (version) params.set("version", version);
  return api(`/api/browse?${params}`);
}

async function loadHomeMods() {
  const version = current()?.version || "";
  try {
    let mods = await browse("mod", "", 4, version);
    if (mods.length < 4 && version) mods = await browse("mod", "", 4);
    $("home-mods").replaceChildren(...mods.map((m) => el("div", { class: "mod-card", title: m.description },
      img(m.icon),
      el("div", { class: "mod-name", textContent: m.title }),
      el("div", { class: "mod-meta", textContent: `${compact(m.downloads)} downloads` }),
      installControls(m))));
  } catch {
    $("home-mods").replaceChildren(el("p", { class: "muted", textContent: "Modrinth is unavailable right now." }));
  }
}

async function loadHomePacks() {
  try {
    const packs = await browse("modpack", "", 2);
    $("home-packs").replaceChildren(...packs.map((p) => el("div", { class: "pack" },
      img(p.image || p.icon),
      el("div", { class: "pack-body" },
        el("div", { class: "pack-title", textContent: p.title }),
        el("div", { class: "pack-desc", textContent: p.description }),
        viewButton(p.url)))));
  } catch {
    $("home-packs").replaceChildren(el("p", { class: "muted", textContent: "Modrinth is unavailable right now." }));
  }
}

// ------------------------------------------------------------ Mods and Modpacks pages

async function searchBrowse(type) {
  const box = $(type === "mod" ? "mods-results" : "packs-results");
  const q = $(type === "mod" ? "mods-q" : "packs-q").value.trim();
  box.replaceChildren(el("p", { class: "muted", textContent: "Searching Modrinth..." }));
  try {
    const items = await browse(type, q, 24, type === "mod" && !q ? current()?.version || "" : "");
    if (!items.length) {
      box.replaceChildren(el("p", { class: "muted", textContent: "Nothing found." }));
      return;
    }
    box.replaceChildren(...items.map((m) => el("div", { class: "browse-card" },
      img(m.icon),
      el("div", {},
        el("strong", { textContent: m.title }),
        el("div", { class: "desc", textContent: m.description }),
        el("div", { class: "meta" },
          el("span", { textContent: `by ${m.author}` }),
          el("span", { textContent: `${compact(m.downloads)} downloads` }),
          type === "mod" ? installControls(m) : viewButton(m.url))))));
  } catch (e) {
    box.replaceChildren(el("p", { class: "muted", textContent: e.message }));
  }
}
function debounce(fn, ms) { let t; return () => { clearTimeout(t); t = setTimeout(fn, ms); }; }
$("mods-q").addEventListener("input", debounce(() => searchBrowse("mod"), 350));
$("packs-q").addEventListener("input", debounce(() => searchBrowse("modpack"), 350));

// ------------------------------------------------------------ Versions page

function versionChoices() {
  return data.versions
    .filter((v) => v.kind === "release" || (data.settings.show_snapshots && v.kind === "snapshot"))
    .map((v) => [v.id, v.id]);
}

function renderVersions() {
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
      onclick: () => { selectInstance(i.name); hideConfirm(); },
    }, el("strong", { textContent: i.name }), el("small", { textContent: `Minecraft ${i.version}` })))));
  if (!data.instances.length) $("instance-list").append(el("li", { class: "muted", textContent: "No versions yet. Create one above." }));

  const inst = current();
  show($("editor-empty"), !inst);
  show($("editor-form"), !!inst);
  if (!inst || editorFor === inst.name) return;

  editorFor = inst.name;
  $("editor-title").textContent = `${inst.name}  ·  Minecraft ${inst.version}`;
  const mem = $("memory");
  mem.max = String(Math.max(data.memory.max, inst.memory_mb));
  mem.value = String(inst.memory_mb);
  $("memory-out").textContent = `${inst.memory_mb} MB`;
  fillSelect($("profile"), data.profiles.map((p) => [p.id, p.label]), inst.profile);
  renderProfileDesc();
  $("jvm-args").value = (inst.jvm_args || []).join(" ");
  $("java-path").value = inst.java_path || "";
  $("loader").value = inst.loader || "";
  renderLoaderDesc();
  loadInstalled();
}

/** Chunky needs Minecraft 26.1 or newer: those versions have real class names. */
function chunkySupports(version) {
  const major = parseInt(version, 10);
  return major >= 26 && major < 1000;
}

function renderLoaderDesc() {
  const inst = current();
  const chunkyOpt = $("loader").querySelector('option[value="chunky"]');
  const ok = inst && chunkySupports(inst.version) && data.chunky.available;
  chunkyOpt.disabled = !ok;
  $("loader-desc").textContent = !data.chunky.available
    ? "This build of Java4U doesn't include Chunky."
    : !ok ? "Chunky runs Minecraft 26.1 and newer."
    : $("loader").value === "chunky"
      ? `Chunky ${data.chunky.version}: Java4U's own lightweight mod loader. Runs Fabric mods with no remapping on 26.x.`
      : "Plain Minecraft, no mods.";
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
$("show-snapshots").addEventListener("change", (e) => saveSettings({ show_snapshots: e.target.checked }));
$("create").addEventListener("click", () => run(async () => {
  const inst = await api("/api/instances", { name: $("new-name").value.trim(), version: $("new-version").value });
  $("new-name").value = "";
  selected = inst.name;
  editorFor = null;
  await refresh();
  loadHomeMods();
}));
$("memory").addEventListener("input", (e) => { $("memory-out").textContent = `${e.target.value} MB`; });
$("memory-auto").addEventListener("click", () => {
  $("memory").value = String(data.memory.recommended);
  $("memory-out").textContent = `${data.memory.recommended} MB`;
});
$("profile").addEventListener("change", renderProfileDesc);
$("loader").addEventListener("change", renderLoaderDesc);
$("save").addEventListener("click", () => saveInstance({
  memory_mb: Number($("memory").value),
  profile: $("profile").value,
  jvm_args: $("jvm-args").value,
  java_path: $("java-path").value,
  loader: $("loader").value,
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

// ------------------------------------------------------------ Account page

function renderAccounts() {
  const list = $("account-list");
  if (!data.accounts.length) {
    list.replaceChildren(el("li", { class: "muted", textContent: "No accounts yet. Sign in with the Microsoft account that owns Minecraft." }));
    return;
  }
  list.replaceChildren(...data.accounts.map((a) => el("li", {},
    avatarFor(a),
    el("strong", { textContent: a.username }),
    !offline() && a.username === account ? el("span", { class: "muted", textContent: "(playing as)" }) :
      el("button", { class: "btn", textContent: "Play as", onclick: () => chooseAccount(a.username) }),
    el("span", { class: "spacer" }),
    el("button", { class: "btn", textContent: "Remove", onclick: () => run(async () => {
      await api("/api/accounts/remove", { username: a.username });
      await refresh();
    }) }))));
}

$("login").addEventListener("click", () => run(async () => {
  await api("/api/login", { client_id: $("client-id").value });
  poll();
}));
$("copy-code").addEventListener("click", () => {
  navigator.clipboard.writeText($("device-user-code").textContent).then(
    () => { $("copy-code").textContent = "Copied"; setTimeout(() => { $("copy-code").textContent = "Copy code"; }, 1500); },
    () => setStatus("Could not copy; select the code and copy it yourself.", "err"));
});

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

// ------------------------------------------------------------ Settings page

function renderSettings() {
  const s = data.settings;
  $("no-wallpaper").disabled = !s.wallpaper;
  if (document.activeElement !== $("dim-slider")) $("dim-slider").value = String(s.wallpaper_dim);
  $("play-offline").checked = !!s.play_offline;
  if (document.activeElement !== $("offline-name")) $("offline-name").value = s.offline_username || "Player";
  $("wallpaper-list").replaceChildren(...data.wallpapers.map((name) => {
    const active = name === s.wallpaper;
    return el("li", {},
      el("img", { src: `/wallpapers/${encodeURIComponent(name)}`, alt: "" }),
      el("span", { class: "name", textContent: name.replace(/\.jpg$/, "") + (active ? "  (in use)" : "") }),
      el("button", { class: "btn", textContent: "Use", disabled: active, onclick: () => { slide = 0; saveSettings({ wallpaper: name }); } }),
      el("button", { class: "btn", textContent: "Delete", onclick: () => run(async () => {
        await api("/api/wallpapers/delete", { name });
        await refresh();
      }) }));
  }));
}

async function uploadWallpaper(file) {
  setStatus("Importing image...", "busy");
  const form = new FormData();
  form.append("file", file);
  await run(async () => {
    const res = await fetch("/api/wallpapers", { method: "POST", body: form });
    const out = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(out.error || res.statusText);
    slide = 0;
    await refresh();
    setStatus("Banner image added.");
  });
}
$("wallpaper-file").addEventListener("change", (e) => {
  const f = e.target.files[0];
  e.target.value = "";
  if (f) uploadWallpaper(f);
});
$("no-wallpaper").addEventListener("click", () => { slide = 0; saveSettings({ wallpaper: "" }); });
$("dim-slider").addEventListener("input", (e) => {
  data.settings.wallpaper_dim = Number(e.target.value);
  renderSlides();
});
$("dim-slider").addEventListener("change", (e) => saveSettings({ wallpaper_dim: Number(e.target.value) }));
$("play-offline").addEventListener("change", (e) => saveSettings({ play_offline: e.target.checked }));
$("offline-name").addEventListener("change", (e) => {
  const name = e.target.value.trim();
  if (!/^[A-Za-z0-9_]{3,16}$/.test(name)) { setStatus("Offline names are 3-16 letters, digits or underscores.", "err"); return; }
  saveSettings({ offline_username: name });
});

// Drop an image anywhere to use it as the banner.
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

// ------------------------------------------------------------ quit and polling

$("quit").addEventListener("click", () => {
  if (lastState.running && !confirm("Minecraft keeps running after Java4U closes. Close Java4U?")) return;
  if (lastState.busy && !lastState.running && !confirm("A download is still in progress. Close Java4U anyway?")) return;
  api("/api/quit", {}).finally(() => {
    document.body.replaceChildren(el("p", { class: "card", textContent: "Java4U has closed. You can close this window." }));
    window.close();
  });
});

let pollTimer = null;
let lastServerStatus = "";
async function poll() {
  clearTimeout(pollTimer);
  let next = 1000;
  try {
    const st = await api(`/api/state?epoch=${logEpoch}&since=${logNext}`);
    const loginFinished = lastState.logging_in && !st.logging_in;
    const launchFinished = lastState.busy && !st.busy;
    lastState = st;
    appendLog(st.log, st.log_reset);
    logEpoch = st.log_epoch;
    logNext = st.log_next;
    renderDeviceCode(st.device_code);
    $("login").disabled = st.logging_in;

    const p = st.progress;
    show($("progress"), !!p);
    if (p) {
      const frac = p.total === 0 ? 1 : p.done / p.total;
      $("progress-fill").style.width = `${Math.round(frac * 100)}%`;
      $("progress-text").textContent = `${p.stage}  ${p.done}/${p.total}`;
    }
    renderLaunch();

    // Status bar: newest server message, otherwise what the launcher is doing.
    if (st.status && st.status !== lastServerStatus) {
      lastServerStatus = st.status;
      setStatus(st.status, /fail|could not|error/i.test(st.status) ? "err" : "");
    } else if (st.running) {
      setStatus("Minecraft is running.", "busy");
    } else if (st.busy) {
      setStatus(p ? `Downloading ${p.stage}...` : "Preparing Minecraft...", "busy");
    } else if (launchFinished) {
      const last = $("log").textContent.trim().split("\n").pop() || "";
      if (last.startsWith("Error:")) { setStatus(last, "err"); show($("drawer"), true); }
      else setStatus("Game closed. Launcher ready!");
    }

    if (loginFinished) await refresh();
    if (st.busy || st.logging_in) next = 400;
    if (data && data.versions.length === 0 && !data.versions_error) await refresh();
  } catch (e) {
    setStatus("Lost connection to Java4U. Is it still running?", "err");
    next = 3000;
  }
  pollTimer = setTimeout(poll, next);
}

// ------------------------------------------------------------ start

drawArt();
drawIcons();
run(async () => {
  await refresh();
  await loadInstalled();
  loadNews();
  loadHomeMods();
  loadHomePacks();
  setSlide(0);
}).then(poll);
