import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Api } from '../core/api';
import { describe } from '../core/errors';
import { Language } from '../core/lang';
import { Prefs } from '../core/prefs';
import { Session } from '../core/session';
import { Icon } from '../ui/icon';
import { Mark } from '../ui/mark';

/** A wave of the harbour: a path across twice the width, drifted by CSS. */
function wave(y: number, amp: number): string {
  let d = `M0 ${y}`;
  for (let x = 0; x < 2400; x += 300) {
    d += ` q75 ${-amp} 150 0 t150 0`;
  }
  return `${d} V230 H0Z`;
}

/**
 * The login: one card on the harbour. The password goes to the server once,
 * in the login request, and is cleared from the form right after.
 */
@Component({
  selector: 'f-login',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, Icon, Mark],
  template: `<main class="login">
    <div class="corner">
      <div class="langs" role="group" aria-label="Language" i18n-aria-label="@@lang.label">
        <button
          type="button"
          [attr.aria-pressed]="lang.current === 'en'"
          (click)="lang.switch('en')"
          lang="en"
        >
          EN
        </button>
        <button
          type="button"
          [attr.aria-pressed]="lang.current === 'de'"
          (click)="lang.switch('de')"
          lang="de"
        >
          DE
        </button>
      </div>
      <button type="button" (click)="prefs.toggleTheme()" title="Light or dark" i18n-title="@@theme.toggle">
        <f-icon [name]="prefs.theme() === 'dark' ? 'sun' : 'moon'" [size]="17" />
      </button>
    </div>
    <div class="brand"><f-mark [size]="38" /> ferry</div>
    <p class="tagline" i18n="@@login.tagline">Your files from the NAS, in the browser.</p>
    <form class="login-card" (ngSubmit)="submit()" autocomplete="on">
      <h2 i18n="@@login.title">Sign in</h2>
      <p class="sub" i18n="@@login.sub">
        with your account on <b>{{ server() }}</b>
      </p>
      @if (message(); as m) {
        <div class="error" role="alert">
          <f-icon name="info" /><span>{{ m }}</span>
        </div>
      }
      <label class="field">
        <span i18n="@@login.user">Username</span>
        <div class="input">
          <f-icon name="user" />
          <input
            name="username"
            [(ngModel)]="username"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            required
          />
        </div>
      </label>
      <label class="field">
        <span i18n="@@login.pass">Password</span>
        <div class="input">
          <f-icon name="lock" />
          <input
            name="password"
            [(ngModel)]="password"
            [type]="showPassword() ? 'text' : 'password'"
            autocomplete="current-password"
            placeholder="Samba password"
            i18n-placeholder="@@login.passPh"
            required
          />
          <button
            type="button"
            class="eye"
            (click)="showPassword.set(!showPassword())"
            [attr.aria-pressed]="showPassword()"
            aria-label="Show password"
            i18n-aria-label="@@login.show"
          >
            <f-icon name="eye" />
          </button>
        </div>
      </label>
      <button class="btn-big" type="submit" [disabled]="busy()">
        <ng-container i18n="@@login.submit">Sign in</ng-container> <f-icon name="right" [stroke]="2" />
      </button>
    </form>
    <p class="note">
      <f-icon name="lock" [size]="15" />
      <span i18n="@@login.trust"
        >ferry does not store your password. The NAS checks it when you sign in; after
        {{ idleMinutes() }} minutes without activity you are signed out.</span
      >
    </p>
    <div class="sea" aria-hidden="true">
      <svg class="w1" viewBox="0 0 2400 230" preserveAspectRatio="none">
        <path [attr.d]="waves[0]" fill="#ffffff0f" />
      </svg>
      <svg class="w2" viewBox="0 0 2400 230" preserveAspectRatio="none">
        <path [attr.d]="waves[1]" fill="#ffffff14" />
      </svg>
      <svg class="w3" viewBox="0 0 2400 230" preserveAspectRatio="none">
        <path [attr.d]="waves[2]" fill="#0b4a47" />
      </svg>
    </div>
    <span class="boat" aria-hidden="true"><f-mark [size]="54" /></span>
  </main>`,
})
export class Login {
  private readonly api = inject(Api);
  private readonly session = inject(Session);
  protected readonly prefs = inject(Prefs);
  protected readonly lang = inject(Language);

  protected readonly waves = [wave(70, 14), wave(110, 12), wave(150, 10)];
  protected username = '';
  protected password = '';
  protected readonly showPassword = signal(false);
  protected readonly busy = signal(false);
  private readonly failure = signal<string | null>(null);

  protected readonly server = computed(() => this.session.info()?.server ?? 'NAS');
  protected readonly idleMinutes = computed(() =>
    Math.round((this.session.info()?.idleTimeoutSeconds ?? 1800) / 60),
  );

  /** Why the person is here, unless the last attempt failed. */
  protected readonly message = computed(() => {
    const f = this.failure();
    if (f) return f;
    switch (this.session.reason()) {
      case 'expired':
        return $localize`:@@login.expired:You were signed out after a while without activity. Please sign in again.`;
      case 'lost':
        return $localize`:@@login.lost:The connection to the NAS was lost. Please sign in again.`;
      default:
        return null;
    }
  });

  protected async submit(): Promise<void> {
    const user = this.username.trim();
    if (!user || !this.password || this.busy()) {
      return;
    }
    this.busy.set(true);
    this.failure.set(null);
    try {
      const me = await this.api.login(user, this.password);
      this.password = '';
      this.session.started(me);
    } catch (e) {
      this.password = '';
      const code = (e as { code?: string }).code;
      this.failure.set(
        code === 'login_failed'
          ? $localize`:@@login.rejected:The NAS rejected the sign-in. The username or password is wrong.`
          : describe(e),
      );
    } finally {
      this.busy.set(false);
    }
  }
}
