// Worlds and screenshots in the Versions editor, plus .mrpack import and export.
"use strict";

const instPath = (name) => `/api/instances/${encodeURIComponent(name)}`;

function fmtSize(bytes) {
  if (bytes >= 1 << 30) return (bytes / (1 << 30)).toFixed(1) + " GB";
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " MB";
  return Math.max(1, Math.round(bytes / 1024)) + " KB";
}
const fmtDate = (unix) => new Date(unix * 1000).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

let filesFor = null;
async function loadGameFiles() {
  const inst = current();
  if (!inst) return;
  filesFor = inst.name;
  let files;
  try {
    files = await api(`${instPath(inst.name)}/files`);
  } catch (e) {
    setStatus(e.message, "err");
    return;
  }
  if (filesFor !== inst.name) return; // picked another version meanwhile

  const worlds = $("world-list");
  worlds.replaceChildren(...files.worlds.map((w) => el("li", {},
    w.has_icon ? img(`${instPath(inst.name)}/worlds/${encodeURIComponent(w.folder)}/icon`, "world-icon") : el("div", { class: "noimg world-icon" }),
    el("div", { class: "grow" },
      el("strong", { textContent: w.folder }),
      el("small", { class: "muted", textContent: `Last played ${fmtDate(w.last_played)}  ·  ${fmtSize(w.size_bytes)}` })))));
  if (!files.worlds.length) worlds.append(el("li", { class: "muted", textContent: "No worlds yet. Worlds you make in this version show up here." }));

  const grid = $("shot-grid");
  grid.replaceChildren(...files.screenshots.slice(0, 60).map((s) => {
    const src = `${instPath(inst.name)}/screenshots/${encodeURIComponent(s.file)}`;
    return el("button", { class: "shot", title: `${s.file}  ·  ${fmtDate(s.taken)}`, onclick: () => openShot(src) }, img(src));
  }));
  if (!files.screenshots.length) grid.append(el("p", { class: "muted", textContent: "No screenshots yet. Press F2 in game to take one." }));
  else if (files.screenshots.length > 60) grid.append(el("p", { class: "muted", textContent: `Showing the newest 60 of ${files.screenshots.length}. Open the folder to see them all.` }));
}

function openShot(src) {
  $("shot-big").src = src;
  show($("shot-viewer"), true);
}
$("shot-viewer").addEventListener("click", () => show($("shot-viewer"), false));
document.addEventListener("keydown", (e) => { if (e.key === "Escape") show($("shot-viewer"), false); });

document.querySelectorAll("[data-open-folder]").forEach((b) => b.addEventListener("click", () => run(async () => {
  const inst = current();
  if (inst) await api("/api/open-folder", { instance: inst.name, folder: b.dataset.openFolder });
})));

// Export: the server builds the pack and the browser saves it as a download.
$("export-pack").addEventListener("click", () => {
  const inst = current();
  if (!inst) return;
  setStatus(`Exporting "${inst.name}"... (looking up mods on Modrinth)`, "busy");
  const a = el("a", { href: `${instPath(inst.name)}/export`, download: `${inst.name}.mrpack` });
  document.body.append(a);
  a.click();
  a.remove();
});

$("mrpack-file").addEventListener("change", (e) => {
  const file = e.target.files[0];
  e.target.value = "";
  if (!file) return;
  run(async () => {
    const form = new FormData();
    form.append("file", file);
    setStatus(`Reading ${file.name}...`, "busy");
    const res = await fetch("/api/modpacks/import", { method: "POST", body: form });
    const out = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(out.error || res.statusText);
    statusOverride = "";
    poll();
  });
});

// ------------------------------------------------------------ updates

function renderUpdate() {
  const u = data?.update;
  show($("update-banner"), !!u);
  if (!u) return;
  $("update-text").textContent = u.installed
    ? `Java4U ${u.version} is installed.`
    : `Java4U ${u.version} is available (you have ${data.app_version}).`;
  $("update-go").textContent = u.installed ? "Restart now" : "Update now";
  $("update-go").disabled = !u.installed && !!lastState.busy;
  const notes = $("update-notes");
  notes.href = u.page || "https://github.com/adamirshad/Java4u/releases";
}

$("update-go").addEventListener("click", () => run(async () => {
  const u = data?.update;
  if (!u) return;
  if (u.installed) {
    if (lastState.running && !confirm("Minecraft keeps running. Restart Java4U anyway?")) return;
    await api("/api/update/restart", {});
    document.body.replaceChildren(el("p", { class: "card", textContent: "Java4U is restarting in a new window. You can close this one." }));
    window.close();
    return;
  }
  await api("/api/update/apply", {});
  poll();
}));

$("check-update").addEventListener("click", () => run(async () => {
  $("update-result").textContent = "Checking...";
  try {
    const out = await api("/api/update/check", {});
    $("update-result").textContent = out.update ? `Java4U ${out.update.version} is available.` : out.message;
    await refresh();
  } catch (e) {
    $("update-result").textContent = "";
    throw e;
  }
}));
