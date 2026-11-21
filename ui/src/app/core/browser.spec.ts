import { LOCALE_ID } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Api, Entry } from './api';
import { Browser } from './browser';

const entry = (
  name: string,
  type: 'file' | 'folder',
  size = 0,
  modified = '2026-01-01T00:00:00Z',
): Entry => ({
  name,
  path: name,
  type,
  size,
  modified,
});

describe('Browser', () => {
  let b: Browser;
  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        { provide: Api, useValue: {} },
      ],
    });
    b = TestBed.inject(Browser);
    b.entries.set([
      entry('b.txt', 'file', 30, '2026-03-01T00:00:00Z'),
      entry('Zeta', 'folder'),
      entry('a.txt', 'file', 10, '2026-01-01T00:00:00Z'),
      entry('alpha', 'folder'),
      entry('C.jpg', 'file', 20, '2026-02-01T00:00:00Z'),
    ]);
  });

  const names = () => b.visible().map((e) => e.name);

  it('lists folders first, then files, by name', () => {
    expect(names()).toEqual(['alpha', 'Zeta', 'a.txt', 'b.txt', 'C.jpg']);
  });

  it('sorts by size and date, newest and largest first, folders still first', () => {
    b.setSort('size');
    expect(names()).toEqual(['alpha', 'Zeta', 'b.txt', 'C.jpg', 'a.txt']);
    b.setSort('size');
    expect(names()).toEqual(['alpha', 'Zeta', 'a.txt', 'C.jpg', 'b.txt']);
    b.setSort('modified');
    expect(names().slice(2)).toEqual(['b.txt', 'C.jpg', 'a.txt']);
  });

  it('filters by name, case aside', () => {
    b.filter.set('A');
    expect(names()).toEqual(['alpha', 'Zeta', 'a.txt']);
  });

  it('selects one, a range, and all', () => {
    b.toggle('alpha');
    b.toggle('b.txt', true);
    expect([...b.selected()]).toEqual(['alpha', 'Zeta', 'a.txt', 'b.txt']);
    b.toggle('Zeta');
    expect(b.selected().has('Zeta')).toBe(false);
    b.selectAll();
    expect(b.selected().size).toBe(5);
    b.clear();
    expect(b.selected().size).toBe(0);
  });
});
