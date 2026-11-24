import { Injectable, inject, signal } from '@angular/core';
import { Api, Usage } from './api';
import { describe } from './errors';

/** A folder being added up, or added up: what the size card shows. */
export interface Sizing {
  share: string;
  path: string;
  name: string;
  usage: Usage | null;
  running: boolean;
  error: string | null;
}

/**
 * Folder sizes, on request only: the server lists the folders below one by
 * one and counts, and the count comes in as it grows. One at a time — the
 * server ends a session's earlier one too — and nothing is kept.
 */
@Injectable({ providedIn: 'root' })
export class FolderSize {
  private readonly api = inject(Api);
  readonly current = signal<Sizing | null>(null);
  private abort: AbortController | null = null;
  private runs = 0;

  async run(share: string, path: string, name: string): Promise<void> {
    this.abort?.abort();
    const n = ++this.runs;
    const abort = new AbortController();
    this.abort = abort;
    const base: Sizing = { share, path, name, usage: null, running: true, error: null };
    this.current.set(base);
    try {
      const end = await this.api.usage(
        share,
        path,
        (u) => n === this.runs && this.current.set({ ...base, usage: u }),
        abort.signal,
      );
      if (n === this.runs) this.current.set({ ...base, usage: end, running: false });
    } catch (e) {
      if (n === this.runs && !abort.signal.aborted)
        this.current.update((c) => (c ? { ...c, running: false, error: describe(e) } : c));
    }
  }

  /** Stops counting, keeping what was counted, or closes the card when done. */
  stop(): void {
    const c = this.current();
    this.abort?.abort();
    this.runs++;
    if (c?.running)
      this.current.set({ ...c, running: false, usage: c.usage && { ...c.usage, truncated: true } });
    else this.current.set(null);
  }
}
