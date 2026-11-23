import { TestBed } from '@angular/core/testing';
import { Entry } from './api';
import { Search } from './search';
import { Session } from './session';

const hit = (path: string): Entry => ({
  name: path.slice(path.lastIndexOf('/') + 1),
  path,
  type: 'file',
  size: 1,
  modified: '2026-01-01T00:00:00Z',
  version: '1-0',
});

function streamed(text: string): Response {
  return new Response(text, { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } });
}

describe('Search', () => {
  let search: Search;
  let urls: string[];
  beforeEach(() => {
    urls = [];
    TestBed.configureTestingModule({
      providers: [{ provide: Session, useValue: { ended: () => undefined } }],
    });
    search = TestBed.inject(Search);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('gathers the hits as they come, and how the search ended', async () => {
    const lines =
      [hit('a/Rechnung.pdf'), hit('a/b/rechnung.txt')]
        .map((h) => JSON.stringify({ hit: h }) + '\n')
        .join('') +
      JSON.stringify({ end: { hits: 2, folders: 3, skipped: 1, truncated: false } }) +
      '\n';
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        urls.push(url);
        return Promise.resolve(streamed(lines));
      }),
    );
    await search.run('alex', 'a', 'rechnung');
    expect(urls[0]).toContain('/shares/alex/search?path=a&q=rechnung');
    expect(search.hits().map((h) => h.path)).toEqual(['a/Rechnung.pdf', 'a/b/rechnung.txt']);
    expect(search.end()).toEqual({ hits: 2, folders: 3, skipped: 1, truncated: false });
    expect(search.running()).toBe(false);
  });

  it('a stream cut off is an error, and stop() clears everything', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(streamed(JSON.stringify({ hit: hit('x') }) + '\n'))),
    );
    await search.run('alex', '', 'x');
    expect(search.error()).not.toBeNull();
    search.stop();
    expect(search.query()).toBeNull();
    expect(search.hits()).toEqual([]);
  });
});
