import { LOCALE_ID, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Actions } from './actions';
import { Api, Entry } from './api';
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
});
const files = (n: number) => Array.from({ length: n }, (_, i) => file(i));

describe('Actions on many entries', () => {
  let actions: Actions;
  let errors: string[];
  let asked: { title: string; text?: string }[];
  let answer: boolean;
  let removed: string[];
  beforeEach(() => {
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
          useValue: { info: signal({ limits: { listing: 200000, delete: 10000, upload: 0 } }) },
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
          useValue: { error: (m: string) => errors.push(m), show: () => undefined },
        },
        { provide: FolderPicker, useValue: {} },
      ],
    });
    actions = TestBed.inject(Actions);
  });
  afterEach(() => vi.useRealTimers());

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

  it('asks before downloading many files, and refuses too many', async () => {
    await actions.download(files(20));
    expect(asked).toEqual([]);
    await actions.download(files(150));
    expect(asked[0].title).toBe('Download 150 files?');
    expect(asked[0].text).toContain('1 min');
    await actions.download(files(60000));
    expect(asked.length).toBe(1);
    expect(errors[0]).toContain('60,000');
  });
});
