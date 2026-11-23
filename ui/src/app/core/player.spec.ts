import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { Entry } from './api';
import { Player } from './player';
import { Session } from './session';

const file = (name: string): Entry => ({
  name,
  path: `Music/${name}`,
  type: 'file',
  size: 1,
  modified: '2026-01-01T00:00:00Z',
  version: '1-0',
});

describe('Player', () => {
  let p: Player;
  let me: ReturnType<typeof signal<object | null>>;
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => Promise.resolve());
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined);
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined);
    me = signal<object | null>({ username: 'alex' });
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { me, ended: () => undefined } }],
    });
    p = TestBed.inject(Player);
  });
  afterEach(() => vi.restoreAllMocks());

  const tracks = ['a.mp3', 'b.flac', 'c.m4a'].map(file);
  const q = { share: 'alex', folder: 'Music', at: null, tracks };

  it('takes only the recordings of a folder', () => {
    expect(
      Player.recordings([file('a.mp3'), file('cover.jpg'), file('notes.txt'), file('b.opus')]).map(
        (e) => e.name,
      ),
    ).toEqual(['a.mp3', 'b.opus']);
  });

  it("plays in the folder's order from the one chosen, and stops at the end", () => {
    p.play(q, tracks[1]);
    expect(p.track()?.name).toBe('b.flac');
    p.next();
    expect(p.track()?.name).toBe('c.m4a');
    p.next();
    expect(p.track()?.name).toBe('c.m4a');
  });

  it('shuffles what comes after the track playing, and keeps that track', () => {
    p.play(q, tracks[0]);
    p.setShuffle(true);
    expect(p.track()?.name).toBe('a.mp3');
    const seen = new Set([p.track()?.name]);
    p.next();
    seen.add(p.track()?.name);
    p.next();
    seen.add(p.track()?.name);
    expect(seen.size).toBe(3);
  });

  it('stops when the session ends', () => {
    p.play(q);
    me.set(null);
    TestBed.tick();
    expect(p.track()).toBeNull();
  });
});
