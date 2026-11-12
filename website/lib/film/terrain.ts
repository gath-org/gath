/**
 * Deterministic terrain for the Gath film. Pure functions, no randomness at runtime:
 * the same seed always gives the same landscape.
 */

/** Small seeded PRNG (mulberry32). */
export function rng(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function hash2(ix: number, iz: number): number {
  let h = Math.imul(ix, 374761393) + Math.imul(iz, 668265263);
  h = Math.imul(h ^ (h >>> 13), 1274126177);
  return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
}

/** Smooth value noise in [-1, 1]. */
function vnoise(x: number, z: number): number {
  const ix = Math.floor(x);
  const iz = Math.floor(z);
  const fx = x - ix;
  const fz = z - iz;
  const ux = fx * fx * fx * (fx * (fx * 6 - 15) + 10);
  const uz = fz * fz * fz * (fz * (fz * 6 - 15) + 10);
  const a = hash2(ix, iz);
  const b = hash2(ix + 1, iz);
  const c = hash2(ix, iz + 1);
  const d = hash2(ix + 1, iz + 1);
  return (a + (b - a) * ux + (c - a) * uz + (a - b - c + d) * ux * uz) * 2 - 1;
}

function fbm(x: number, z: number, oct = 4): number {
  let s = 0;
  let amp = 0.5;
  let f = 1;
  for (let i = 0; i < oct; i++) {
    s += amp * vnoise(x * f, z * f);
    f *= 2.03;
    amp *= 0.5;
  }
  return s;
}

/** Terrain bounds in world units (x across, z away from the camera is negative). */
export const BOUNDS = { x0: -78, x1: 78, z0: -92, z1: 50 };

/** Centre line of the main ridge at a given x. */
export function ridgeZ(x: number): number {
  return -40 + 6 * Math.sin(x * 0.043 + 0.6) + 3.5 * Math.sin(x * 0.11 + 1.7);
}

/** Height of the surface at (x, z). */
export function height(x: number, z: number): number {
  const zr = ridgeZ(x);
  const d = (z - zr) / 14;
  const ridge = Math.exp(-d * d);
  // Ridge crest, broken up so it reads as a range rather than a wall.
  let h = 12.5 * ridge * (0.72 + 0.45 * fbm(x * 0.028 + 3.1, z * 0.028 - 1.3, 3));
  // Back slope stays high and rolls away toward the horizon.
  if (z < zr) h += 2.5 * (1 - Math.exp(-((zr - z) / 22))) * (0.6 + 0.4 * fbm(x * 0.02, z * 0.02 + 7, 2));
  // Rolling ground everywhere.
  h += 3.4 * fbm(x * 0.055 + 11.7, z * 0.055 - 4.2, 4);
  // Fine relief so contour rows wobble like a scan.
  h += 0.85 * fbm(x * 0.21 - 2.3, z * 0.21 + 9.1, 3);
  // Gentle basin in the foreground.
  const fz = (z - 16) / 26;
  h -= 2.2 * Math.exp(-fz * fz);
  return h;
}

/** Surface normal by central differences. */
export function normal(x: number, z: number, e = 0.35): [number, number, number] {
  const hx = height(x + e, z) - height(x - e, z);
  const hz = height(x, z + e) - height(x, z - e);
  const nx = -hx;
  const ny = 2 * e;
  const nz = -hz;
  const l = Math.hypot(nx, ny, nz);
  return [nx / l, ny / l, nz / l];
}
