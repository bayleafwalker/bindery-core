const canvas = document.getElementById("viewport");
const statusEl = document.getElementById("status");
const statsEl = document.getElementById("stats");
const resetBtn = document.getElementById("resetBtn");
const ctx = canvas.getContext("2d");

function resize() {
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.floor(window.innerWidth * dpr);
  canvas.height = Math.floor(window.innerHeight * dpr);
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
}

window.addEventListener("resize", resize);
resize();

async function resetWorld() {
  resetBtn.disabled = true;
  try {
    const res = await fetch("/api/reset", { method: "POST" });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(body.error || `HTTP ${res.status}`);
    }
  } finally {
    resetBtn.disabled = false;
  }
}

resetBtn.addEventListener("click", () => {
  resetWorld().catch((e) => {
    statusEl.textContent = `reset failed: ${e}`;
    statusEl.style.color = "#ffb84d";
  });
});

function lerp(a, b, t) {
  return a + (b - a) * t;
}

function draw(state) {
  const width = window.innerWidth;
  const height = window.innerHeight;

  ctx.clearRect(0, 0, width, height);

  const entities = state.entities || [];
  const planets = entities.filter((e) => e.kind === "planet");
  const ships = entities.filter((e) => e.kind === "ship");

  const xs = entities.map((e) => e.x);
  const ys = entities.map((e) => e.y);

  let minX = Math.min(...xs, -120);
  let maxX = Math.max(...xs, 120);
  let minY = Math.min(...ys, -120);
  let maxY = Math.max(...ys, 120);

  const pad = 40;
  minX -= pad;
  maxX += pad;
  minY -= pad;
  maxY += pad;

  const worldW = Math.max(1, maxX - minX);
  const worldH = Math.max(1, maxY - minY);
  const scale = Math.min(width / worldW, height / worldH);

  const centerX = lerp(minX, maxX, 0.5);
  const centerY = lerp(minY, maxY, 0.5);

  const toScreen = (x, y) => {
    const sx = (x - centerX) * scale + width / 2;
    const sy = (y - centerY) * scale + height / 2;
    return [sx, sy];
  };

  // Stars
  ctx.fillStyle = "rgba(232,236,255,0.05)";
  for (let i = 0; i < 120; i++) {
    const sx = ((i * 997) % width) + 0.5;
    const sy = ((i * 613) % height) + 0.5;
    ctx.fillRect(sx, sy, 1, 1);
  }

  // Planets
  for (const p of planets) {
    const [sx, sy] = toScreen(p.x, p.y);
    const r = Math.max(6, (p.radius || 20) * scale);
    ctx.beginPath();
    ctx.arc(sx, sy, r, 0, Math.PI * 2);
    ctx.fillStyle = p.team === "blue" ? "#4d8dff" : "#ff4d6d";
    ctx.fill();
    ctx.strokeStyle = "rgba(232,236,255,0.35)";
    ctx.lineWidth = 1;
    ctx.stroke();
  }

  // Ships
  for (const s of ships) {
    const [sx, sy] = toScreen(s.x, s.y);
    const r = Math.max(2.5, 3.0 * (window.devicePixelRatio || 1));
    ctx.beginPath();
    ctx.arc(sx, sy, r, 0, Math.PI * 2);
    ctx.fillStyle = s.team === "blue" ? "#b9d1ff" : "#ffd1dc";
    ctx.fill();
  }

  // HUD text
  const redShips = ships.filter((s) => s.team === "red").length;
  const blueShips = ships.filter((s) => s.team === "blue").length;
  statsEl.textContent = `tick=${state.tick}  ships red=${redShips} blue=${blueShips}`;
}

async function poll() {
  try {
    const res = await fetch("/api/state", { cache: "no-store" });
    const state = await res.json();
    if (state.error) {
      statusEl.textContent = state.error;
      statusEl.style.color = "#ffb84d";
    } else {
      statusEl.textContent = `world=${state.worldId}`;
      statusEl.style.color = "#b0ffb0";
    }
    draw(state);
  } catch (e) {
    statusEl.textContent = `fetch failed: ${e}`;
    statusEl.style.color = "#ffb84d";
  } finally {
    setTimeout(poll, 200);
  }
}

poll();
