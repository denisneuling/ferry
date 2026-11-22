import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Browser } from '../core/browser';
import { shareIcon } from '../core/kinds';
import { Session } from '../core/session';
import { Icon } from '../ui/icon';
import { Space } from '../ui/space';

/** The start: a greeting and the shares, as cards. */
@Component({
  selector: 'f-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Icon, Space],
  template: `<header class="top">
      <div class="heading">
        <h1>{{ greeting() }}</h1>
        <div class="meta" i18n="@@home.sub">What would you like to open?</div>
      </div>
    </header>
    <section class="content">
      @if (browser.sharesError(); as err) {
        <div class="error-pane">
          <f-icon name="drive" [size]="48" [stroke]="1.3" />
          <b i18n="@@home.noShares">The shares cannot be shown</b>{{ err }}<br />
          <button class="btn" type="button" (click)="browser.loadShares()" i18n="@@retry">Try again</button>
        </div>
      } @else if (browser.shares(); as shares) {
        @if (shares.length) {
          <div class="shares">
            @for (s of shares; track s.name) {
              <a class="share" [routerLink]="['/browse', s.name]">
                <span class="si"><f-icon [name]="icon(s.name)" [size]="22" /></span>
                <span
                  ><b>{{ s.name }}</b
                  ><small>{{ s.comment }}</small
                  ><small class="share-space"><f-space [share]="s.name" /></small
                ></span>
              </a>
            }
          </div>
        } @else {
          <div class="empty">
            <f-icon name="drive" [size]="56" [stroke]="1.2" />
            <b i18n="@@home.none">No shares</b>
            <ng-container i18n="@@home.noneText">The NAS offers you no shares here.</ng-container>
          </div>
        }
      } @else {
        <p class="loading" i18n="@@loading">Loading…</p>
      }
    </section>`,
})
export class Home {
  protected readonly browser = inject(Browser);
  private readonly session = inject(Session);

  constructor() {
    this.browser.leave();
  }

  protected readonly greeting = computed(() => {
    const name = this.session.me()?.username ?? '';
    const h = new Date().getHours();
    if (h < 11) return $localize`:@@hello.morning:Good morning, ${name}:name:`;
    if (h < 18) return $localize`:@@hello.day:Hello, ${name}:name:`;
    return $localize`:@@hello.evening:Good evening, ${name}:name:`;
  });

  protected icon(name: string): string {
    return shareIcon(name, this.session.me()?.username);
  }
}
