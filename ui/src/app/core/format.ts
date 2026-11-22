import { LOCALE_ID, inject, Injectable } from '@angular/core';

/** Bytes in decimal units, as file managers show them: 3.2 MB. */
export function formatSize(n: number, locale?: string): string {
  if (n < 1000) {
    return `${n} B`;
  }
  const units = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  let v = n;
  do {
    v /= 1000;
    i++;
  } while (v >= 1000 && i < units.length - 1);
  return `${v.toLocaleString(locale, { maximumFractionDigits: v < 10 ? 1 : 0 })} ${units[i]}`;
}

/** Sizes and dates in the interface's language. */
@Injectable({ providedIn: 'root' })
export class Format {
  private readonly locale = inject(LOCALE_ID);
  // One collator for every comparison: localeCompare with options builds a
  // new one per call, which a sort of fifty thousand names feels.
  private readonly collator = new Intl.Collator(this.locale, { numeric: true, sensitivity: 'base' });

  /** A count, with the language's grouping: 60.000 in German. */
  count(n: number): string {
    return n.toLocaleString(this.locale);
  }

  /** Bytes in decimal units, as file managers show them: 3.2 MB. */
  size(n: number | null | undefined): string {
    return n == null ? '—' : formatSize(n, this.locale);
  }

  /** A modification time: today and yesterday by time, this year by day, older with the year. */
  date(iso: string, now = new Date()): string {
    const d = new Date(iso);
    const time = d.toLocaleTimeString(this.locale, { hour: '2-digit', minute: '2-digit' });
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    if (d.getTime() >= startOfToday) {
      return $localize`:@@date.today:Today, ${time}:time:`;
    }
    if (d.getTime() >= startOfToday - 864e5) {
      return $localize`:@@date.yesterday:Yesterday, ${time}:time:`;
    }
    if (d.getFullYear() === now.getFullYear()) {
      return d.toLocaleDateString(this.locale, { day: 'numeric', month: 'short' });
    }
    return d.toLocaleDateString(this.locale, { day: 'numeric', month: 'short', year: 'numeric' });
  }

  /** A full date and time, for the details pane. */
  dateTime(iso: string): string {
    return new Date(iso).toLocaleString(this.locale, { dateStyle: 'medium', timeStyle: 'short' });
  }

  /** Names compared as people sort them: numbers by value, case and accents aside. */
  compare(a: string, b: string): number {
    return this.collator.compare(a, b);
  }
}
