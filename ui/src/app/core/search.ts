import { Injectable, inject, signal } from '@angular/core';
import { Api, Entry, SearchEnd } from './api';
import { describe } from './errors';

/**
 * A search below the folder shown: the server lists the folders one by one
 * and the hits come in as it finds them. One at a time: a new search, or
 * none, aborts the one running, and the server stops before its next
 * folder. Nothing is kept beyond the hits on screen.
 */
@Injectable({ providedIn: 'root' })
export class Search {
  private readonly api = inject(Api);
  /** Whether the search field looks below the folder, not only in it. */
  readonly deep = signal(false);
  readonly hits = signal<Entry[]>([]);
  readonly end = signal<SearchEnd | null>(null);
  readonly running = signal(false);
  readonly error = signal<string | null>(null);
  /** What is searched for, and where: null while no search is shown. */
  readonly query = signal<{ share: string; path: string; q: string } | null>(null);
  private abort: AbortController | null = null;
  private runs = 0;

  /** Searches for q below the folder, replacing whatever ran before. */
  async run(share: string, path: string, q: string): Promise<void> {
    this.stop();
    const n = ++this.runs;
    const abort = new AbortController();
    this.abort = abort;
    this.query.set({ share, path, q });
    this.hits.set([]);
    this.end.set(null);
    this.error.set(null);
    this.running.set(true);
    try {
      const end = await this.api.search(
        share,
        path,
        q,
        (batch) => n === this.runs && this.hits.update((h) => [...h, ...batch]),
        abort.signal,
      );
      if (n === this.runs) this.end.set(end);
    } catch (e) {
      if (n === this.runs && !abort.signal.aborted) this.error.set(describe(e, 'list'));
    } finally {
      if (n === this.runs) this.running.set(false);
    }
  }

  /** Ends the search running, if one is, and clears its hits. */
  stop(): void {
    this.abort?.abort();
    this.abort = null;
    this.runs++;
    this.running.set(false);
    this.query.set(null);
    this.hits.set([]);
    this.end.set(null);
    this.error.set(null);
  }
}
