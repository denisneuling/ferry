import { TestBed } from '@angular/core/testing';
import { Api, ApiError, Entry } from './api';
import { Session } from './session';

const entry = (name: string): Entry => ({
  name,
  path: name,
  type: 'file',
  size: 1,
  modified: '2026-01-01T00:00:00Z',
  version: '1-0',
});

/** A response whose body arrives in the given pieces. */
function streamed(pieces: string[], status = 200): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      for (const p of pieces) c.enqueue(enc.encode(p));
      c.close();
    },
  });
  return new Response(body, { status, headers: { 'Content-Type': 'application/x-ndjson' } });
}

/** Cuts a text into pieces at arbitrary places, lines torn apart included. */
function torn(text: string, size: number): string[] {
  const out: string[] = [];
  for (let i = 0; i < text.length; i += size) out.push(text.slice(i, i + size));
  return out;
}

describe('Api.entriesStream', () => {
  let api: Api;
  let ended: string[];
  beforeEach(() => {
    ended = [];
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { ended: (r?: string) => ended.push(r ?? 'logout') } }],
    });
    api = TestBed.inject(Api);
  });
  afterEach(() => vi.unstubAllGlobals());

  const lines = (names: string[], last: object) =>
    names.map((n) => JSON.stringify(entry(n)) + '\n').join('') + JSON.stringify(last) + '\n';

  it('hands on every entry, however the lines are torn, and returns the count', async () => {
    const names = Array.from({ length: 1500 }, (_, i) => `photo-${i}.jpg`);
    const text = lines(names, { end: { count: names.length } });
    for (const size of [1, 7, 333, 65536]) {
      vi.stubGlobal('fetch', async () => streamed(torn(text, size)));
      const got: string[] = [];
      const count = await api.entriesStream('alex', 'Many', (b) => got.push(...b.map((e) => e.name)));
      expect(count).toBe(1500);
      expect(got).toEqual(names);
    }
  });

  it('turns a problem in the last line into an error, with its limit', async () => {
    const text = lines(['a', 'b'], {
      problem: { type: 'about:blank', title: 't', status: 413, code: 'too_large', limit: 2000 },
    });
    vi.stubGlobal('fetch', async () => streamed([text]));
    const err = await api.entriesStream('alex', 'Many', () => undefined).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('too_large');
    expect(err.limit).toBe(2000);
  });

  it('treats a stream without a last line as cut off', async () => {
    vi.stubGlobal('fetch', async () => streamed([JSON.stringify(entry('a')) + '\n']));
    const err = await api.entriesStream('alex', 'Many', () => undefined).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('network');
  });

  it('reads a problem response before the stream as usual', async () => {
    vi.stubGlobal(
      'fetch',
      async () =>
        new Response(
          JSON.stringify({ type: 'about:blank', title: 't', status: 413, code: 'too_large', limit: 100 }),
          {
            status: 413,
          },
        ),
    );
    const err = await api.entriesStream('alex', 'Many', () => undefined).catch((e) => e);
    expect(err.status).toBe(413);
    expect(err.limit).toBe(100);
  });
});

describe('Api, versioned URLs', () => {
  let api: Api;
  let asked: string[];
  beforeEach(() => {
    asked = [];
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { ended: () => undefined } }],
    });
    api = TestBed.inject(Api);
    vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
      asked.push(String(input));
      return new Response('{"kind":"other"}', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("puts the entry's version into the thumbnail's URL, so the browser keeps it", () => {
    expect(api.thumbnailUrl('alex', 'Photos/a b.jpg', 320, '1f-18a')).toBe(
      '/api/v1/shares/alex/thumbnail?size=320&path=Photos%2Fa%20b.jpg&v=1f-18a',
    );
    expect(api.thumbnailUrl('alex', 'a.jpg', 160)).toBe('/api/v1/shares/alex/thumbnail?size=160&path=a.jpg');
  });

  it('asks for the details with the version when there is one', async () => {
    await api.metadata('alex', 'a.jpg', '1f-18a');
    await api.metadata('alex', 'a.jpg');
    expect(asked).toEqual([
      '/api/v1/shares/alex/metadata?path=a.jpg&v=1f-18a',
      '/api/v1/shares/alex/metadata?path=a.jpg',
    ]);
  });
});
