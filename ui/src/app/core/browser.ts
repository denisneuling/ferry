import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { Api, ApiError, Entry, Share } from './api';
import { Format } from './format';
import { describe } from './errors';
import { Session } from './session';
import { Snapshot } from './snapshot';
import { Places } from './places';

export type SortKey = 'name' | 'modified' | 'size';

/**
 * The folders kept in memory for going back to them without waiting: the
 * last ones opened, so many entries in all, and none larger than a folder
 * worth keeping — a folder of a hundred thousand entries would push out
 * everything else, and lists in a second anyway.
 */
export const MEMORY_FOLDERS = 20;
export const MEMORY_ENTRIES = 100_000;
export const MEMORY_LARGEST = 20_000;

/**
 * What the person is looking at: the shares, the folder open, its entries,
 * the selection, the sort and the filter. Pages and panels read it; actions
 * that change a folder ask it to reload.
 */
@Injectable({ providedIn: 'root' })
export class Browser {
  private readonly api = inject(Api);
  private readonly format = inject(Format);
  private readonly snapshot = inject(Snapshot);
  private readonly places = inject(Places);

  readonly shares = signal<Share[] | null>(null);
  readonly sharesError = signal<string | null>(null);

  readonly share = signal<string | null>(null);
  /** The folder, slash-separated, '' for the share's root. */
  readonly path = signal('');
  readonly entries = signal<Entry[] | null>(null);
  /** Whether a listing is still arriving: entries() may be its first part. */
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly errorCode = signal<string | null>(null);

  readonly sort = signal<SortKey>('name');
  readonly desc = signal(false);
  readonly filter = signal('');
  readonly selected = signal<ReadonlySet<string>>(new Set());
  private anchor: string | null = null;
  /** The index into files() of the file previewed, or null. */
  readonly preview = signal<number | null>(null);
  /** A name to select once the folder opening has listed it: a search's hit. */
  readonly reveal = signal<string | null>(null);
  /** Whether the name revealed is a file to preview once it is there. */
  revealOpens = false;

  private loads = 0;
  private abort: AbortController | null = null;
  /**
   * The folders last listed, the most recent last: a folder opened again
   * shows at once from here while its listing is read anew. Only in this
   * tab's memory — never in the browser's storage — and gone with the
   * session, whoever logs in next.
   */
  private readonly memory = new Map<string, Entry[]>();
  private remembered = 0;

  constructor() {
    const session = inject(Session);
    effect(() => {
      if (!session.me()) this.forget();
    });
  }

  /** The folder's entries, filtered and sorted: folders first, then files. */
  readonly visible = computed(() => {
    const list = this.entries() ?? [];
    const f = this.filter().trim().toLowerCase();
    const shown = f ? list.filter((e) => e.name.toLowerCase().includes(f)) : list;
    const key = this.sort();
    const dir = this.desc() ? -1 : 1;
    const cmp = (a: Entry, b: Entry): number => {
      if (key === 'size' && a.size !== b.size) return (a.size - b.size) * dir;
      if (key === 'modified' && a.modified !== b.modified) return (a.modified < b.modified ? -1 : 1) * dir;
      return this.format.compare(a.name, b.name) * (key === 'name' ? dir : 1);
    };
    return [...shown].sort((a, b) => (a.type !== b.type ? (a.type === 'folder' ? -1 : 1) : cmp(a, b)));
  });
  readonly folders = computed(() => this.visible().filter((e) => e.type === 'folder'));
  readonly files = computed(() => this.visible().filter((e) => e.type === 'file'));
  readonly selection = computed(() => this.visible().filter((e) => this.selected().has(e.name)));

  /** The name the folder is shown under: its own, or the share's. */
  readonly title = computed(() => {
    const p = this.path();
    return p ? p.slice(p.lastIndexOf('/') + 1) : (this.share() ?? '');
  });

  async loadShares(): Promise<void> {
    try {
      this.shares.set(await this.api.shares());
      this.sharesError.set(null);
    } catch (e) {
      this.sharesError.set(describe(e));
    }
  }

  /**
   * Opens a folder, as it is now or, with at, as that snapshot holds it; a
   * different one clears the selection, the filter and the preview.
   */
  async open(share: string, path: string, at: string | null = null): Promise<void> {
    const same = share === this.share() && path === this.path() && at === this.snapshot.at();
    if (!same) {
      this.share.set(share);
      this.path.set(path);
      this.snapshot.at.set(at);
      // A folder seen a moment ago shows as it was, and its new listing
      // replaces it when complete (stale while revalidating).
      this.entries.set(this.recall(share, path));
      this.filter.set('');
      this.clear();
      this.preview.set(null);
    }
    await this.reload();
  }

  /** Reads the folder again, keeping the selection of what is still there. */
  async reload(): Promise<void> {
    const share = this.share();
    if (share === null) {
      return;
    }
    const path = this.path();
    const n = ++this.loads;
    this.abort?.abort();
    const abort = new AbortController();
    this.abort = abort;
    // A folder opened afresh shows its first entries at once and fills in
    // while the rest arrive; a folder already shown is replaced only when
    // its new listing is complete, so that an action's reload does not
    // flicker. Updates come at most every few hundred milliseconds: each one
    // sorts the whole folder again.
    const progressive = this.entries() === null;
    const got: Entry[] = [];
    // A server may list a name twice when the folder changes under the
    // listing; the interface keys entries by name, so the first one counts.
    const names = new Set<string>();
    let shownAt = -Infinity;
    this.loading.set(true);
    try {
      await this.api.entriesStream(
        share,
        path,
        (batch) => {
          if (n !== this.loads) return;
          for (const e of batch) {
            if (names.has(e.name)) continue;
            names.add(e.name);
            got.push(e);
          }
          const now = performance.now();
          if (progressive && now - shownAt > 400) {
            this.entries.set(got.slice());
            shownAt = now;
          }
        },
        abort.signal,
      );
      if (n !== this.loads) {
        return; // A newer load is on its way.
      }
      this.entries.set(got);
      this.remember(share, path, got);
      this.error.set(null);
      this.errorCode.set(null);
      this.selected.update((s) => new Set([...s].filter((x) => names.has(x))));
      if (!this.snapshot.at()) this.places.used({ share, path, folder: true });
      const r = this.reveal();
      if (r !== null) {
        this.reveal.set(null);
        if (names.has(r)) {
          this.only(r);
          if (this.revealOpens) {
            const i = this.files().findIndex((e) => e.name === r);
            if (i >= 0) this.preview.set(i);
          }
        }
        this.revealOpens = false;
      }
    } catch (e) {
      if (n !== this.loads) {
        return;
      }
      this.entries.set(null);
      this.drop(this.key(share, path));
      this.error.set(describe(e, 'list'));
      this.errorCode.set(e instanceof ApiError ? e.code : 'internal');
    } finally {
      if (n === this.loads) {
        this.loading.set(false);
      }
    }
  }

  leave(): void {
    this.abort?.abort();
    this.loads++;
    this.loading.set(false);
    this.snapshot.at.set(null);
    this.share.set(null);
    this.path.set('');
    this.entries.set(null);
    this.error.set(null);
    this.clear();
    this.preview.set(null);
  }

  reset(): void {
    this.leave();
    this.shares.set(null);
    this.forget();
  }

  private recall(share: string, path: string): Entry[] | null {
    const k = this.key(share, path);
    const got = this.memory.get(k);
    if (!got) return null;
    this.memory.delete(k); // to the end: the most recent
    this.memory.set(k, got);
    return got;
  }

  private remember(share: string, path: string, entries: Entry[]): void {
    const k = this.key(share, path);
    this.drop(k);
    if (entries.length > MEMORY_LARGEST) return;
    this.memory.set(k, entries);
    this.remembered += entries.length;
    // The oldest go first, until both bounds hold.
    for (const old of this.memory.keys()) {
      if (this.memory.size <= MEMORY_FOLDERS && this.remembered <= MEMORY_ENTRIES) break;
      this.drop(old);
    }
  }

  private key(share: string, path: string): string {
    return key(share, path, this.snapshot.at());
  }

  private drop(k: string): void {
    this.remembered -= this.memory.get(k)?.length ?? 0;
    this.memory.delete(k);
  }

  private forget(): void {
    this.memory.clear();
    this.remembered = 0;
  }

  setSort(key: SortKey): void {
    if (this.sort() === key) {
      this.desc.update((d) => !d);
    } else {
      this.sort.set(key);
      this.desc.set(key !== 'name');
    }
  }

  toggle(name: string, range = false): void {
    const names = this.visible().map((e) => e.name);
    this.selected.update((s) => {
      const next = new Set(s);
      if (range && this.anchor !== null && names.includes(this.anchor)) {
        const [a, b] = [names.indexOf(this.anchor), names.indexOf(name)].sort((x, y) => x - y);
        names.slice(a, b + 1).forEach((n) => next.add(n));
      } else {
        if (next.has(name)) next.delete(name);
        else next.add(name);
        this.anchor = name;
      }
      return next;
    });
  }

  only(name: string): void {
    this.selected.set(new Set([name]));
    this.anchor = name;
  }

  selectAll(): void {
    this.selected.set(new Set(this.visible().map((e) => e.name)));
  }

  clear(): void {
    this.selected.set(new Set());
    this.anchor = null;
  }
}

/** A folder's place in memory: the share, the path, and the snapshot, if one. */
function key(share: string, path: string, at: string | null): string {
  return `${share}\u0000${path}\u0000${at ?? ''}`;
}
