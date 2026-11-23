import { TestBed } from '@angular/core/testing';
import { Api } from './api';
import { Session } from './session';

const ndjson = (lines: object[]) =>
  new Response(lines.map((l) => JSON.stringify(l) + '\n').join(''), {
    status: 200,
    headers: { 'Content-Type': 'application/x-ndjson' },
  });
const p = (files: number, totalFiles = 3) => ({ files, bytes: files * 10, totalFiles, totalBytes: 30 });

describe('Api.copy', () => {
  let api: Api;
  let bodies: unknown[];
  beforeEach(() => {
    bodies = [];
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { ended: () => undefined } }],
    });
    api = TestBed.inject(Api);
  });
  afterEach(() => vi.unstubAllGlobals());

  const serve = (res: Response) =>
    vi.stubGlobal(
      'fetch',
      vi.fn((_: string, init: RequestInit) => {
        bodies.push(JSON.parse(String(init.body)));
        return Promise.resolve(res);
      }),
    );

  it('hears the progress and ends with the new entry', async () => {
    const entry = {
      name: 'b',
      path: 'b',
      type: 'file',
      size: 30,
      modified: '2026-01-01T00:00:00Z',
      version: '1',
    };
    serve(ndjson([{ progress: p(0) }, { progress: p(2) }, { end: { entry, progress: p(3) } }]));
    const heard: number[] = [];
    const r = await api.copy('alex', 'a', 'b', { at: '@GMT-2026.11.23-15.00.09' }, (x) =>
      heard.push(x.files),
    );
    expect(bodies[0]).toEqual({ from: 'a', to: 'b', at: '@GMT-2026.11.23-15.00.09' });
    expect(heard).toEqual([0, 2]);
    expect(r.entry?.name).toBe('b');
    expect(r.progress.files).toBe(3);
  });

  it('says how far a copy got that failed part-way', async () => {
    serve(
      ndjson([
        { progress: p(1) },
        { problem: { status: 403, code: 'access_denied', title: 'no' }, progress: p(2) },
      ]),
    );
    const r = await api.copy('alex', 'a', 'b', {}, () => undefined);
    expect(r.entry).toBeUndefined();
    expect(r.error?.code).toBe('access_denied');
    expect(r.progress.files).toBe(2); // the server's count at the failure
  });
});
