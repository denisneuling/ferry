import { TestBed } from '@angular/core/testing';
import { Api, ApiError } from './api';
import { Browser } from './browser';
import { Transfers } from './transfers';
import { Toasts } from '../ui/toast';

describe('Transfers.addTree', () => {
  let made: string[];
  let errors: string[];
  let t: Transfers;
  beforeEach(() => {
    made = [];
    errors = [];
    TestBed.configureTestingModule({
      providers: [
        {
          provide: Api,
          useValue: {
            mkdir: (_: string, p: string) => {
              made.push(p);
              return p === 'up/Trip/Day 1'
                ? Promise.reject(new ApiError(409, 'exists'))
                : Promise.resolve({});
            },
            // Never finishes: the queue is what is looked at.
            upload: () => ({ done: new Promise(() => undefined), abort: () => undefined }),
          },
        },
        {
          provide: Browser,
          useValue: { share: () => 'alex', path: () => 'up', title: () => 'up', reload: () => undefined },
        },
        { provide: Toasts, useValue: { error: (s: string) => errors.push(s), show: () => undefined } },
      ],
    });
    t = TestBed.inject(Transfers);
  });

  const file = (name: string) => new File(['x'], name);

  it('makes the folders, parents first and one there already taken as it is, then queues the files into them', async () => {
    await t.addTree('alex', 'up', [
      { rel: 'Trip/Day 2/b.jpg', file: file('b.jpg') },
      { rel: 'Trip/Day 1/a.jpg', file: file('a.jpg') },
      { rel: 'Trip/notes.txt', file: file('notes.txt') },
    ]);
    expect(made).toEqual(['up/Trip', 'up/Trip/Day 1', 'up/Trip/Day 2']);
    expect(errors).toEqual([]);
    expect(
      t
        .jobs()
        .map((j) => `${j.dir}/${j.name}`)
        .sort(),
    ).toEqual(['up/Trip/Day 1/a.jpg', 'up/Trip/Day 2/b.jpg', 'up/Trip/notes.txt']);
  });

  it('refuses more files than it takes, and makes nothing', async () => {
    await t.addTree(
      'alex',
      'up',
      [1, 2, 3].map((i) => ({ rel: `F/${i}`, file: file(String(i)) })),
      2,
    );
    expect(made).toEqual([]);
    expect(t.jobs()).toEqual([]);
    expect(errors.length).toBe(1);
  });
});
