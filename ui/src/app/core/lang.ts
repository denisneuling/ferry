import { Injectable, LOCALE_ID, inject } from '@angular/core';

/** The languages the interface is built in (angular.json), and their names in themselves. */
export const LANGUAGES = [
  { code: 'en', name: 'English' },
  { code: 'de', name: 'Deutsch' },
] as const;

export type Lang = (typeof LANGUAGES)[number]['code'];

/**
 * The interface is built once per language, each under its own path (/en/,
 * /de/). Switching loads the other build; the choice is kept in a cookie the
 * server reads to send a path without a language to it — the browser is the
 * only place ferry keeps it.
 */
@Injectable({ providedIn: 'root' })
export class Language {
  readonly current: Lang = (inject(LOCALE_ID).slice(0, 2) === 'de' ? 'de' : 'en') as Lang;

  get other(): (typeof LANGUAGES)[number] {
    return LANGUAGES.find((l) => l.code !== this.current)!;
  }

  switch(to: Lang): void {
    if (to === this.current) {
      return;
    }
    document.cookie = `ferry-lang=${to}; Path=/; Max-Age=31536000; SameSite=Strict`;
    const rest = location.pathname.replace(/^\/(en|de)(?=\/|$)/, '');
    location.assign(`/${to}${rest || '/'}${location.search}`);
  }
}
