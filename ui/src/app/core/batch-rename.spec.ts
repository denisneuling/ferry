import { Entry } from './api';
import { applyPattern, planRename, steps } from './batch-rename';

const f = (name: string, modified = '2026-07-14T10:00:00Z', type: 'file' | 'folder' = 'file'): Entry => ({
  name,
  path: 'Fotos/' + name,
  type,
  size: 1,
  modified,
  version: '1-0',
});
const opts = { start: 1, digits: 3 };

describe('batch rename', () => {
  it('fills in the pattern', () => {
    const e = f('IMG_0042.JPG');
    expect(applyPattern('Urlaub {n} {name}.{ext}', e, 0, opts)).toBe('Urlaub 001 IMG_0042.JPG');
    expect(applyPattern('{n:1}-{name}', e, 9, opts)).toBe('10-IMG_0042');
    expect(applyPattern('{taken} {name}.{ext}', e, 0, opts, '2025-06-01T23:30:00')).toBe(
      '2025-06-01 IMG_0042.JPG',
    );
    expect(applyPattern('{name}{ext}', f('Ordner', undefined, 'folder'), 0, opts)).toBe('Ordner');
    expect(applyPattern('{unknown} {name}', e, 0, opts)).toBe('{unknown} IMG_0042');
  });

  it('refuses the whole rename for one name that cannot be', () => {
    const folder = [f('a.jpg'), f('b.jpg'), f('Urlaub 001.jpg')];
    let p = planRename([folder[0], folder[1]], folder, 'Urlaub {n}.{ext}', opts);
    expect(p.rows.map((r) => r.problem)).toEqual(['taken', null]);
    expect(p.ok).toBe(false);
    // Two of them to the same name, case aside.
    p = planRename([folder[0], folder[1]], folder, 'x.{ext}', opts);
    expect(p.rows.map((r) => r.problem)).toEqual(['twice', 'twice']);
    p = planRename([folder[0]], folder, 'a/b', opts);
    expect(p.rows[0].problem).toBe('invalid');
    // Nothing changes: nothing to do.
    expect(planRename([folder[0]], folder, '{name}.{ext}', opts).ok).toBe(false);
  });

  it('swaps names through temporary ones, and renames straight otherwise', () => {
    const folder = [f('1.jpg'), f('2.jpg')];
    // 1 → 2 and 2 → 1: start 2, counting down is not possible, so by name.
    const swap = planRename(folder, folder, '{n:1}.jpg', { start: 2, digits: 1 });
    expect(swap.rows.map((r) => r.to)).toEqual(['2.jpg', '3.jpg']);
    expect(swap.ok).toBe(true);
    expect(swap.swaps).toBe(true);
    const s = steps(swap, (i) => `.ferry-tmp-x-${i}`);
    expect(s.map((x) => `${x.from}>${x.to}`)).toEqual([
      '1.jpg>.ferry-tmp-x-0',
      '2.jpg>.ferry-tmp-x-1',
      '.ferry-tmp-x-0>2.jpg',
      '.ferry-tmp-x-1>3.jpg',
    ]);
    // A change of case is no swap.
    const upper = planRename([f('a.jpg')], [f('a.jpg')], '{name}.JPG', opts);
    expect(upper.ok).toBe(true);
    expect(upper.swaps).toBe(false);
    expect(steps(upper, () => 'never')).toEqual([
      { from: 'a.jpg', to: 'a.JPG', temp: false, original: 'a.jpg' },
    ]);
  });
});
