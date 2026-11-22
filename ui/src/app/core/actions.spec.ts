import { LOCALE_ID, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Actions } from './actions';
import { Api, ApiError, Entry } from './api';
import { Browser } from './browser';
import { Session } from './session';
import { Dialogs } from '../ui/dialog';
import { FolderPicker } from '../ui/folder-picker';
import { Toasts } from '../ui/toast';

const file = (i: number): Entry => ({
  name: `IMG_${i}.jpg`,
  path: `IMG_${i}.jpg`,
  type: 'file',
  size: 1,
  modified: '2026-01-01T00:00:00Z',
  version: '1-0',
});
const files = (n: number) => Array.from({ length: n }, (_, i) => file(i));

describe('Actions on many entries', () => {
  let actions: Actions;
  let errors: string[];
  let asked: { title: string; text?: string }[];
  let answer: boolean;
  let removed: string[];
  let archived: string[][];
  let saved: string[];
  let shown: string[];
  let refuse: unknown;
  beforeEach(() => {
    archived = [];
    saved = [];
    shown = [];
    refuse = null;
    // The links the browser is asked to follow, instead of following them.
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(`${this.getAttribute('href')} as ${this.download}`);
    });
    vi.useFakeTimers();
    errors = [];
    asked = [];
    answer = false;
    removed = [];
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        {
          provide: Session,
          useValue: {
            info: signal({ limits: { listing: 200000, delete: 10000, upload: 0, archive: 10000 } }),
          },
        },
        {
          provide: Browser,
          useValue: {
            share: signal('alex'),
            path: signal(''),
            clear: () => undefined,
            reload: async () => undefined,
          },
        },
        {
          provide: Api,
          useValue: {
            contentUrl: (_s: string, p: string) => `/c/${p}`,
            remove: async (_s: string, p: string) => void removed.push(p),
            archive: async (_s: string, paths: string[]) => {
              if (refuse) throw refuse;
              archived.push(paths);
              return { ticket: 'T1', name: 'alex.zip', entries: 61, bytes: 3_200_000 };
            },
            archiveUrl: (_s: string, t: string) => `/a/${t}`,
          },
        },
        {
          provide: Dialogs,
          useValue: {
            confirm: async (a: { title: string; text?: string }) => {
              asked.push(a);
              return answer;
            },
          },
        },
        {
          provide: Toasts,
          useValue: { error: (m: string) => errors.push(m), show: (m: string) => shown.push(m) },
        },
        { provide: FolderPicker, useValue: {} },
      ],
    });
    actions = TestBed.inject(Actions);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('refuses to delete a selection beyond FERRY_MAX_DELETE, saying both numbers', async () => {
    expect(await actions.remove(files(10001))).toBe(false);
    expect(asked).toEqual([]);
    expect(removed).toEqual([]);
    expect(errors[0]).toContain('10,001');
    expect(errors[0]).toContain('10,000');
  });

  it('names the count when it asks before deleting', async () => {
    answer = true;
    await actions.remove(files(1500));
    expect(asked[0].title).toBe('Delete 1,500 items?');
    expect(removed.length).toBe(1500);
  });

  it('downloads one file as it is', async () => {
    await actions.download([file(7)]);
    expect(saved).toEqual(['/c/IMG_7.jpg as IMG_7.jpg']);
    expect(archived).toEqual([]);
  });

  it('downloads several entries, or a folder, as one ZIP', async () => {
    await actions.download(files(3));
    const folder: Entry = { ...file(0), name: 'Fotos', path: 'Fotos', type: 'folder' };
    await actions.download([folder]);
    expect(archived).toEqual([['IMG_0.jpg', 'IMG_1.jpg', 'IMG_2.jpg'], ['Fotos']]);
    expect(saved).toEqual(['/a/T1 as alex.zip', '/a/T1 as alex.zip']);
    expect(shown[0]).toBe('Downloading alex.zip — 61 items, 3.2 MB');
  });

  it('holds a ZIP to FERRY_MAX_ARCHIVE, and says what the server refused', async () => {
    await actions.download(files(10001));
    expect(archived).toEqual([]);
    expect(errors[0]).toContain('10,001');
    refuse = new ApiError(413, 'too_large', 10000);
    await actions.download(files(2));
    expect(errors[1]).toContain('more than 10,000 items');
    expect(saved).toEqual([]);
  });
});
