import * as THREE from "three";
import { BOUNDS, height, ridgeZ, rng } from "./terrain";
import { T, captionAt, clamp01, smooth, smoother } from "./timeline";

/* ------------------------------------------------------------------ */
/* Scene data                                                          */
/* ------------------------------------------------------------------ */

const CELL = 9; // tile size in world units
const MAX_TILES = 32;
const GRID = 6; // surface grid spacing

type Tile = { cx: number; cz: number; delay: number; pattern: number; ox: number; oy: number; oz: number; angle: number; phase: number };

type Site = { x: number; z: number; y: number; kind: "miner" | "node" | "wallet"; h: number; t0: number };

const TILE_LABELS: [string, string][] = [
  ["GENESIS", "PAYS 0"],
  ["SHA3-256", "PROOF OF WORK"],
  ["SUBSIDY", "5 GATH, HALVING"],
  ["CAP", "20,999,999.727"],
  ["KEYS", "24 WORDS"],
  ["TX", "≥ 1 ATOM / BYTE"],
  ["ML-DSA-44", "SIGNATURES"],
  ["NODES", "VERIFY ALL"],
];

function makeTiles(): Tile[] {
  const r = rng(7);
  const tiles: Tile[] = [];
  for (let cz = -54; cz <= 9 && tiles.length < MAX_TILES; cz += CELL) {
    for (let cx = -45; cx <= 45 && tiles.length < MAX_TILES; cx += CELL) {
      const keep = r();
      if (keep > 0.5) continue;
      tiles.push({
        cx: cx + CELL / 2,
        cz: cz + CELL / 2,
        delay: r() * 0.34,
        pattern: [0, 0, 1, 1, 1, 2, 3][Math.floor(r() * 7)],
        ox: (r() - 0.5) * 7,
        oy: 4 + r() * 16,
        oz: (r() - 0.5) * 7,
        angle: (r() - 0.5) * 0.5,
        phase: r() * 6.28,
      });
    }
  }
  return tiles;
}

function site(x: number, z: number, kind: Site["kind"], h: number, t0: number): Site {
  return { x, z, y: height(x, z), kind, h, t0 };
}

function makeSites(): Site[] {
  const mx = [0, 8, 15, 22, 30];
  const mdz = [2, -3, 1.5, -2, 3];
  const mh = [8.5, 13, 7, 10.5, 6.5];
  const miners = mx.map((x, i) => site(x, ridgeZ(x) + mdz[i], "miner", mh[i], T.miners[i]));
  return [
    ...miners,
    site(1, -19, "wallet", 0, T.sitesIn[0]),
    site(12, -28, "node", 2.6, T.sitesIn[0] + 0.15),
    site(22, -21, "node", 2.6, T.sitesIn[0] + 0.3),
    site(33, -31, "wallet", 0, T.sitesIn[0] + 0.45),
  ];
}

/* ------------------------------------------------------------------ */
/* Shaders                                                             */
/* ------------------------------------------------------------------ */

const terrainVert = /* glsl */ `
uniform float uTime, uSpark, uTiles, uMerge, uGrid, uSize, uDpr, uFade, uShimmer, uRef;
uniform vec4 uTileC[${MAX_TILES}];
uniform vec4 uTileO[${MAX_TILES}];
attribute vec4 aData; // rand, light, kind (0 surface, 1 grid), tile index or -1
varying float vA;

float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float ease(float x) { return x * x * (3.0 - 2.0 * x); }

void main() {
  vec3 p = position;
  float rnd = aData.x;
  float light = aData.y;
  float dens = 0.42 + 0.58 * light;
  float surfA = step(rnd, dens) * (0.3 + 0.7 * light);
  float a;
  if (aData.z > 0.5) {
    a = uGrid * (0.2 + 0.42 * light);
  } else if (aData.w > -0.5) {
    int ti = int(aData.w + 0.5);
    vec4 tc = uTileC[ti];
    vec4 to = uTileO[ti];
    float mi = ease(clamp((uMerge - tc.z) / 0.46, 0.0, 1.0));
    vec2 l = p.xz - tc.xy;
    float an = to.w * (1.0 - mi);
    float cs = cos(an), sn = sin(an);
    vec2 lr = vec2(cs * l.x - sn * l.y, sn * l.x + cs * l.y);
    float bob = sin(uTime * 0.55 + float(ti) * 1.9) * 0.3;
    vec3 tp = vec3(tc.x + to.x + lr.x, p.y * 0.22 + to.y + bob, tc.y + to.z + lr.y);
    p = mix(tp, p, mi);
    float pat;
    float k = tc.w;
    if (k < 0.5) pat = smoothstep(0.62, 0.9, 0.5 + 0.5 * sin(l.x * 2.6 + l.y * 0.45));
    else if (k < 1.5) { float c = abs(fract(position.y * 1.15) - 0.5) * 2.0; pat = smoothstep(0.72, 0.96, c) + 0.05; }
    else if (k < 2.5) pat = 0.3 + 0.5 * light;
    else pat = 0.62 + 0.38 * light;
    float fill = clamp(uTiles * 1.7 - tc.z * 1.6, 0.0, 1.0);
    float tileA = step(rnd, max(pat * fill * fill, 0.018 * uSpark * (0.3 + pat))) * (0.4 + 0.6 * pat);
    a = mix(tileA, surfA, mi);
  } else {
    vec2 cell = floor(p.xz / ${CELL.toFixed(1)});
    float d = 0.22 + 0.42 * hash(cell);
    float mc = ease(clamp((uMerge - d) / 0.34, 0.0, 1.0));
    p.y -= (1.0 - mc) * 5.0;
    a = surfA * mc;
  }
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  float dist = -mv.z;
  float rd = dist / uRef;
  a *= 1.0 - smoothstep(1.3, 1.95, rd);
  a *= 0.5 + 0.5 * smoothstep(0.45, 0.95, rd);
  a *= 1.0 + uShimmer * sin(uTime * 0.9 + rnd * 61.0);
  a *= uFade;
  vA = a;
  gl_Position = projectionMatrix * mv;
  gl_PointSize = uSize * uDpr * clamp(1.15 / rd, 0.8, 1.4);
  if (a < 0.004) gl_Position = vec4(2.0, 2.0, 2.0, 1.0);
}
`;

const pointFrag = /* glsl */ `
varying float vA;
void main() {
  vec2 c = gl_PointCoord - 0.5;
  float r = dot(c, c);
  float m = 1.0 - smoothstep(0.16, 0.25, r);
  gl_FragColor = vec4(vec3(1.0), vA * m);
}
`;

const fxVert = /* glsl */ `
uniform float uTime, uDpr, uSize, uFade, uArc, uArcA, uRef;
uniform vec4 uSite[12]; // reveal, height, intensity, unused
attribute vec3 aOff;
attribute vec4 aInfo; // kind (0 beam, 1 blob, 2 arc), site, rand, s
varying float vA;
void main() {
  vec3 p = position;
  float a = 0.0;
  float size = uSize;
  float kind = aInfo.x;
  if (kind < 1.5) {
    vec4 s = uSite[int(aInfo.y + 0.5)];
    float rev = s.x;
    if (kind < 0.5) {
      float u = aOff.y;
      float grow = 1.0 - pow(1.0 - rev, 3.0);
      p += vec3(aOff.x * (0.6 + 0.4 * grow), u * s.y * grow, aOff.z * (0.6 + 0.4 * grow));
      a = (0.1 + 0.9 * pow(1.0 - u, 1.6)) * smoothstep(0.0, 0.12, rev) * s.z;
      a *= 1.0 - smoothstep(0.82, 1.0, u);
    } else {
      float grow = smoothstep(0.0, 1.0, rev);
      p += vec3(aOff.x * grow, aOff.y, aOff.z * grow);
      float r2 = (aOff.x * aOff.x + aOff.z * aOff.z);
      a = exp(-r2 * 1.4) * grow * s.z * 0.9;
    }
  } else {
    p += aOff;
    float s = aInfo.w;
    float head = exp(-pow((uArc - s) * 28.0, 2.0));
    a = step(s, uArc) * (0.55 + 1.1 * head) * uArcA;
    size *= 1.0 + 0.8 * head;
  }
  a *= 1.0 + 0.06 * sin(uTime * 0.8 + aInfo.z * 40.0);
  a *= uFade;
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  vA = a;
  gl_Position = projectionMatrix * mv;
  gl_PointSize = size * uDpr * clamp(1.2 * uRef / -mv.z, 0.9, 2.2);
  if (a < 0.004) gl_Position = vec4(2.0, 2.0, 2.0, 1.0);
}
`;

/* ------------------------------------------------------------------ */
/* Geometry builders                                                   */
/* ------------------------------------------------------------------ */

type Tier = "high" | "low";

function heightField(res: number) {
  const nx = Math.ceil((BOUNDS.x1 - BOUNDS.x0) / res) + 2;
  const nz = Math.ceil((BOUNDS.z1 - BOUNDS.z0) / res) + 2;
  const h = new Float32Array(nx * nz);
  for (let j = 0; j < nz; j++) for (let i = 0; i < nx; i++) h[j * nx + i] = height(BOUNDS.x0 + i * res, BOUNDS.z0 + j * res);
  const sample = (x: number, z: number) => {
    const fx = (x - BOUNDS.x0) / res;
    const fz = (z - BOUNDS.z0) / res;
    const i = Math.min(nx - 2, Math.max(0, Math.floor(fx)));
    const j = Math.min(nz - 2, Math.max(0, Math.floor(fz)));
    const u = fx - i;
    const v = fz - j;
    const a = h[j * nx + i];
    const b = h[j * nx + i + 1];
    const c = h[(j + 1) * nx + i];
    const d = h[(j + 1) * nx + i + 1];
    return a + (b - a) * u + (c - a) * v + (a - b - c + d) * u * v;
  };
  return sample;
}

function buildTerrain(tier: Tier, tiles: Tile[]) {
  const rows = tier === "high" ? 620 : 370;
  const cols = tier === "high" ? 1200 : 700;
  const H = heightField(0.3);
  const r = rng(11);
  const W = BOUNDS.x1 - BOUNDS.x0;
  const D = BOUNDS.z1 - BOUNDS.z0;
  const dz = D / rows;
  const dx = W / cols;

  const gridLinesX = Math.floor(W / GRID);
  const gridLinesZ = Math.floor(D / GRID);
  const gstep = tier === "high" ? 0.1 : 0.2;
  const gridCount = Math.floor((gridLinesZ + 1) * (W / gstep)) + Math.floor((gridLinesX + 1) * (D / gstep));
  const total = rows * cols + gridCount;
  const pos = new Float32Array(total * 3);
  const data = new Float32Array(total * 4);

  const tileIndex = new Map<string, number>();
  tiles.forEach((t, i) => tileIndex.set(`${Math.floor(t.cx / CELL)},${Math.floor(t.cz / CELL)}`, i));

  const L = new THREE.Vector3(-0.38, 0.78, 0.5).normalize();
  const e = 0.4;
  let n = 0;
  const light = (x: number, z: number, y: number) => {
    const hx = H(x + e, z) - H(x - e, z);
    const hz = H(x, z + e) - H(x, z - e);
    const nx = -hx;
    const ny = 2 * e;
    const nz = -hz;
    const l = Math.hypot(nx, ny, nz);
    const lam = Math.max(0, (nx * L.x + ny * L.y + nz * L.z) / l);
    const crest = clamp01((y - 6) / 14);
    return clamp01(Math.pow(clamp01((lam - 0.62) / 0.38), 1.3) * 0.8 + crest * 0.35);
  };

  for (let j = 0; j < rows; j++) {
    const zRow = BOUNDS.z0 + (j + 0.5) * dz;
    for (let i = 0; i < cols; i++) {
      const x = BOUNDS.x0 + (i + r()) * dx;
      const z = zRow + (r() - 0.5) * dz * 0.25;
      const y = H(x, z);
      pos[n * 3] = x;
      pos[n * 3 + 1] = y;
      pos[n * 3 + 2] = z;
      const ti = tileIndex.get(`${Math.floor(x / CELL)},${Math.floor(z / CELL)}`);
      data[n * 4] = r();
      data[n * 4 + 1] = light(x, z, y);
      data[n * 4 + 2] = 0;
      data[n * 4 + 3] = ti === undefined ? -1 : ti;
      n++;
    }
  }
  const pushGrid = (x: number, z: number) => {
    if (n >= total) return;
    const y = H(x, z) + 0.02;
    pos[n * 3] = x;
    pos[n * 3 + 1] = y;
    pos[n * 3 + 2] = z;
    data[n * 4] = r();
    data[n * 4 + 1] = light(x, z, y);
    data[n * 4 + 2] = 1;
    data[n * 4 + 3] = -1;
    n++;
  };
  for (let k = 0; k <= gridLinesZ; k++) {
    const z = Math.ceil(BOUNDS.z0 / GRID) * GRID + k * GRID;
    for (let x = BOUNDS.x0; x < BOUNDS.x1; x += gstep) pushGrid(x + (r() - 0.5) * gstep, z);
  }
  for (let k = 0; k <= gridLinesX; k++) {
    const x = Math.ceil(BOUNDS.x0 / GRID) * GRID + k * GRID;
    for (let z = BOUNDS.z0; z < BOUNDS.z1; z += gstep) pushGrid(x, z + (r() - 0.5) * gstep);
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute("position", new THREE.BufferAttribute(pos.subarray(0, n * 3), 3));
  g.setAttribute("aData", new THREE.BufferAttribute(data.subarray(0, n * 4), 4));
  g.boundingSphere = new THREE.Sphere(new THREE.Vector3(0, 0, -30), 200);
  return g;
}

type Hop = { a: THREE.Vector3; b: THREE.Vector3; peak: number };

function arcPoint(h: Hop, u: number, out: THREE.Vector3) {
  out.lerpVectors(h.a, h.b, u);
  out.y += 4 * h.peak * u * (1 - u);
  return out;
}

function buildFx(tier: Tier, sites: Site[], hops: Hop[]) {
  const r = rng(23);
  const beamN = tier === "high" ? 900 : 500;
  const blobN = tier === "high" ? 700 : 380;
  const arcN = tier === "high" ? 1800 : 1000;
  const pos: number[] = [];
  const off: number[] = [];
  const info: number[] = [];
  const gauss = () => {
    const u = Math.max(1e-6, r());
    return Math.sqrt(-2 * Math.log(u)) * Math.cos(6.2832 * r());
  };
  sites.forEach((s, si) => {
    const beam = s.h > 0;
    const blobR = s.kind === "miner" ? 0.9 : s.kind === "wallet" ? 1.4 : 0.8;
    if (beam) {
      for (let i = 0; i < beamN; i++) {
        const u = Math.pow(r(), 1.35);
        const rad = 0.12 + 0.18 * (1 - u);
        pos.push(s.x, s.y, s.z);
        off.push(gauss() * rad * 0.6, u, gauss() * rad * 0.6);
        info.push(0, si, r(), 0);
      }
    }
    for (let i = 0; i < blobN; i++) {
      const ox = gauss() * blobR * 0.5;
      const oz = gauss() * blobR * 0.5;
      const y = height(s.x + ox, s.z + oz) - s.y + 0.06 + Math.abs(gauss()) * 0.08;
      pos.push(s.x, s.y, s.z);
      off.push(ox, y, oz);
      info.push(1, si, r(), 0);
    }
  });
  const v = new THREE.Vector3();
  hops.forEach((h, hi) => {
    for (let i = 0; i < arcN; i++) {
      const u = i / (arcN - 1);
      arcPoint(h, u, v);
      pos.push(v.x, v.y, v.z);
      off.push(gauss() * 0.035, gauss() * 0.035, gauss() * 0.035);
      info.push(2, 0, r(), (hi + u) / hops.length);
    }
  });
  const g = new THREE.BufferGeometry();
  g.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3));
  g.setAttribute("aOff", new THREE.Float32BufferAttribute(off, 3));
  g.setAttribute("aInfo", new THREE.Float32BufferAttribute(info, 4));
  g.boundingSphere = new THREE.Sphere(new THREE.Vector3(0, 0, -30), 200);
  return g;
}

/* ------------------------------------------------------------------ */
/* Engine                                                              */
/* ------------------------------------------------------------------ */

export type FilmOptions = {
  canvas: HTMLCanvasElement;
  hud: HTMLElement; // container for projected labels
  counter?: HTMLElement | null;
  onCaption: (i: number) => void;
  onFinal: () => void;
  onFilm: () => void;
};

export type Film = {
  play(): void;
  skip(): void;
  still(): void; // settled frame, no motion
  freeze(t: number): void;
  dispose(): void;
};

type Label = {
  el: HTMLElement;
  pos: (t: number, out: THREE.Vector3) => THREE.Vector3;
  alpha: (t: number) => number;
};

export function createFilm(o: FilmOptions): Film | null {
  let renderer: THREE.WebGLRenderer;
  try {
    renderer = new THREE.WebGLRenderer({ canvas: o.canvas, antialias: false, alpha: false, powerPreference: "high-performance" });
  } catch {
    return null;
  }
  renderer.setClearColor(0x000000, 1);

  const small = Math.min(window.innerWidth, window.innerHeight) < 640;
  const cores = navigator.hardwareConcurrency || 4;
  const tier: Tier = small || cores <= 4 ? "low" : "high";
  const dprCap = tier === "high" ? 1.75 : 1.5;

  const tiles = makeTiles();
  const sites = makeSites();
  const S = (i: number) => new THREE.Vector3(sites[i].x, sites[i].y + 0.15, sites[i].z);
  const hopDefs: [number, number][] = [
    [5, 6],
    [6, 7],
    [7, 8],
  ];
  const hops: Hop[] = hopDefs.map(([a, b]) => {
    const A = S(a);
    const B = S(b);
    return { a: A, b: B, peak: A.distanceTo(B) * 0.42 };
  });

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(42, 1, 0.5, 400);

  const tileC = tiles.map((t) => new THREE.Vector4(t.cx, t.cz, t.delay, t.pattern));
  const tileO = tiles.map((t) => new THREE.Vector4(t.ox, t.oy, t.oz, t.angle));
  while (tileC.length < MAX_TILES) {
    tileC.push(new THREE.Vector4());
    tileO.push(new THREE.Vector4());
  }

  const tu = {
    uTime: { value: 0 },
    uSpark: { value: 0 },
    uTiles: { value: 0 },
    uMerge: { value: 0 },
    uGrid: { value: 0 },
    uSize: { value: tier === "high" ? 1.1 : 1.05 },
    uDpr: { value: 1 },
    uFade: { value: 1 },
    uShimmer: { value: 0 },
    uRef: { value: 140 },
    uTileC: { value: tileC },
    uTileO: { value: tileO },
  };
  const terrainMat = new THREE.ShaderMaterial({
    uniforms: tu,
    vertexShader: terrainVert,
    fragmentShader: pointFrag,
    transparent: true,
    depthTest: false,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  const terrainGeo = buildTerrain(tier, tiles);
  const terrain = new THREE.Points(terrainGeo, terrainMat);
  terrain.frustumCulled = false;
  scene.add(terrain);

  const siteU = sites.map(() => new THREE.Vector4());
  while (siteU.length < 12) siteU.push(new THREE.Vector4());
  const fu = {
    uTime: tu.uTime,
    uDpr: tu.uDpr,
    uFade: tu.uFade,
    uSize: { value: tier === "high" ? 1.6 : 1.8 },
    uArc: { value: 0 },
    uArcA: { value: 0 },
    uRef: tu.uRef,
    uSite: { value: siteU },
  };
  const fxMat = new THREE.ShaderMaterial({
    uniforms: fu,
    vertexShader: fxVert,
    fragmentShader: pointFrag,
    transparent: true,
    depthTest: false,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  const fxGeo = buildFx(tier, sites, hops);
  const fx = new THREE.Points(fxGeo, fxMat);
  fx.frustumCulled = false;
  scene.add(fx);

  /* ---------------- HUD labels ---------------- */
  const labels: Label[] = [];
  const mk = (cls: string, html: string) => {
    const el = document.createElement("div");
    el.className = `hud ${cls}`;
    el.innerHTML = html;
    el.setAttribute("aria-hidden", "true");
    o.hud.appendChild(el);
    return el;
  };
  const tilePose = (i: number, t: number, merge: number, out: THREE.Vector3) => {
    const tl = tiles[i];
    const mi = smooth(0, 1, (merge - tl.delay) / 0.46);
    const bob = Math.sin(t * 0.55 + i * 1.9) * 0.3;
    const cy = height(tl.cx, tl.cz);
    out.set(tl.cx + tl.ox * (1 - mi), cy * 0.22 + tl.oy + bob, tl.cz + tl.oz * (1 - mi));
    out.y = out.y * (1 - mi) + cy * mi;
    return out;
  };
  // Pick labelled tiles: the ones nearest the frame centre in the tile shot.
  const order = tiles
    .map((t, i) => ({ i, d: Math.hypot(t.cx - 2, (t.cz + 22) * 1.3) }))
    .sort((a, b) => a.d - b.d)
    .slice(0, TILE_LABELS.length);
  order.forEach(({ i }, k) => {
    const [a, b] = TILE_LABELS[k];
    const half = CELL / 2;
    labels.push({
      el: mk("hud-tile", `<span>${a}</span><span class="hud-sub">${b}</span>`),
      pos: (t, out) => {
        tilePose(i, t, mergeAt(t), out);
        out.x -= half * 0.92;
        out.z -= half * 0.92;
        return out;
      },
      alpha: (t) => smooth(T.labelsIn + k * 0.16, T.labelsIn + k * 0.16 + 0.5, t) * (1 - smooth(T.labelsOut[0], T.labelsOut[1], t)),
    });
  });
  const hudA = (t0: number) => (t: number) => smooth(t0, t0 + 0.45, t) * (1 - smooth(T.hudOut[0], T.hudOut[1], t));
  const tallest = 1;
  labels.push({
    el: mk("hud-site", `<span>MINER</span><span class="hud-sub">SHA3-256 · NEW BLOCK</span>`),
    pos: (_t, out) => out.set(sites[tallest].x + 0.6, sites[tallest].y + sites[tallest].h * 0.98, sites[tallest].z),
    alpha: hudA(T.miners[tallest] + 0.4),
  });
  const bracket = `<svg class="hud-br" viewBox="0 0 40 40" aria-hidden="true"><path d="M1 11V1h10M29 1h10v10M39 29v10H29M11 39H1V29" fill="none" stroke="currentColor" stroke-width="1" vector-effect="non-scaling-stroke"/></svg>`;
  ([
    [5, "WALLET A", "KEYS ON DEVICE"],
    [8, "WALLET B", "gath…"],
  ] as const).forEach(([si, a, b]) => {
    labels.push({
      el: mk("hud-bracket", `${bracket}<div class="hud-cap"><span>${a}</span><span class="hud-sub">${b}</span></div>`),
      pos: (_t, out) => out.set(sites[si].x, sites[si].y + 0.2, sites[si].z),
      alpha: hudA(sites[si].t0),
    });
  });
  [6, 7].forEach((si) => {
    labels.push({
      el: mk("hud-site hud-node", `<span>NODE</span><span class="hud-sub">VERIFIES</span>`),
      pos: (_t, out) => out.set(sites[si].x + 0.5, sites[si].y + sites[si].h + 0.4, sites[si].z),
      alpha: hudA(sites[si].t0 + 0.2),
    });
  });
  labels.push({
    el: mk("hud-site", `<span>TX · ML-DSA-44</span><span class="hud-sub">SIGNED · FEE ≥ 1 ATOM/BYTE</span>`),
    pos: (_t, out) => arcPoint(hops[0], 0.22, out).add(new THREE.Vector3(-0.6, 0.4, 0)),
    alpha: hudA(T.arc[0] + 0.5),
  });

  /* ---------------- camera ---------------- */
  // Each key: target, distance, elevation (deg), azimuth (deg), vertical fov.
  type Key = { tgt: THREE.Vector3; d: number; el: number; az: number; fov: number };
  const key = (x: number, y: number, z: number, d: number, el: number, az: number, fov: number): Key => ({ tgt: new THREE.Vector3(x, y, z), d, el, az, fov });
  let aspect = 1;
  let K = { hi: key(0, 0, -22, 85, 62, 5, 40), mid: key(9, -1, -28, 150, 30, -4, 24), end: key(11, 0, -30, 140, 24, 0, 22) };
  const layoutCamera = () => {
    if (aspect < 0.85) K = { hi: key(4, 0, -24, 120, 66, 5, 48), mid: key(18, -1, -28, 185, 30, -4, 36), end: key(18.5, 0, -29, 168, 26, 0, 34) };
    else if (aspect < 1.3) K = { hi: key(2, 0, -22, 100, 63, 5, 44), mid: key(12, -1, -28, 150, 30, -4, 28), end: key(14, 0, -30, 135, 24, 0, 27) };
    else K = { hi: key(0, 0, -22, 85, 62, 5, 40), mid: key(9, -1, -28, 150, 30, -4, 24), end: key(11, 0, -30, 140, 24, 0, 22) };
  };
  const lerpKey = (a: Key, b: Key, k: number, out: Key) => {
    out.tgt.lerpVectors(a.tgt, b.tgt, k);
    out.d = a.d + (b.d - a.d) * k;
    out.el = a.el + (b.el - a.el) * k;
    out.az = a.az + (b.az - a.az) * k;
    out.fov = a.fov + (b.fov - a.fov) * k;
    return out;
  };
  const kA = key(0, 0, 0, 1, 0, 0, 40);
  const kB = key(0, 0, 0, 1, 0, 0, 40);
  let camDist = 140;
  const placeCamera = (t: number) => {
    const d = smoother(T.camDown[0], T.camDown[1], t);
    const k = smoother(T.camPush[0], T.camPush[1], t);
    const push = (1 - d) * Math.min(t, T.camDown[1]) * 2.2; // slow push over the tiles
    lerpKey(K.mid, K.end, k, kB);
    lerpKey(K.hi, kB, d, kA);
    kA.d -= push;
    const el = (kA.el * Math.PI) / 180;
    const az = (kA.az * Math.PI) / 180;
    camera.position.set(
      kA.tgt.x + kA.d * Math.sin(az) * Math.cos(el),
      kA.tgt.y + kA.d * Math.sin(el),
      kA.tgt.z + kA.d * Math.cos(az) * Math.cos(el),
    );
    camera.lookAt(kA.tgt);
    if (Math.abs(camera.fov - kA.fov) > 1e-4) {
      camera.fov = kA.fov;
      camera.updateProjectionMatrix();
    }
    camDist = kA.d;
  };

  function mergeAt(t: number) {
    return smooth(0, 1, (t - T.merge[0]) / (T.merge[1] - T.merge[0]));
  }

  /* ---------------- frame ---------------- */
  let W = 0;
  let Hh = 0;
  const resize = () => {
    const w = o.canvas.clientWidth || window.innerWidth;
    const h = o.canvas.clientHeight || window.innerHeight;
    if (w === W && h === Hh) return;
    W = w;
    Hh = h;
    const dpr = Math.min(window.devicePixelRatio || 1, dprCap);
    renderer.setPixelRatio(dpr);
    renderer.setSize(w, h, false);
    tu.uDpr.value = dpr;
    aspect = w / h;
    camera.aspect = aspect;
    layoutCamera();
    camera.updateProjectionMatrix();
  };

  let lastCaption = -2;
  let lastCount = -1;
  let finalSent = false;
  const v = new THREE.Vector3();

  const draw = (t: number, clock: number, shimmer: boolean) => {
    resize();
    tu.uTime.value = clock;
    tu.uSpark.value = smooth(0.05, 0.9, t);
    tu.uTiles.value = smooth(T.tilesIn[0], T.tilesIn[1], t);
    tu.uMerge.value = mergeAt(t);
    tu.uGrid.value = smooth(T.merge[1] - 1.4, T.merge[1] + 0.4, t);
    tu.uShimmer.value = shimmer ? 0.1 : 0;
    
    tu.uFade.value = 1;

    sites.forEach((s, i) => {
      const rev = smooth(s.t0, s.t0 + (s.kind === "miner" ? 0.9 : 0.6), t);
      const settle = 1 - 0.45 * smooth(T.hudOut[0], T.hudOut[1] + 0.3, t);
      siteU[i].set(rev, s.h, (s.kind === "miner" ? 1 : 0.85) * settle, 0);
    });
    fu.uArc.value = smooth(T.arc[0], T.arc[1], t) * 1.02;
    fu.uArcA.value = smooth(T.arc[0], T.arc[0] + 0.2, t) * (1 - 0.82 * smooth(T.hudOut[0], T.hudOut[1] + 0.3, t));

    placeCamera(t);
    tu.uRef.value = camDist;
    camera.updateMatrixWorld();
    renderer.render(scene, camera);

    // Labels
    for (const l of labels) {
      const a = l.alpha(t);
      if (a <= 0.001) {
        if (l.el.style.opacity !== "0") l.el.style.opacity = "0";
        continue;
      }
      l.pos(t, v).project(camera);
      if (v.z > 1) {
        l.el.style.opacity = "0";
        continue;
      }
      const x = (v.x * 0.5 + 0.5) * W;
      const y = (-v.y * 0.5 + 0.5) * Hh;
      l.el.style.transform = `translate3d(${x.toFixed(1)}px, ${y.toFixed(1)}px, 0)`;
      const flip = x > W - 150;
      if (flip !== (l.el.dataset.flip === "1")) l.el.dataset.flip = flip ? "1" : "0";
      l.el.style.opacity = a.toFixed(3);
    }

    // Counter: blocks found in the film, one step per miner beacon.
    if (o.counter) {
      let c = 0;
      for (const m of T.miners) if (t >= m + 0.25) c++;
      const show = t >= T.miners[0] - 0.2 && t < T.hudOut[1];
      if (c !== lastCount) {
        lastCount = c;
        const num = o.counter.querySelector("[data-n]");
        if (num) num.textContent = String(c);
      }
      o.counter.style.opacity = show ? "1" : "0";
    }

    const ci = captionAt(t);
    if (ci !== lastCaption) {
      lastCaption = ci;
      o.onCaption(ci);
    }
    if (t >= T.finalUI && !finalSent) {
      finalSent = true;
      o.onFinal();
    }
  };

  /* ---------------- loop ---------------- */
  let raf = 0;
  let start = 0;
  let running = false;
  let hiddenAt = 0;
  let lastFrame = 0;
  let mode: "play" | "still" | "freeze" = "play";

  const tick = (now: number) => {
    raf = requestAnimationFrame(tick);
    const t = (now - start) / 1000;
    const film = Math.min(t, T.end);
    // After the film settles, keep only the shimmer at ~30 fps.
    if (t > T.end + 0.5 && now - lastFrame < 32) return;
    lastFrame = now;
    draw(film, t, true);
  };
  const run = () => {
    cancelAnimationFrame(raf);
    running = true;
    raf = requestAnimationFrame(tick);
  };
  const stop = () => {
    cancelAnimationFrame(raf);
    running = false;
  };

  const onVis = () => {
    if (mode !== "play") return;
    if (document.hidden) {
      hiddenAt = performance.now();
      stop();
    } else if (!running) {
      start += performance.now() - hiddenAt;
      run();
    }
  };
  document.addEventListener("visibilitychange", onVis);
  const onResize = () => {
    if (mode !== "play") {
      W = 0;
      if (mode === "still") draw(T.end, 0, false);
      else draw(frozenT, frozenT, false);
    }
  };
  window.addEventListener("resize", onResize);
  let frozenT = 0;

  return {
    play() {
      mode = "play";
      finalSent = false;
      lastCaption = -2;
      o.onFilm();
      start = performance.now();
      run();
    },
    skip() {
      mode = "play";
      start = performance.now() - T.end * 1000;
      if (!finalSent) {
        finalSent = true;
        o.onFinal();
      }
      if (!running) run();
    },
    still() {
      mode = "still";
      stop();
      finalSent = false;
      draw(T.end, 0, false);
    },
    freeze(t: number) {
      mode = "freeze";
      stop();
      frozenT = t;
      finalSent = false;
      lastCaption = -2;
      draw(t, t, false);
    },
    dispose() {
      stop();
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("resize", onResize);
      terrainGeo.dispose();
      fxGeo.dispose();
      terrainMat.dispose();
      fxMat.dispose();
      renderer.dispose();
      o.hud.replaceChildren();
    },
  };
}

