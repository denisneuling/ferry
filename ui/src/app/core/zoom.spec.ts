import { FIT, clampView, maxScale, zoomAt } from './zoom';

describe('zoom', () => {
  it('keeps the point under the pointer where it is', () => {
    const v = zoomAt(FIT, 2, 100, -50);
    // The picture point under (100, -50) was (100, -50) at scale 1; at
    // scale 2 it is drawn at v + 2·p, which must be (100, -50) again.
    expect(v.x + 2 * 100).toBe(100);
    expect(v.y + 2 * -50).toBe(-50);
    const back = zoomAt(v, 1, 100, -50);
    expect(back).toEqual({ s: 1, x: 0, y: 0 });
  });

  it('leaves no gap at an edge, and centres what is smaller than the stage', () => {
    // 800×400 fitted, scale 2 → 1600×800 on a 1000×1000 stage.
    expect(clampView({ s: 2, x: 900, y: 50 }, 800, 400, 1000, 1000)).toEqual({ s: 2, x: 300, y: 0 });
    expect(clampView({ s: 2, x: -900, y: -50 }, 800, 400, 1000, 1000)).toEqual({ s: 2, x: -300, y: 0 });
    expect(clampView({ s: 1, x: 10, y: 10 }, 800, 400, 1000, 1000)).toEqual({ s: 1, x: 0, y: 0 });
  });

  it('bounds the scale', () => {
    expect(maxScale(4000, 1000)).toBe(8);
    expect(maxScale(500, 1000)).toBe(4);
    expect(maxScale(100000, 1000)).toBe(16);
    expect(maxScale(100, 0)).toBe(4);
  });
});
