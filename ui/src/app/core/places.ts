import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import type { Browser } from './browser';
import { Session } from './session';

/** A folder or a file someone keeps at hand. */
export interface Place {
  share: string;
  /** Slash-separated; '' for a share's root. */
  path: string;
  folder: boolean;
}

export const MAX_FAVOURITES = 50;
export const MAX_RECENT = 20;

/**
 * Favourites and what was used last. They live in this browser only, per
 * server and user — ferry keeps nothing — and are never checked against the
 * share: a place that is gone shows as gone when it is opened, as any link
 * would. Snapshots are not remembered; they are a moment, not a place.
 */
@Injectable({ providedIn: 'root' })
export class Places {
  private readonly session = inject(Session);
  private readonly key = computed(() => {
    const me = this.session.me();
    return me ? `ferry.places.${me.server}.${me.username}` : null;
  });
  readonly favourites = signal<Place[]>([]);
  readonly recent = signal<Place[]>([]);

  constructor() {
    effect(() => {
      const k = this.key();
      const stored = k ? load(k) : { favourites: [], recent: [] };
      this.favourites.set(stored.favourites);
      this.recent.set(stored.recent);
    });
  }

  isFavourite(p: Place): boolean {
    return this.favourites().some((f) => same(f, p));
  }

  toggleFavourite(p: Place): void {
    const list = this.favourites();
    this.favourites.set(
      list.some((f) => same(f, p)) ? list.filter((f) => !same(f, p)) : [...list, p].slice(-MAX_FAVOURITES),
    );
    this.save();
  }

  /** Puts a place first among the recent ones. */
  used(p: Place): void {
    if (!this.key()) return;
    const first = this.recent()[0];
    if (first && same(first, p)) return; // a folder read again, after an action
    this.recent.set([p, ...this.recent().filter((r) => !same(r, p))].slice(0, MAX_RECENT));
    this.save();
  }

  forget(p: Place): void {
    this.recent.set(this.recent().filter((r) => !same(r, p)));
    this.save();
  }

  clearRecent(): void {
    this.recent.set([]);
    this.save();
  }

  private save(): void {
    const k = this.key();
    if (!k) return;
    try {
      localStorage.setItem(k, JSON.stringify({ favourites: this.favourites(), recent: this.recent() }));
    } catch {
      // Storage blocked or full: the places last for this page only.
    }
  }
}

function same(a: Place, b: Place): boolean {
  return a.share === b.share && a.path === b.path && a.folder === b.folder;
}

/** What the browser keeps, read defensively: anything malformed is left out. */
export function load(key: string): { favourites: Place[]; recent: Place[] } {
  let raw: unknown = null;
  try {
    raw = JSON.parse(localStorage.getItem(key) ?? 'null');
  } catch {
    raw = null;
  }
  const r = (raw ?? {}) as { favourites?: unknown; recent?: unknown };
  return { favourites: places(r.favourites, MAX_FAVOURITES), recent: places(r.recent, MAX_RECENT) };
}

function places(v: unknown, max: number): Place[] {
  if (!Array.isArray(v)) return [];
  return v
    .filter(
      (p): p is Place =>
        typeof p === 'object' &&
        p !== null &&
        typeof (p as Place).share === 'string' &&
        (p as Place).share !== '' &&
        typeof (p as Place).path === 'string' &&
        typeof (p as Place).folder === 'boolean',
    )
    .map((p) => ({ share: p.share, path: p.path, folder: p.folder }))
    .slice(0, max);
}

/** The name a place is shown by: its last part, or the share's name. */
export function placeName(p: Place): string {
  return p.path ? p.path.slice(p.path.lastIndexOf('/') + 1) : p.share;
}

/** Where a place is: the share and the folders above it. */
export function placeWhere(p: Place): string {
  const i = p.path.lastIndexOf('/');
  return i < 0 ? p.share : `${p.share} / ${p.path.slice(0, i).split('/').join(' / ')}`;
}

/**
 * Goes to a place: a folder opens; a file opens in its folder's preview,
 * at once when that folder is the one shown, or once it has been listed.
 */
export function openPlace(p: Place, router: Router, browser: Browser): void {
  if (p.folder) {
    void router.navigate(['/browse', p.share], { queryParams: p.path ? { path: p.path } : {} });
    return;
  }
  const i = p.path.lastIndexOf('/');
  const dir = i < 0 ? '' : p.path.slice(0, i);
  const name = p.path.slice(i + 1);
  browser.reveal.set(name);
  browser.revealOpens = true;
  const snapshot = router.parseUrl(router.url).queryParamMap.has('at');
  if (browser.share() === p.share && browser.path() === dir && !snapshot) {
    // The folder shown: no navigation happens, so it is read again here.
    browser.filter.set('');
    void browser.reload();
    return;
  }
  void router.navigate(['/browse', p.share], { queryParams: dir ? { path: dir } : {} });
}
