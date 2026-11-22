import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';
import { Api, Entry } from '../core/api';
import { Actions } from '../core/actions';
import { Browser } from '../core/browser';
import { describe } from '../core/errors';
import { Format } from '../core/format';
import { hasThumbnail, kindIcon, kindLabel, kindOf, previewKind } from '../core/kinds';
import { Icon } from '../ui/icon';
import { FileDetails } from '../ui/file-details';

/**
 * The preview: a file of the folder over everything, with the files of the
 * folder in a strip below. Images, video, audio and PDF are shown by the
 * browser itself from the content URL — which the server sends sandboxed, so
 * nothing in a file runs in ferry's origin; text is shown as text.
 */
@Component({
  selector: 'f-preview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon, FileDetails],
  host: { '(document:keydown)': 'key($event)' },
  template: `@if (entry(); as e) {
    <div
      #box
      class="lightbox"
      [class.info]="info()"
      role="dialog"
      aria-modal="true"
      [attr.aria-label]="e.name"
      tabindex="-1"
    >
      <div class="lb-top">
        <div class="t">
          <b>{{ e.name }}</b><small
            ><ng-container i18n="@@pv.count">{{ index() + 1 }} of {{ files().length }}</ng-container> ·
            {{ format.size(e.size) }}</small
          >
        </div>
        <button
          class="lb-btn"
          type="button"
          (click)="actions.download([e])"
          title="Download"
          i18n-title="@@act.download"
        >
          <f-icon name="download" [size]="20" />
        </button>
        <button class="lb-btn" type="button" (click)="remove(e)" title="Delete" i18n-title="@@act.delete">
          <f-icon name="trash" [size]="20" />
        </button>
        <button
          class="lb-btn"
          type="button"
          (click)="info.set(!info())"
          title="Details (i)"
          i18n-title="@@pv.details"
          [attr.aria-pressed]="info()"
        >
          <f-icon name="info" [size]="20" />
        </button>
        <button class="lb-btn" type="button" (click)="close()" title="Close (Esc)" i18n-title="@@pv.close">
          <f-icon name="x" [size]="22" />
        </button>
      </div>
      <div class="lb-stage">
        @switch (kind()) {
          @case ('image') {
            <img [src]="url()" [alt]="e.name" />
          }
          @case ('video') {
            <video [src]="url()" controls preload="metadata" playsinline></video>
          }
          @case ('audio') {
            <audio [src]="url()" controls preload="metadata"></audio>
          }
          @case ('pdf') {
            <iframe [src]="frameUrl()" [title]="e.name"></iframe>
          }
          @case ('text') {
            <div class="textview" [class.loading]="text() === null">
              @if (text() === null) {
                <ng-container i18n="@@loading">Loading…</ng-container>
              } @else {
                {{ text() }}
                @if (truncated()) {
                  <p i18n="@@pv.truncated">… The rest of the file is not shown; download it to see it all.</p>
                }
              }
            </div>
          }
          @default {
            <div class="noprev">
              <f-icon [name]="icon(e)" [size]="56" [stroke]="1.2" />
              <b i18n="@@pv.none">No preview</b>{{ label(e) }} · {{ format.size(e.size) }}<br />
              <button class="btn" type="button" (click)="actions.download([e])">
                <f-icon name="download" [size]="17" /><ng-container i18n="@@act.download"
                  >Download</ng-container
                >
              </button>
            </div>
          }
        }
        @if (files().length > 1) {
          <button
            class="lb-nav l"
            type="button"
            (click)="step(-1)"
            aria-label="Previous"
            i18n-aria-label="@@pv.prev"
          >
            <f-icon name="left" [size]="26" />
          </button>
          <button
            class="lb-nav r"
            type="button"
            (click)="step(1)"
            aria-label="Next"
            i18n-aria-label="@@pv.next"
          >
            <f-icon name="right" [size]="26" />
          </button>
        }
      </div>
      <div class="strip">
        @for (f of files(); track f.name; let j = $index) {
          <button type="button" [class.on]="j === index()" (click)="browser.preview.set(j)" [title]="f.name">
            @if (thumb(f); as src) {
              <img loading="lazy" [src]="src" alt="" />
            } @else {
              <span class="k"><f-icon [name]="icon(f)" /></span>
            }
          </button>
        }
      </div>
      @if (info()) {
        <aside class="lb-info">
          <h4 i18n="@@info.title">Details</h4>
          <dl>
            <dt i18n="@@info.name">Name</dt>
            <dd>{{ e.name }}</dd>
            <dt i18n="@@info.kind">Type</dt>
            <dd>{{ label(e) }}</dd>
            <dt i18n="@@info.size">Size</dt>
            <dd>{{ format.size(e.size) }}</dd>
            <dt i18n="@@info.mod">Modified</dt>
            <dd>{{ format.dateTime(e.modified) }}</dd>
            <dt i18n="@@info.where">Location</dt>
            <dd>{{ where() }}</dd>
          </dl>
          @if (browser.share(); as share) {
            <f-file-details [share]="share" [path]="e.path" />
          }
        </aside>
      }
    </div>
  }`,
})
export class Preview {
  protected readonly browser = inject(Browser);
  protected readonly actions = inject(Actions);
  protected readonly format = inject(Format);
  private readonly api = inject(Api);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly box = viewChild<ElementRef<HTMLElement>>('box');

  protected readonly info = signal(false);
  protected readonly files = this.browser.files;
  protected readonly index = computed(() => this.browser.preview() ?? 0);
  protected readonly entry = computed(() => {
    const i = this.browser.preview();
    return i === null ? null : (this.files()[i] ?? null);
  });
  protected readonly kind = computed(() => (this.entry() ? previewKind(this.entry()!.name) : 'none'));
  protected readonly url = computed(() => {
    const e = this.entry();
    const share = this.browser.share();
    return e && share ? this.api.contentUrl(share, e.path, true) : '';
  });
  // The PDF viewer needs a frame; the URL is ferry's own content endpoint.
  protected readonly frameUrl = computed<SafeResourceUrl>(() =>
    this.sanitizer.bypassSecurityTrustResourceUrl(this.url()),
  );
  protected readonly where = computed(() =>
    [this.browser.share(), ...(this.browser.path() ? this.browser.path().split('/') : [])].join(' / '),
  );
  protected readonly text = signal<string | null>(null);
  protected readonly truncated = signal(false);
  private opener: HTMLElement | null = null;

  constructor() {
    effect(() => {
      const e = this.entry();
      const share = this.browser.share();
      this.text.set(null);
      this.truncated.set(false);
      if (e && share && previewKind(e.name) === 'text') {
        this.api.text(share, e.path).then(
          (r) => {
            if (this.entry() === e) {
              this.text.set(r.text);
              this.truncated.set(r.truncated);
            }
          },
          (err) => this.entry() === e && this.text.set(describe(err)),
        );
      }
    });
    // Focus moves into the preview while it is open and back afterwards.
    effect(() => {
      const open = this.entry() !== null;
      document.body.classList.toggle('fx-locked', open);
      const el = this.box()?.nativeElement;
      if (open && el && document.activeElement && !el.contains(document.activeElement)) {
        this.opener = document.activeElement as HTMLElement;
        el.focus();
      }
    });
  }

  protected icon(e: Entry): string {
    return kindIcon[kindOf(e.name, false)];
  }

  protected label(e: Entry): string {
    return kindLabel(kindOf(e.name, false));
  }

  protected thumb(e: Entry): string | null {
    const share = this.browser.share();
    return share && hasThumbnail(e.name) ? this.api.thumbnailUrl(share, e.path, 160) : null;
  }

  protected step(d: number): void {
    const n = this.files().length;
    if (n) this.browser.preview.set((this.index() + d + n) % n);
  }

  protected close(): void {
    this.browser.preview.set(null);
    this.opener?.focus();
    this.opener = null;
  }

  protected async remove(e: Entry): Promise<void> {
    if (await this.actions.remove([e])) {
      const n = this.files().length;
      if (!n) this.close();
      else if (this.index() >= n) this.browser.preview.set(n - 1);
    }
  }

  protected key(ev: KeyboardEvent): void {
    if (this.browser.preview() === null || document.querySelector('dialog[open]')) return;
    const media = /VIDEO|AUDIO/.test((document.activeElement as HTMLElement | null)?.tagName ?? '');
    if (ev.key === 'Escape') this.close();
    else if (ev.key === 'ArrowRight' && !media) this.step(1);
    else if (ev.key === 'ArrowLeft' && !media) this.step(-1);
    else if (ev.key === 'i') this.info.update((v) => !v);
    else if (ev.key === 'Tab') this.trapFocus(ev);
  }

  /** Keeps Tab inside the preview while it is open. */
  private trapFocus(ev: KeyboardEvent): void {
    const el = this.box()?.nativeElement;
    if (!el) return;
    const focusable = Array.from(
      el.querySelectorAll<HTMLElement>('button, video, audio, iframe, [tabindex]:not([tabindex="-1"])'),
    );
    if (!focusable.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (ev.shiftKey && document.activeElement === first) {
      ev.preventDefault();
      last.focus();
    } else if (!ev.shiftKey && document.activeElement === last) {
      ev.preventDefault();
      first.focus();
    }
  }
}
