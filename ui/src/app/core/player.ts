import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { Api, Entry } from './api';
import { hasThumbnail, previewKind } from './kinds';
import { Session } from './session';

/** A folder's recordings, played one after the other. */
export interface Queue {
  share: string;
  /** The folder's name, shown as the album. */
  folder: string;
  /** The snapshot the folder was read at, if one: its recordings play from there. */
  at: string | null;
  tracks: Entry[];
}

/**
 * Plays a folder's recordings as a playlist, on, while the person browses:
 * the browser's own audio element, streaming from ferry with Range as a
 * download does, nothing cached or kept. The system's media controls —
 * a phone's lock screen, a keyboard's keys — work through the Media
 * Session API, where there is one. Logging out stops it.
 */
@Injectable({ providedIn: 'root' })
export class Player {
  private readonly api = inject(Api);
  readonly queue = signal<Queue | null>(null);
  /** The index into the order played, not into the folder. */
  readonly index = signal(0);
  readonly playing = signal(false);
  readonly shuffle = signal(false);
  readonly position = signal(0);
  readonly duration = signal(0);
  /** The order the tracks play in: the folder's, or shuffled once when asked. */
  private readonly order = signal<number[]>([]);
  private audio: HTMLAudioElement | null = null;

  readonly track = computed(() => {
    const q = this.queue();
    const o = this.order();
    return q && o.length ? q.tracks[o[this.index()]] : null;
  });

  constructor() {
    const session = inject(Session);
    effect(() => {
      if (!session.me()) this.stop();
    });
  }

  /** The recordings among entries, in their order. */
  static recordings(entries: readonly Entry[]): Entry[] {
    return entries.filter((e) => e.type === 'file' && previewKind(e.name) === 'audio');
  }

  /** Plays the folder's recordings, from the one given or the first. */
  play(q: Queue, from?: Entry): void {
    if (!q.tracks.length) return;
    this.queue.set(q);
    const start = from
      ? Math.max(
          0,
          q.tracks.findIndex((t) => t.path === from.path),
        )
      : 0;
    this.order.set(this.makeOrder(q.tracks.length, start));
    this.index.set(0);
    this.load(true);
  }

  toggle(): void {
    const a = this.audio;
    if (!a) return;
    if (a.paused) this.start(a);
    else a.pause();
  }

  next(): void {
    if (this.index() + 1 < this.order().length) {
      this.index.update((i) => i + 1);
      this.load(true);
    } else {
      this.audio?.pause();
    }
  }

  previous(): void {
    // As players do: back to the start of the track, unless at its start.
    if (this.audio && this.audio.currentTime > 3) {
      this.audio.currentTime = 0;
      return;
    }
    if (this.index() > 0) {
      this.index.update((i) => i - 1);
      this.load(true);
    }
  }

  seek(seconds: number): void {
    if (this.audio) this.audio.currentTime = seconds;
  }

  setShuffle(on: boolean): void {
    this.shuffle.set(on);
    const q = this.queue();
    if (!q) return;
    // The track playing stays; what comes after it is reordered.
    const current = this.order()[this.index()];
    this.order.set(this.makeOrder(q.tracks.length, current));
    this.index.set(0);
  }

  stop(): void {
    if (this.audio) {
      this.audio.pause();
      this.audio.removeAttribute('src');
      this.audio.load();
    }
    this.queue.set(null);
    this.order.set([]);
    this.playing.set(false);
    this.position.set(0);
    this.duration.set(0);
    const ms = mediaSession();
    if (ms) ms.metadata = null;
  }

  /** The cover of a track, when it carries one the server can show. */
  cover(t: Entry, size: 160 | 320 = 160): string | null {
    const q = this.queue();
    return q && hasThumbnail(t.name) ? this.api.thumbnailUrl(q.share, t.path, size, t.version, q.at) : null;
  }

  private makeOrder(n: number, first: number): number[] {
    const rest = Array.from({ length: n }, (_, i) => i).filter((i) => i !== first);
    if (this.shuffle()) {
      for (let i = rest.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [rest[i], rest[j]] = [rest[j], rest[i]];
      }
      return [first, ...rest];
    }
    // In order: from the one chosen to the end of the folder.
    return Array.from({ length: n - first }, (_, i) => first + i);
  }

  private load(autoplay: boolean): void {
    const q = this.queue();
    const t = this.track();
    if (!q || !t) return;
    const a = (this.audio ??= this.makeAudio());
    a.src = this.api.contentUrl(q.share, t.path, true, q.at);
    this.position.set(0);
    if (autoplay) this.start(a);
    const ms = mediaSession();
    if (ms && typeof MediaMetadata !== 'undefined') {
      const cover = this.cover(t, 320);
      ms.metadata = new MediaMetadata({
        title: t.name.replace(/\.[^.]+$/, ''),
        album: q.folder,
        artwork: cover ? [{ src: cover, sizes: '320x320', type: 'image/jpeg' }] : [],
      });
    }
  }

  /** Plays; a browser that refuses (no gesture yet) leaves it paused. */
  private start(a: HTMLAudioElement): void {
    const p = a.play() as Promise<void> | undefined;
    p?.catch(() => this.playing.set(false));
  }

  private makeAudio(): HTMLAudioElement {
    const a = new Audio();
    a.preload = 'auto';
    a.addEventListener('play', () => this.playing.set(true));
    a.addEventListener('pause', () => this.playing.set(false));
    a.addEventListener('ended', () => this.next());
    a.addEventListener('timeupdate', () => this.position.set(a.currentTime));
    a.addEventListener('durationchange', () => this.duration.set(isFinite(a.duration) ? a.duration : 0));
    const ms = mediaSession();
    if (ms) {
      const on = (action: MediaSessionAction, h: MediaSessionActionHandler) => {
        try {
          ms.setActionHandler(action, h);
        } catch {
          // An action this browser does not know.
        }
      };
      on('play', () => this.toggle());
      on('pause', () => this.toggle());
      on('nexttrack', () => this.next());
      on('previoustrack', () => this.previous());
      on('seekto', (d) => d.seekTime !== undefined && this.seek(d.seekTime));
    }
    return a;
  }
}

function mediaSession(): MediaSession | null {
  return typeof navigator !== 'undefined' && 'mediaSession' in navigator ? navigator.mediaSession : null;
}
