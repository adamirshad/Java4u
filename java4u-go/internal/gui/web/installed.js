"use strict";

// The Installed view on the Mods page: installed modpacks (switch a whole pack on or off)
// and installed mods in any version (switch each on or off). Uses helpers from app.js.

let installedView = [];   // one entry per version: mods, how many are on, its pack
let pickedVersion = null; // version shown in "Installed mods"
let pickedMods = [];

/** An accessible on/off switch. */
function switchEl(checked, label, onchange, disabled = false) {
  const input = el("input", { type: "checkbox", checked, disabled, "aria-label": label, title: label });
  input.addEventListener("change", () => onchange(input.checked, input));
  return el("label", { class: "switch" }, input, el("span", { class: "track" }));
}

function showModsTab(which) {
  const installedTab = which === "installed";
  $("mods-tab-browse").classList.toggle("on", !installedTab);
  $("mods-tab-installed").classList.toggle("on", installedTab);
  $("mods-tab-browse").setAttribute("aria-selected", String(!installedTab));
  $("mods-tab-installed").setAttribute("aria-selected", String(installedTab));
  show($("mods-browse-head"), !installedTab);
  show($("mods-results"), !installedTab);
  show($("mods-installed"), installedTab);
  if (installedTab) loadInstalledView();
}

async function loadInstalledView() {
  try {
    installedView = await api("/api/mods/installed");
  } catch (e) {
    setStatus(e.message, "err");
    return;
  }
  if (!installedView.some((v) => v.instance === pickedVersion)) pickedVersion = selected || installedView[0]?.instance || null;
  renderPacks();
  fillSelect($("inst-pick"), installedView.map((v) =>
    [v.instance, `${v.instance}  (Minecraft ${v.version}${v.modpack ? ", pack" : ""}, ${v.mods} mods)`]), pickedVersion);
  await loadPickedMods();
}

function renderPacks() {
  const packs = installedView.filter((v) => v.modpack);
  if (!packs.length) {
    $("pack-list").replaceChildren(el("p", { class: "muted" }, "No modpacks installed yet. ",
      el("a", { href: "#", textContent: "Browse modpacks", onclick: (e) => { e.preventDefault(); go("modpacks"); } })));
    return;
  }
  $("pack-list").replaceChildren(...packs.map((p) => {
    const on = p.enabled > 0;
    return el("div", { class: "pack-row" + (on ? "" : " off") },
      switchEl(on, `${on ? "Turn off" : "Turn on"} ${p.modpack}`, (checked, input) => run(async () => {
        input.disabled = true;
        await api("/api/mods/toggle-all", { instance: p.instance, enabled: checked });
        await loadInstalledView();
        if (p.instance === selected) loadInstalled(true);
      })),
      el("div", { class: "pack-row-text" },
        el("strong", { textContent: p.modpack }),
        el("small", { textContent: `Version "${p.instance}" · Minecraft ${p.version} · ${p.enabled} of ${p.mods} mods on` })),
      el("button", { class: "btn", textContent: "Show mods", onclick: () => { pickedVersion = p.instance; loadInstalledView(); $("inst-pick").scrollIntoView({ block: "center" }); } }),
      el("button", { class: "btn primary", textContent: "Play", onclick: () => { selectInstance(p.instance); go("home"); } }),
      el("button", { class: "btn", textContent: "Remove", onclick: () => run(async () => {
        if (!confirm(`Remove the ${p.modpack} pack? This deletes its version "${p.instance}", including its worlds.`)) return;
        await api("/api/instances/delete", { name: p.instance });
        await refresh();
        await loadInstalledView();
      }) }));
  }));
}

async function loadPickedMods() {
  if (!pickedVersion) {
    pickedMods = [];
    renderPickedMods();
    return;
  }
  const out = await api(`/api/mods?instance=${encodeURIComponent(pickedVersion)}`).catch(() => ({ mods: [] }));
  pickedMods = out.mods;
  renderPickedMods();
}

function renderPickedMods() {
  const q = $("inst-filter").value.trim().toLowerCase();
  const shown = pickedMods.filter((m) => !q || m.name.toLowerCase().includes(q) || m.id.toLowerCase().includes(q) || m.file.toLowerCase().includes(q));
  const on = pickedMods.filter((m) => m.enabled).length;
  $("inst-summary").textContent = pickedVersion
    ? `${on} of ${pickedMods.length} mods on${q ? ` · showing ${shown.length}` : ""}`
    : "No versions yet.";
  for (const id of ["inst-all-on", "inst-all-off"]) $(id).disabled = !pickedMods.length;
  if (!pickedMods.length) {
    $("inst-mods").replaceChildren(el("li", { class: "muted", textContent: pickedVersion ? "No mods in this version yet." : "" }));
    return;
  }
  $("inst-mods").replaceChildren(...shown.map((m) => el("li", { class: m.enabled ? "" : "off" },
    switchEl(m.enabled, `${m.enabled ? "Turn off" : "Turn on"} ${m.name}`, (checked, input) => run(async () => {
      input.disabled = true;
      await api("/api/mods/toggle", { instance: pickedVersion, file: m.file, enabled: checked });
      m.enabled = checked;
      renderPickedMods();
      refreshPackCounts();
      if (pickedVersion === selected) loadInstalled(true);
    })),
    el("div", { class: "mod-text" },
      el("strong", { textContent: m.name }), " ",
      el("small", { textContent: `${m.version || ""}${m.description ? "  ·  " + m.description : ""}` })),
    el("button", { class: "btn", textContent: "Remove", onclick: () => run(async () => {
      if (!confirm(`Remove ${m.name}?`)) return;
      await api("/api/mods/remove", { instance: pickedVersion, file: m.file });
      await loadInstalledView();
      if (pickedVersion === selected) loadInstalled(true);
    }) }))));
}

/** Keeps the "N of M mods on" text on pack rows current without reloading everything. */
async function refreshPackCounts() {
  installedView = await api("/api/mods/installed").catch(() => installedView);
  renderPacks();
}

async function setAllPicked(enabled) {
  await run(async () => {
    await api("/api/mods/toggle-all", { instance: pickedVersion, enabled });
    await loadInstalledView();
    if (pickedVersion === selected) loadInstalled(true);
  });
}

$("mods-tab-browse").addEventListener("click", () => showModsTab("browse"));
$("mods-tab-installed").addEventListener("click", () => showModsTab("installed"));
$("inst-pick").addEventListener("change", (e) => { pickedVersion = e.target.value; loadPickedMods(); });
$("inst-filter").addEventListener("input", renderPickedMods);
$("inst-all-on").addEventListener("click", () => setAllPicked(true));
$("inst-all-off").addEventListener("click", () => setAllPicked(false));
// Refresh the Installed view whenever the Mods page is opened on it.
document.querySelector('.nav[data-page="mods"]').addEventListener("click", () => {
  if ($("mods-tab-installed").classList.contains("on")) loadInstalledView();
});
