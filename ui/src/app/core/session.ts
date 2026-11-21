import { Injectable, signal } from '@angular/core';
import type { components } from './schema';

type Me = components['schemas']['Me'];
type Info = components['schemas']['Info'];

/** Why the login page is shown: the start, a logout, or a session the server ended. */
export type Reason = 'start' | 'logout' | 'expired' | 'lost';

/**
 * Who is logged in. ferry's session lives in an HttpOnly cookie the
 * interface never sees; this only mirrors what the server said about it.
 */
@Injectable({ providedIn: 'root' })
export class Session {
  /** What the login page needs: the server's name and the timeouts. */
  readonly info = signal<Info | null>(null);
  /** The current session; null when logged out, undefined while finding out. */
  readonly me = signal<Me | null | undefined>(undefined);
  readonly reason = signal<Reason>('start');

  started(me: Me): void {
    this.reason.set('start');
    this.me.set(me);
  }

  /** The server ended the session, or the user logged out. */
  ended(reason: Reason = 'expired'): void {
    // Without a session to end — the first look at /me, or a second 401 —
    // there is nothing to explain on the login page.
    if (!this.me()) {
      this.me.set(null);
      return;
    }
    this.reason.set(reason);
    this.me.set(null);
  }
}
