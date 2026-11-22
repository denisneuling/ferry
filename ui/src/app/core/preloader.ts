import { Injectable } from '@angular/core';
import { Entry } from './api';
import { previewKind } from './kinds';

/**
 * Loads images ahead into the browser, so that the next one in the preview
 * is there when the person steps to it. It asks for exactly the URL the
 * preview will show, so the browser has it already; and it holds only what
 * it was last asked for — a request for an image no longer wanted is
 * dropped, so that stepping quickly through a folder does not queue a
 * download for every image passed.
 */
@Injectable({ providedIn: 'root' })
export class Preloader {
  private readonly held = new Map<string, HTMLImageElement>();

  /** Loads these URLs ahead and lets go of all others; none when data is to be saved. */
  want(urls: readonly string[]): void {
    const keep = new Set(saveData() ? [] : urls);
    for (const [url, img] of this.held) {
      if (keep.has(url)) continue;
      img.src = ''; // stops a load still running
      this.held.delete(url);
    }
    for (const url of keep) {
      if (this.held.has(url)) continue;
      const img = new Image();
      img.decoding = 'async';
      img.src = url;
      this.held.set(url, img);
    }
  }

  /** What is being held now, for the tests. */
  holding(): string[] {
    return [...this.held.keys()];
  }
}

/**
 * The files next to the i-th one, the next first, round the folder's end
 * as the preview steps — images only: a video or a PDF is large, and the
 * browser loads only the start of one anyway.
 */
export function neighbours(files: readonly Entry[], i: number): Entry[] {
  const n = files.length;
  const out: Entry[] = [];
  for (const j of [(i + 1) % n, (i - 1 + n) % n]) {
    const f = files[j];
    if (n > 1 && j !== i && f && !out.includes(f) && previewKind(f.name) === 'image') out.push(f);
  }
  return out;
}

/** Whether the person asked the browser to save data (Network Information API, where there is one). */
function saveData(): boolean {
  return (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData === true;
}
