import { Injectable, computed, inject, signal } from '@angular/core';
import { Api, ApiError, Entry, Share } from './api';
import { Format } from './format';
import { describe } from './errors';

export type SortKey = 'name' | 'modified' | 'size';

/**
 * What the person is looking at: the shares, the folder open, its entries,
 * the selection, the sort and the filter. Pages and panels read it; actions
 * that change a folder ask it to reload.
 */
@Injectable({ providedIn: 'root' })
export class Browser {
  private readonly api = inject(Api);
  private readonly format = inject(Format);

  readonly shares = signal<Share[] | null>(null);
  readonly sharesError = signal<string | null>(null);

  readonly share = signal<string | null>(null);
  /** The folder, slash-separated, '' for the share's root. */
  readonly path = signal('');
  readonly entries = signal<Entry[] | null>(null);
  readonly error = signal<string | null>(null);
  readonly errorCode = signal<string | null>(null);

  readonly sort = signal<SortKey>('name');
  readonly desc = signal(false);
  readonly filter = signal('');
  readonly selected = signal<ReadonlySet<string>>(new Set());
  private anchor: string | null = null;
  /** The index into files() of the file previewed, or null. */
  readonly preview = signal<number | null>(null);

  private loads = 0;

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

  /** Opens a folder; a different one clears the selection, the filter and the preview. */
  async open(share: string, path: string): Promise<void> {
    const same = share === this.share() && path === this.path();
    if (!same) {
      this.share.set(share);
      this.path.set(path);
      this.entries.set(null);
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
    try {
      const list = await this.api.entries(share, path);
      if (n !== this.loads) {
        return; // A newer load is on its way.
      }
      this.entries.set(list.entries);
      this.error.set(null);
      this.errorCode.set(null);
      const names = new Set(list.entries.map((e) => e.name));
      this.selected.update((s) => new Set([...s].filter((x) => names.has(x))));
    } catch (e) {
      if (n !== this.loads) {
        return;
      }
      this.entries.set(null);
      this.error.set(describe(e));
      this.errorCode.set(e instanceof ApiError ? e.code : 'internal');
    }
  }

  leave(): void {
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
