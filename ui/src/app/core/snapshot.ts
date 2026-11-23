import { Injectable, signal } from '@angular/core';

/**
 * The time the interface reads the share at: null for the share as it is
 * now, or a snapshot's token (`@GMT-YYYY.MM.DD-HH.MM.SS`, UTC) while a
 * folder is browsed as a previous version holds it. Every read the Api
 * makes carries it; nothing that changes a file does, and the server
 * refuses a change that would.
 */
@Injectable({ providedIn: 'root' })
export class Snapshot {
  readonly at = signal<string | null>(null);
}

const TOKEN = /^@GMT-(\d{4})\.(\d\d)\.(\d\d)-(\d\d)\.(\d\d)\.(\d\d)$/;

/** Whether s is a snapshot token as the server writes one. */
export function isToken(s: string | null | undefined): s is string {
  return !!s && TOKEN.test(s);
}

/** The moment a snapshot token names. */
export function tokenTime(token: string): Date | null {
  const m = TOKEN.exec(token);
  if (!m) return null;
  const [, y, mo, d, h, mi, s] = m.map(Number);
  return new Date(Date.UTC(y, mo - 1, d, h, mi, s));
}
