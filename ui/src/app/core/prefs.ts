import { Injectable, effect, signal } from '@angular/core';

export type Theme = 'light' | 'dark';
export type View = 'grid' | 'list';

/**
 * The viewer's choices: theme and view. They live in the browser only —
 * ferry has nowhere else to keep them — and survive a blocked storage by
 * falling back to the defaults.
 */
@Injectable({ providedIn: 'root' })
export class Prefs {
  readonly theme = signal<Theme>(read('ferry.theme', ['light', 'dark']) ?? systemTheme());
  readonly view = signal<View>(read('ferry.view', ['grid', 'list']) ?? 'grid');

  constructor() {
    effect(() => {
      document.documentElement.dataset['theme'] = this.theme();
      write('ferry.theme', this.theme());
    });
    effect(() => write('ferry.view', this.view()));
  }

  toggleTheme(): void {
    this.theme.update((t) => (t === 'dark' ? 'light' : 'dark'));
  }
}

function systemTheme(): Theme {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function read<T extends string>(key: string, allowed: T[]): T | null {
  try {
    const v = localStorage.getItem(key) as T | null;
    return v && allowed.includes(v) ? v : null;
  } catch {
    return null;
  }
}

function write(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage blocked: the choice lasts for this page only.
  }
}
