import { TestBed } from '@angular/core/testing';
import { Entry } from './api';
import { Preloader, neighbours } from './preloader';

const file = (name: string): Entry => ({
  name,
  path: name,
  type: 'file',
  size: 1,
  modified: '2026-01-01T00:00:00Z',
  version: '1-0',
});

describe('neighbours', () => {
  const names = (files: Entry[], i: number) => neighbours(files, i).map((f) => f.name);

  it('are the next and the previous image, round the end', () => {
    const files = ['a.jpg', 'b.jpg', 'c.jpg', 'd.jpg'].map(file);
    expect(names(files, 1)).toEqual(['c.jpg', 'a.jpg']);
    expect(names(files, 3)).toEqual(['a.jpg', 'c.jpg']);
    expect(names(files, 0)).toEqual(['b.jpg', 'd.jpg']);
  });

  it('leave out videos, PDFs and the rest', () => {
    const files = ['a.mp4', 'b.jpg', 'c.pdf'].map(file);
    expect(names(files, 1)).toEqual([]);
    expect(names(['a.png', 'b.txt', 'c.webp'].map(file), 1)).toEqual(['c.webp', 'a.png']);
  });

  it('are none or one in a small folder', () => {
    expect(names([file('a.jpg')], 0)).toEqual([]);
    expect(names([file('a.jpg'), file('b.jpg')], 0)).toEqual(['b.jpg']);
  });
});

describe('Preloader', () => {
  let p: Preloader;
  beforeEach(() => {
    p = TestBed.inject(Preloader);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('holds what it was last asked for, and lets go of the rest', () => {
    p.want(['/a', '/b']);
    expect(p.holding()).toEqual(['/a', '/b']);
    p.want(['/b', '/c']);
    expect(p.holding()).toEqual(['/b', '/c']);
    p.want([]);
    expect(p.holding()).toEqual([]);
  });

  it('loads nothing ahead when the person saves data', () => {
    vi.stubGlobal('navigator', { connection: { saveData: true } });
    p.want(['/a']);
    expect(p.holding()).toEqual([]);
  });
});
