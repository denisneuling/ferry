import { LOCALE_ID } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Api, Entry } from './api';
import { Browser, MEMORY_FOLDERS, MEMORY_LARGEST } from './browser';
import { Session } from './session';

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
  version: `${size}-0`,
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

describe('Browser loading a folder', () => {
  const page = (names: string[]) => names.map((n) => entry(n, 'file'));
  let batches: Entry[][];
  let release: () => void;
  let b: Browser;
  beforeEach(() => {
    batches = [];
    const api = {
      // A listing in two batches, the second held back until release().
      entriesStream: (_s: string, _p: string, on: (b: Entry[]) => void) => {
        on(batches[0]);
        return new Promise<number>((resolve) => {
          release = () => {
            on(batches[1]);
            resolve(batches[0].length + batches[1].length);
          };
        });
      },
    };
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        { provide: Api, useValue: api },
      ],
    });
    b = TestBed.inject(Browser);
  });

  it("shows a new folder's first entries while the rest arrive", async () => {
    batches = [page(['a', 'b']), page(['c'])];
    const done = b.open('alex', 'Many');
    await Promise.resolve();
    expect(b.entries()?.map((e) => e.name)).toEqual(['a', 'b']);
    expect(b.loading()).toBe(true);
    release();
    await done;
    expect(b.entries()?.map((e) => e.name)).toEqual(['a', 'b', 'c']);
    expect(b.loading()).toBe(false);
  });

  it('takes a name once, should a server list it twice', async () => {
    batches = [page(['a', 'b']), page(['b', 'c'])];
    const done = b.open('alex', 'Changing');
    release();
    await done;
    expect(b.entries()?.map((e) => e.name)).toEqual(['a', 'b', 'c']);
  });

  it('replaces a folder already shown only when its new listing is complete', async () => {
    batches = [page(['a']), page(['b'])];
    const first = b.open('alex', 'Many');
    release();
    await first;
    batches = [page(['x']), page(['y'])];
    const again = b.reload();
    await Promise.resolve();
    expect(b.entries()?.map((e) => e.name)).toEqual(['a', 'b']);
    release();
    await again;
    expect(b.entries()?.map((e) => e.name)).toEqual(['x', 'y']);
  });
});

describe('Browser going back', () => {
  /** What each folder lists now; a folder named 'hold' waits for release(). */
  let listings: Record<string, Entry[] | Error>;
  let held: (() => void) | null;
  let b: Browser;
  let session: Session;
  beforeEach(() => {
    listings = {};
    held = null;
    const api = {
      entriesStream: (_s: string, path: string, on: (b: Entry[]) => void) => {
        const answer = () => {
          const l = listings[path] ?? [];
          if (l instanceof Error) throw l;
          on(l);
          return l.length;
        };
        if (path !== 'hold') return Promise.resolve().then(answer);
        return new Promise<number>((resolve, reject) => {
          held = () => {
            try {
              resolve(answer());
            } catch (e) {
              reject(e);
            }
          };
        });
      },
    };
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        { provide: Api, useValue: api },
      ],
    });
    session = TestBed.inject(Session);
    session.started({ user: 'alex' } as never);
    b = TestBed.inject(Browser);
    TestBed.tick();
  });

  const names = () => b.entries()?.map((e) => e.name) ?? null;
  const files = (...n: string[]) => n.map((x) => entry(x, 'file'));

  it('shows a folder seen before at once, and its new listing when complete', async () => {
    listings['hold'] = files('old');
    const first = b.open('alex', 'hold');
    held!();
    await first;
    await b.open('alex', 'elsewhere');

    listings['hold'] = files('old', 'new');
    const back = b.open('alex', 'hold');
    expect(names()).toEqual(['old']);
    expect(b.loading()).toBe(true);
    held!();
    await back;
    expect(names()).toEqual(['old', 'new']);
    expect(b.loading()).toBe(false);
  });

  it(`keeps the last ${MEMORY_FOLDERS} folders`, async () => {
    for (let i = 0; i <= MEMORY_FOLDERS; i++) {
      listings[`f${i}`] = files(`in-${i}`);
      await b.open('alex', `f${i}`);
    }
    // The first went out; the second is still there.
    void b.open('alex', 'f0');
    expect(names()).toBeNull();
    void b.open('alex', 'f1');
    expect(names()).toEqual(['in-1']);
  });

  it('does not keep a folder too large to be worth it', async () => {
    listings['big'] = Array.from({ length: MEMORY_LARGEST + 1 }, (_, i) => entry(`f${i}`, 'file'));
    await b.open('alex', 'big');
    await b.open('alex', 'elsewhere');
    void b.open('alex', 'big');
    expect(names()).toBeNull();
  });

  it('forgets a folder whose listing failed', async () => {
    listings['gone'] = files('a');
    await b.open('alex', 'gone');
    listings['gone'] = new Error('not found');
    await b.reload();
    await b.open('alex', 'elsewhere');
    void b.open('alex', 'gone');
    expect(names()).toBeNull();
  });

  it('forgets everything when the session ends, whoever logs in next', async () => {
    listings['Photos'] = files('IMG_1.jpg');
    await b.open('alex', 'Photos');
    await b.open('alex', 'elsewhere');
    session.ended('expired');
    TestBed.tick();
    session.started({ user: 'sam' } as never);
    void b.open('alex', 'Photos');
    expect(names()).toBeNull();
  });
});
