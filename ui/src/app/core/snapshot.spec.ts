import { TestBed } from '@angular/core/testing';
import { Api } from './api';
import { Session } from './session';
import { Snapshot, isToken, tokenTime } from './snapshot';

describe('snapshot tokens', () => {
  it('are read exactly as the server writes them, in UTC', () => {
    expect(isToken('@GMT-2026.11.23-15.00.09')).toBe(true);
    expect(tokenTime('@GMT-2026.11.23-15.00.09')?.toISOString()).toBe('2026-11-23T15:00:09.000Z');
    for (const bad of [
      '',
      null,
      undefined,
      '@GMT-2026.11.23-15.00.9',
      '2026-11-23T15:00:09Z',
      '@GMT-2026.11.23-15.00.09/x',
    ]) {
      expect(isToken(bad)).toBe(false);
    }
  });
});

describe('Api while a snapshot is browsed', () => {
  let api: Api;
  let snapshot: Snapshot;
  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { ended: () => undefined } }],
    });
    api = TestBed.inject(Api);
    snapshot = TestBed.inject(Snapshot);
  });

  it('reads at the snapshot, and at a given one over it', () => {
    const at = '@GMT-2026.11.23-15.00.09';
    expect(api.contentUrl('alex', 'a.txt')).not.toContain('at=');
    snapshot.at.set(at);
    expect(api.contentUrl('alex', 'a.txt')).toContain('&at=%40GMT-2026.11.23-15.00.09');
    expect(api.thumbnailUrl('alex', 'a.jpg', 320, '1-0')).toContain('&at=%40GMT-2026.11.23-15.00.09');
    // A version's own time wins; null asks for the file as it is now.
    expect(api.contentUrl('alex', 'a.txt', false, '@GMT-2026.11.22-00.00.01')).toContain(
      'at=%40GMT-2026.11.22-00.00.01',
    );
    expect(api.contentUrl('alex', 'a.txt', false, null)).not.toContain('at=');
  });
});
