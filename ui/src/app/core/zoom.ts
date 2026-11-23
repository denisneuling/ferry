/**
 * The arithmetic of zooming a picture in the preview: a scale over the
 * size it is fitted to, and an offset of its centre from the stage's.
 */
export interface View {
  /** 1 is the picture fitted to the stage. */
  s: number;
  x: number;
  y: number;
}

export const FIT: View = { s: 1, x: 0, y: 0 };

/**
 * The view moved so that the picture never leaves a gap at an edge it could
 * fill: a picture of fitted size w×h, scaled, on a stage W×H. Smaller than
 * the stage along an axis, it stays centred there.
 */
export function clampView(v: View, w: number, h: number, W: number, H: number): View {
  const mx = Math.max(0, (w * v.s - W) / 2);
  const my = Math.max(0, (h * v.s - H) / 2);
  // `|| 0`: a centred axis is 0, not the -0 clamping to ±0 can give.
  const clamp = (a: number, m: number): number => Math.min(Math.max(a, -m), m) || 0;
  return { s: v.s, x: clamp(v.x, mx), y: clamp(v.y, my) };
}

/**
 * The view at scale s, keeping the point (cx, cy) — relative to the stage's
 * centre — where it is on the screen, as a zoom at the pointer does.
 */
export function zoomAt(v: View, s: number, cx: number, cy: number): View {
  const k = s / v.s;
  return { s, x: cx - (cx - v.x) * k, y: cy - (cy - v.y) * k };
}

/**
 * How far a picture may be zoomed: to twice its own pixels, at least four
 * times its fitted size, never more than sixteen.
 */
export function maxScale(natural: number, fitted: number): number {
  const oneToOne = fitted > 0 ? natural / fitted : 1;
  return Math.min(16, Math.max(4, oneToOne * 2));
}
