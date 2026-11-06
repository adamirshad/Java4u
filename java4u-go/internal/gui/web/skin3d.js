// A 3D player model built from CSS boxes, textured with the player's skin.
// Drag to turn it; it turns slowly on its own otherwise.
"use strict";

const SKIN_SCALE = 7; // screen pixels per skin pixel

// One box of the model. [u, v] is where its net starts in the skin; w/h/d in skin pixels;
// x/y/z is the box centre, with the model centred on 0,0,0 and y pointing down.
function skinParts(slim) {
  const aw = slim ? 3 : 4;
  const ax = 4 + aw / 2;
  return [
    { uv: [0, 0], over: [32, 0], w: 8, h: 8, d: 8, x: 0, y: -12, z: 0, grow: 1.125 },
    { uv: [16, 16], over: [16, 32], w: 8, h: 12, d: 4, x: 0, y: -2, z: 0, grow: 1.08 },
    { uv: [40, 16], over: [40, 32], w: aw, h: 12, d: 4, x: -ax, y: -2, z: 0, grow: 1.1, arm: 1 },
    { uv: [32, 48], over: [48, 48], w: aw, h: 12, d: 4, x: ax, y: -2, z: 0, grow: 1.1, arm: -1 },
    { uv: [0, 16], over: [0, 32], w: 4, h: 12, d: 4, x: -2, y: 10, z: 0, grow: 1.1, leg: 1 },
    { uv: [16, 48], over: [0, 48], w: 4, h: 12, d: 4, x: 2, y: 10, z: 0, grow: 1.1, leg: -1 },
  ];
}

function skinBox(url, [u, v], w, h, d, grow) {
  const S = SKIN_SCALE * grow;
  const W = w * S, H = h * S, D = d * S;
  const box = document.createElement("div");
  box.className = "skin-box";
  // [skin x, skin y, width, height in skin px, transform]
  const faces = [
    [u + d, v + d, w, h, `translateZ(${D / 2}px)`],
    [u + 2 * d + w, v + d, w, h, `rotateY(180deg) translateZ(${D / 2}px)`],
    [u, v + d, d, h, `rotateY(-90deg) translateZ(${W / 2}px)`],
    [u + d + w, v + d, d, h, `rotateY(90deg) translateZ(${W / 2}px)`],
    [u + d, v, w, d, `rotateX(90deg) translateZ(${H / 2}px)`],
    [u + d + w, v, w, d, `rotateX(-90deg) translateZ(${H / 2}px)`],
  ];
  for (const [fx, fy, fw, fh, transform] of faces) {
    const f = document.createElement("div");
    f.className = "skin-face";
    const st = f.style;
    st.width = `${fw * S}px`;
    st.height = `${fh * S}px`;
    st.left = `${-fw * S / 2}px`;
    st.top = `${-fh * S / 2}px`;
    st.backgroundImage = `url("${url}")`;
    st.backgroundSize = `${64 * S}px ${64 * S}px`;
    st.backgroundPosition = `${-fx * S}px ${-fy * S}px`;
    st.transform = transform;
    box.append(f);
  }
  return box;
}

// mountSkinViewer fills `host` with a turning model of the player `uuid`.
// It returns a function that stops the animation.
async function mountSkinViewer(host, uuid) {
  host.replaceChildren();
  let slim = false;
  try {
    const r = await fetch(`/api/skin/${encodeURIComponent(uuid)}?info=1`);
    if (!r.ok) throw new Error();
    slim = (await r.json()).slim;
  } catch {
    host.append(Object.assign(document.createElement("p"), { className: "muted", textContent: "Skin not available." }));
    return () => {};
  }
  const url = `/api/skin/${encodeURIComponent(uuid)}`;
  const scene = document.createElement("div");
  scene.className = "skin-scene";
  const model = document.createElement("div");
  model.className = "skin-model";
  scene.append(model);
  host.append(scene);

  const limbs = [];
  for (const p of skinParts(slim)) {
    const part = document.createElement("div");
    part.className = "skin-part";
    part.append(skinBox(url, p.uv, p.w, p.h, p.d, 1), skinBox(url, p.over, p.w, p.h, p.d, p.grow));
    const S = SKIN_SCALE;
    const base = `translate3d(${p.x * S}px, ${p.y * S}px, ${p.z * S}px)`;
    part.style.transform = base;
    if (p.arm || p.leg) limbs.push({ part, base, sign: p.arm || -p.leg, top: (p.h / 2) * S });
    model.append(part);
  }

  let angle = -30, dragging = false, lastX = 0, t = 0, raf = 0, idle = true;
  const draw = () => {
    model.style.transform = `rotateX(-12deg) rotateY(${angle}deg)`;
    const swing = Math.sin(t / 30) * 12;
    for (const l of limbs) {
      // Swing around the shoulder / hip.
      l.part.style.transform = `${l.base} translateY(${-l.top}px) rotateX(${swing * l.sign}deg) translateY(${l.top}px)`;
    }
  };
  const tick = () => {
    t++;
    if (idle && !dragging) angle += 0.35;
    draw();
    raf = requestAnimationFrame(tick);
  };
  scene.addEventListener("pointerdown", (e) => { dragging = true; idle = false; lastX = e.clientX; scene.setPointerCapture(e.pointerId); });
  scene.addEventListener("pointermove", (e) => { if (dragging) { angle += (e.clientX - lastX) * 0.8; lastX = e.clientX; } });
  const stopDrag = () => { dragging = false; setTimeout(() => { idle = true; }, 2500); };
  scene.addEventListener("pointerup", stopDrag);
  scene.addEventListener("pointercancel", stopDrag);
  tick();
  return () => cancelAnimationFrame(raf);
}
