import { PlayerBar } from '../ui/player-bar';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { Api } from '../core/api';
import { Browser } from '../core/browser';
import { shareIcon } from '../core/kinds';
import { Language } from '../core/lang';
import { Prefs } from '../core/prefs';
import { Session } from '../core/session';
import { Transfers } from '../core/transfers';
import { Icon } from '../ui/icon';
import { Mark } from '../ui/mark';
import { Toasts } from '../ui/toast';
import { Preview } from './preview';
import { TransfersPanel } from './transfers-panel';

/** The frame once logged in: the shares on the left, the page on the right. */
@Component({
  selector: 'f-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, RouterLink, Icon, Mark, Preview, TransfersPanel, PlayerBar],
  host: { '(document:keydown.escape)': 'drawer.set(false)' },
  template: `<div class="app" [class.drawer]="drawer()">
      <aside class="side" aria-label="Shares" i18n-aria-label="@@shares">
        <a
          class="brand"
          routerLink="/"
          (click)="drawer.set(false)"
          aria-label="ferry — all shares"
          i18n-aria-label="@@brand.home"
          ><f-mark [size]="30" /><span>ferry</span></a
        >
        <h3 i18n="@@shares">Shares</h3>
        <nav class="shares-nav">
          @for (s of browser.shares() ?? []; track s.name) {
            <a
              class="nav"
              [class.on]="browser.share() === s.name"
              [routerLink]="['/browse', s.name]"
              (click)="drawer.set(false)"
              [attr.aria-current]="browser.share() === s.name ? 'page' : null"
            >
              <f-icon [name]="icon(s.name)" [size]="19" />{{ s.name }}
            </a>
          }
        </nav>
        <div class="grow"></div>
        <button
          class="nav"
          type="button"
          (click)="lang.switch(lang.other.code)"
          [attr.lang]="lang.other.code"
        >
          <f-icon name="globe" [size]="19" />{{ lang.other.name }}
        </button>
        <button class="nav" type="button" (click)="prefs.toggleTheme()">
          <f-icon [name]="prefs.theme() === 'dark' ? 'sun' : 'moon'" [size]="19" />
          @if (prefs.theme() === 'dark') {
            <ng-container i18n="@@theme.light">Light</ng-container>
          } @else {
            <ng-container i18n="@@theme.dark">Dark</ng-container>
          }
        </button>
        <div class="me">
          <span class="avatar" aria-hidden="true">{{ initial() }}</span>
          <div>
            <b>{{ session.me()?.username }}</b><small>{{ session.me()?.server }}</small>
          </div>
          <button class="icon-btn" type="button" (click)="logout()" title="Sign out" i18n-title="@@logout">
            <f-icon name="logout" />
          </button>
        </div>
      </aside>
      <div class="drawer-scrim" (click)="drawer.set(false)"></div>
      <main class="main">
        <div class="mob-top">
          <button
            class="icon-btn"
            type="button"
            (click)="drawer.set(true)"
            aria-label="Menu"
            i18n-aria-label="@@menu"
          >
            <f-icon name="menu" [size]="22" />
          </button>
          <a class="brand" routerLink="/" aria-label="ferry — all shares" i18n-aria-label="@@brand.home"
            ><f-mark [size]="24" /><span>ferry</span></a
          >
        </div>
        <router-outlet />
      </main>
    </div>
    <f-player />
    <f-transfers />
    <f-preview />`,
})
export class Shell {
  protected readonly session = inject(Session);
  protected readonly browser = inject(Browser);
  protected readonly prefs = inject(Prefs);
  protected readonly lang = inject(Language);
  private readonly api = inject(Api);
  private readonly transfers = inject(Transfers);
  private readonly toasts = inject(Toasts);
  private readonly router = inject(Router);

  protected readonly drawer = signal(false);
  protected readonly initial = computed(() => (this.session.me()?.username ?? '?').slice(0, 1).toUpperCase());

  constructor() {
    void this.browser.loadShares();
    this.router.events.subscribe((e) => {
      if (e instanceof NavigationEnd) {
        this.drawer.set(false);
      }
    });
  }

  protected icon(name: string): string {
    return shareIcon(name, this.session.me()?.username);
  }

  protected async logout(): Promise<void> {
    this.transfers.cancelAll();
    try {
      await this.api.logout();
    } catch {
      // The session ends here either way; the server's ends with its timeout.
    }
    this.browser.reset();
    this.session.ended('logout');
    this.toasts.show($localize`:@@toast.loggedOut:Signed out. The SMB session has ended.`);
  }
}
