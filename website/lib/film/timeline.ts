/** Film timing in seconds. Shared by the WebGL engine and the React overlay. */
export const T = {
  tilesIn: [0.6, 3.4] as const,
  labelsIn: 1.3,
  labelsOut: [3.8, 4.5] as const,
  merge: [3.7, 6.9] as const,
  camDown: [3.5, 7.6] as const,
  camPush: [6.4, 11.6] as const,
  miners: [6.7, 7.0, 7.3, 7.65, 8.0],
  sitesIn: [8.3, 8.9] as const,
  arc: [8.8, 10.4] as const,
  hudOut: [10.7, 11.3] as const,
  finalUI: 11.1,
  end: 11.8,
};

/** Short, literal captions. [start, end, text] */
export const CAPTIONS: [number, number, string][] = [
  [0.7, 3.7, "Genesis pays nothing. Every rule is public."],
  [3.9, 6.6, "One chain. The heaviest cumulative work wins."],
  [6.7, 8.4, "Miners search SHA3-256 for a block about every 45 seconds."],
  [8.6, 10.8, "A signed transfer, verified by every node."],
];

export function captionAt(t: number): number {
  for (let i = 0; i < CAPTIONS.length; i++) if (t >= CAPTIONS[i][0] && t < CAPTIONS[i][1]) return i;
  return -1;
}

export const clamp01 = (x: number) => (x < 0 ? 0 : x > 1 ? 1 : x);
export const smooth = (a: number, b: number, x: number) => {
  const t = clamp01((x - a) / (b - a));
  return t * t * (3 - 2 * t);
};
export const smoother = (a: number, b: number, x: number) => {
  const t = clamp01((x - a) / (b - a));
  return t * t * t * (t * (t * 6 - 15) + 10);
};
