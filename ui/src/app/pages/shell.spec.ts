import { LOCALE_ID, NO_ERRORS_SCHEMA, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, RouterLink, RouterOutlet } from '@angular/router';
import { Api } from '../core/api';
import { Browser } from '../core/browser';
import { Language } from '../core/lang';
import { Prefs } from '../core/prefs';
import { Session } from '../core/session';
import { Transfers } from '../core/transfers';
import { Icon } from '../ui/icon';
import { Mark } from '../ui/mark';
import { Toasts } from '../ui/toast';
import { Shell } from './shell';

describe('Shell', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        { provide: LOCALE_ID, useValue: 'en' },
        { provide: Session, useValue: { me: signal({ username: 'alex', server: 'nas.example.lan' }) } },
        {
          provide: Browser,
          useValue: { shares: signal([]), share: signal(null), loadShares: () => Promise.resolve() },
        },
        { provide: Prefs, useValue: { theme: signal('light'), toggleTheme: () => undefined } },
        { provide: Language, useValue: { other: { code: 'de', name: 'Deutsch' } } },
        { provide: Api, useValue: {} },
        { provide: Transfers, useValue: {} },
        { provide: Toasts, useValue: {} },
      ],
    });
    // The preview and the transfers panel bring services of their own and
    // have nothing to do with the logo.
    TestBed.overrideComponent(Shell, {
      set: { imports: [RouterOutlet, RouterLink, Icon, Mark], schemas: [NO_ERRORS_SCHEMA] },
    });
  });

  it('makes both logos real links to the shares', () => {
    const f = TestBed.createComponent(Shell);
    f.detectChanges();
    const links = [...(f.nativeElement as HTMLElement).querySelectorAll<HTMLAnchorElement>('a.brand')];
    expect(links.length).toBe(2);
    for (const a of links) {
      expect(a.getAttribute('href')).toBe('/');
      expect(a.getAttribute('aria-label')).toContain('ferry');
      expect(a.textContent).toContain('ferry');
    }
  });
});
