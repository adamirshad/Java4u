"use strict";

// Modpack installs and the Servers page. Uses helpers from app.js ($, el, api, run, ...).

// ------------------------------------------------------------ modpacks

/** Install button for a modpack, plus a link to its page. */
function packControls(p) {
  const btn = el("button", { class: "btn primary small", textContent: "Install" });
  btn.addEventListener("click", () => run(async () => {
    if (!confirm(`Install ${p.title}? Java4U will create a new version with all of its mods.`)) return;
    btn.disabled = true;
    btn.textContent = "Installing...";
    await api("/api/modpacks/install", { project: p.project_id || p.slug, title: p.title });
    setStatus(`Installing ${p.title}...`, "busy");
    poll();
  }));
  return el("span", { class: "install-box" }, btn, " ", el("a", {
    class: "view-link", href: p.url, target: "_blank", rel: "noopener noreferrer", textContent: "page",
  }));
}

// ------------------------------------------------------------ servers

let servers = { servers: [], running: false, starting: false, active: "", lan_ip: "" };
let selectedServer = null;
let serverFormFor = null;
let consoleEpoch = -1;
let consoleNext = 0;

const currentServer = () => servers.servers.find((s) => s.name === selectedServer) || null;

async function loadServers() {
  servers = await api("/api/servers");
  if (!servers.servers.some((s) => s.name === selectedServer)) {
    selectedServer = servers.active || servers.servers[0]?.name || null;
    serverFormFor = null;
  }
  renderServers();
}

function renderServers() {
  if (data) {
    const choices = data.versions.filter((v) => v.kind === "release").map((v) => [v.id, v.id]);
    if (choices.length && !$("srv-new-version").childElementCount) fillSelect($("srv-new-version"), choices, choices[0][0]);
  }
  $("server-list").replaceChildren(...servers.servers.map((s) => el("li", {},
    el("button", {
      class: "item" + (s.name === selectedServer ? " selected" : ""),
      onclick: () => { selectedServer = s.name; serverFormFor = null; renderServers(); loadServerMods(); },
    },
    el("strong", {}, servers.active === s.name && (servers.running || servers.starting) ? el("span", { class: "server-dot" }) : "", s.name),
    el("small", { textContent: `Minecraft ${s.version}${s.loader === "chunky" ? " · Chunky" : ""} · ${s.mods} mods` })))));
  if (!servers.servers.length) $("server-list").append(el("li", { class: "muted", textContent: "No servers yet." }));

  const s = currentServer();
  show($("srv-empty"), !s);
  show($("srv-form"), !!s);
  if (!s) return;

  const mine = servers.active === s.name;
  const live = mine && servers.running;
  const busy = servers.running || servers.starting;
  $("srv-start").disabled = busy || !s.eula;
  $("srv-stop").disabled = !live;
  $("srv-send").disabled = !live;
  $("srv-state").textContent = mine && servers.starting ? "Starting..." : live ? "Running" : busy ? `"${servers.active}" is running` : "Stopped";
  $("srv-address").textContent = `Players join at localhost (this PC)` +
    (servers.lan_ip ? ` or ${servers.lan_ip} (your home network). Port 25565.` : ".");
  $("srv-eula").checked = !!s.eula;
  $("srv-eula").disabled = !!s.eula;

  if (serverFormFor === s.name) return;
  serverFormFor = s.name;
  $("srv-title").textContent = `${s.name}  ·  Minecraft ${s.version}`;
  const mem = $("srv-memory");
  mem.max = String(Math.max(data?.memory.max || 8192, s.memory_mb));
  mem.value = String(s.memory_mb);
  $("srv-memory-out").textContent = `${s.memory_mb} MB`;
  $("srv-loader").value = s.loader || "";
  $("srv-loader").querySelector('option[value="chunky"]').disabled = !chunkySupports(s.version);
}

async function loadServerMods() {
  const s = currentServer();
  if (!s) return;
  const out = await api(`/api/servers/mods?server=${encodeURIComponent(s.name)}`).catch(() => ({ mods: [] }));
  $("srv-mods").replaceChildren(...out.mods.map((m) => el("li", { class: m.enabled ? "" : "off" },
    el("input", { type: "checkbox", checked: m.enabled, onchange: (e) => run(async () => {
      await api("/api/servers/mods/change", { server: s.name, file: m.file, enabled: e.target.checked });
      loadServerMods();
    }) }),
    el("div", { class: "mod-text" }, el("strong", { textContent: m.name }), " ", el("small", { textContent: m.version || m.file })),
    el("button", { class: "btn", textContent: "Remove", onclick: () => run(async () => {
      await api("/api/servers/mods/change", { server: s.name, file: m.file, remove: true });
      loadServerMods();
      loadServers();
    }) }))));
  if (!out.mods.length) $("srv-mods").append(el("li", { class: "muted", textContent: "No mods yet. Search above to add some." }));
}

async function searchServerMods() {
  const box = $("srv-mod-results");
  const q = $("srv-mod-q").value.trim();
  if (!q) { box.replaceChildren(); return; }
  const s = currentServer();
  try {
    const items = await browse("mod", q, 8, s?.version || "");
    box.replaceChildren(...items.map((m) => el("div", { class: "browse-card" },
      img(m.icon),
      el("div", {},
        el("strong", { textContent: m.title }),
        el("div", { class: "meta" },
          el("span", { textContent: `${compact(m.downloads)} downloads` }),
          el("button", { class: "btn primary small", textContent: "Install", onclick: (e) => run(async () => {
            e.target.disabled = true;
            e.target.textContent = "Installing...";
            const out = await api("/api/servers/mods/install", { server: s.name, project: m.project_id || m.slug });
            setStatus(out.message);
            e.target.textContent = "Installed ✓";
            loadServerMods();
            loadServers();
          }).finally(() => { if (e.target.textContent === "Installing...") { e.target.disabled = false; e.target.textContent = "Install"; } }) }))))));
    if (!items.length) box.replaceChildren(el("p", { class: "muted", textContent: "Nothing found." }));
  } catch (e) {
    box.replaceChildren(el("p", { class: "muted", textContent: e.message }));
  }
}

async function pollConsole() {
  try {
    const c = await api(`/api/servers/console?epoch=${consoleEpoch}&since=${consoleNext}`);
    const con = $("srv-console");
    const atBottom = con.scrollHeight - con.scrollTop - con.clientHeight < 30;
    if (c.reset) con.textContent = "";
    if (c.log.length) con.append(c.log.join("\n") + "\n");
    while (con.childNodes.length > 300) con.firstChild.remove();
    if (atBottom || c.reset) con.scrollTop = con.scrollHeight;
    consoleEpoch = c.epoch;
    consoleNext = c.next;
    const changed = c.running !== servers.running || c.starting !== servers.starting;
    servers.running = c.running;
    servers.starting = c.starting;
    servers.active = c.name;
    if (changed) renderServers();
  } catch { /* the main poll reports connection problems */ }
  const onPage = !$("page-servers").classList.contains("hidden");
  setTimeout(pollConsole, onPage || servers.running ? 1000 : 4000);
}

$("srv-create").addEventListener("click", () => run(async () => {
  const s = await api("/api/servers", { name: $("srv-new-name").value.trim(), version: $("srv-new-version").value });
  $("srv-new-name").value = "";
  selectedServer = s.name;
  serverFormFor = null;
  await loadServers();
  loadServerMods();
}));
$("srv-memory").addEventListener("input", (e) => { $("srv-memory-out").textContent = `${e.target.value} MB`; });
$("srv-save").addEventListener("click", () => run(async () => {
  await api("/api/servers/save", { name: selectedServer, memory_mb: Number($("srv-memory").value), loader: $("srv-loader").value });
  serverFormFor = null;
  setStatus("Server saved.");
  await loadServers();
}));
$("srv-eula").addEventListener("change", (e) => run(async () => {
  if (!e.target.checked) return;
  await api("/api/servers/eula", { name: selectedServer, agree: true });
  await loadServers();
}));
$("srv-start").addEventListener("click", () => run(async () => {
  await api("/api/servers/start", { name: selectedServer });
  servers.starting = true;
  servers.active = selectedServer;
  renderServers();
}));
$("srv-stop").addEventListener("click", () => run(() => api("/api/servers/stop", {})));
function sendCommand() {
  const cmd = $("srv-cmd").value.trim();
  if (!cmd) return;
  run(async () => { await api("/api/servers/command", { command: cmd }); $("srv-cmd").value = ""; });
}
$("srv-send").addEventListener("click", sendCommand);
$("srv-cmd").addEventListener("keydown", (e) => { if (e.key === "Enter") sendCommand(); });
$("srv-mod-q").addEventListener("input", debounce(searchServerMods, 400));
$("srv-delete").addEventListener("click", () => run(async () => {
  if (!confirm(`Delete the server "${selectedServer}" and its world? This can't be undone.`)) return;
  await api("/api/servers/delete", { name: selectedServer });
  selectedServer = null;
  await loadServers();
}));
document.querySelector('.nav[data-page="servers"]').addEventListener("click", () => run(async () => {
  await loadServers();
  loadServerMods();
}));

pollConsole();
