import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  LOCALE_ID,
  computed,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';
import { Api, Entry, ZipEntry } from '../core/api';
import { parseTable } from '../core/csv';
import { Actions } from '../core/actions';
import { Browser } from '../core/browser';
import { describe } from '../core/errors';
import { Format } from '../core/format';
import { Preloader, neighbours } from '../core/preloader';
import { hasThumbnail, isWorkbook, kindIcon, kindLabel, kindOf, previewKind } from '../core/kinds';
import { Icon } from '../ui/icon';
import { FileDetails } from '../ui/file-details';
import { Snapshot } from '../core/snapshot';
import { Versions } from '../ui/versions';
import { TextEditor } from '../ui/text-editor';
import { MAX_EDIT_BYTES } from '../core/text-file';
import { MAX_SUBTITLE_BYTES, decodeSubtitle, subtitlesFor, toWebVTT } from '../core/subtitles';

/**
 * The preview: a file of the folder over everything, with the files of the
 * folder in a strip below. Images, video, audio and PDF are shown by the
 * browser itself from the content URL — which the server sends sandboxed, so
 * nothing in a file runs in ferry's origin; text is shown as text.
 */
@Component({
  selector: 'f-preview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon, FileDetails, TextEditor],
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
        @if (editable(e)) {
          <button
            class="lb-btn"
            type="button"
            (click)="editing.set(true)"
            title="Edit"
            i18n-title="@@edit.open"
          >
            <f-icon name="pencil" [size]="20" />
          </button>
        }
        <button
          class="lb-btn"
          type="button"
          (click)="actions.download([e])"
          title="Download"
          i18n-title="@@act.download"
        >
          <f-icon name="download" [size]="20" />
        </button>
        @if (!snapshot.at()) {
          <button class="lb-btn" type="button" (click)="remove(e)" title="Delete" i18n-title="@@act.delete">
            <f-icon name="trash" [size]="20" />
          </button>
        }
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
            <img [src]="url()" [alt]="e.name" (load)="shown.set(url())" (error)="shown.set(url())" />
          }
          @case ('derived') {
            <!-- The 320-pixel thumbnail, at the size of the 640 one, until that
                 one has come: a photo's 640 can take seconds to make. -->
            <div class="lb-derived">
              @if (derivedSmall(); as s) {
                <img class="lb-small" [src]="s" alt="" [class.gone]="derivedShown() === derivedLarge()" />
              }
              <img
                class="lb-large"
                [src]="derivedLarge()"
                [alt]="e.name"
                [class.on]="derivedShown() === derivedLarge()"
                (load)="derivedShown.set(derivedLarge())"
              />
            </div>
          }
          @case ('video') {
            <video [src]="url()" controls preload="metadata" playsinline>
              @for (t of tracks(); track t.src) {
                <track kind="subtitles" [src]="t.src" [attr.srclang]="t.lang || null" [label]="t.label" />
              }
            </video>
          }
          @case ('audio') {
            <div class="lb-audio">
              @if (cover(); as c) {
                <img class="lb-cover" [src]="c" alt="" (error)="thumbFailed(e)" />
              }
              <audio [src]="url()" controls preload="metadata"></audio>
            </div>
          }
          @case ('pdf') {
            <iframe [src]="frameUrl()" [title]="e.name"></iframe>
          }
          @case ('table') {
            <div class="textview tableview" [class.loading]="text() === null">
              @if (text() === null) {
                <ng-container i18n="@@loading">Loading…</ng-container>
              } @else if (table(); as tb) {
                @if (sheetName(); as n) {
                  <p class="sheet-name">{{ n }}</p>
                }
                <table class="csv">
                  @for (row of tb.rows; track $index) {
                    <tr>
                      @for (cell of row; track $index) {
                        <td>{{ cell }}</td>
                      }
                    </tr>
                  }
                </table>
                @if (tb.more || truncated()) {
                  <p i18n="@@pv.tableMore">
                    … The rest of the table is not shown; download it to see it all.
                  </p>
                }
              }
            </div>
          }
          @case ('archive') {
            <div class="textview zipview" [class.loading]="zipEntries() === null">
              @if (zipError(); as err) {
                <p>{{ err }}</p>
              } @else if (zipEntries() === null) {
                <ng-container i18n="@@loading">Loading…</ng-container>
              } @else {
                <p class="zip-count">
                  <ng-container i18n="@@zip.count">{zipFiles().length, plural,
                    =1 {1 file}
                    other {{{ zipFiles().length }} files}
                  }</ng-container>
                </p>
                <ul class="zip-list">
                  @for (z of zipFiles(); track z.name) {
                    <li>
                      <span class="zip-name">{{ z.name }}</span>
                      <small>{{ format.size(z.size) }}</small>
                      @if (z.readable && browser.share(); as share) {
                        <a
                          class="btn"
                          [href]="api.zipEntryUrl(share, e.path, z.name, true)"
                          target="_blank"
                          rel="noopener"
                          i18n="@@zip.open"
                          >Open</a
                        >
                        <a
                          class="btn"
                          [href]="api.zipEntryUrl(share, e.path, z.name)"
                          [attr.download]="''"
                          i18n="@@zip.download"
                          >Download</a
                        >
                      } @else {
                        <small i18n="@@zip.unreadable"
                          >encrypted or packed in a way ferry does not unpack</small
                        >
                      }
                    </li>
                  }
                </ul>
              }
            </div>
          }
          @case ('document') {
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
          @case ('text') {
            @if (editing() && browser.share(); as share) {
              <f-text-editor [share]="share" [entry]="e" (done)="edited($event)" />
            } @else {
              <div class="textview" [class.loading]="text() === null">
                @if (text() === null) {
                  <ng-container i18n="@@loading">Loading…</ng-container>
                } @else {
                  {{ text() }}
                  @if (truncated()) {
                    <p i18n="@@pv.truncated">
                      … The rest of the file is not shown; download it to see it all.
                    </p>
                  }
                }
              </div>
            }
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
              <img loading="lazy" [src]="src" alt="" (error)="thumbFailed(f)" />
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
            @if (!snapshot.at()) {
              <button
                type="button"
                class="btn lb-versions"
                (click)="versions.show({ share, path: e.path, name: e.name, folder: false })"
              >
                <f-icon name="clock" [size]="16" /><span i18n="@@act.versions">Previous versions</span>
              </button>
            }
            <f-file-details [share]="share" [path]="e.path" [version]="e.version" />
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
  protected readonly snapshot = inject(Snapshot);
  protected readonly versions = inject(Versions);
  protected readonly api = inject(Api);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly locale = inject(LOCALE_ID);
  private readonly preloader = inject(Preloader);
  private readonly box = viewChild<ElementRef<HTMLElement>>('box');

  protected readonly info = signal(false);
  /** Whether the text shown is being edited. */
  protected readonly editing = signal(false);
  private readonly editor = viewChild(TextEditor);
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
  /** The URL of the image last shown, loaded or failed. */
  protected readonly shown = signal('');
  /** Files the server made no thumbnail of — recordings without a cover among them. */
  private readonly broken = signal<ReadonlySet<string>>(new Set());
  /** The server's thumbnails of a picture the browser cannot show itself. */
  protected readonly derivedLarge = computed(() => this.derived(640));
  protected readonly derivedSmall = computed(() => this.derived(320));
  /** The 640 last loaded: the small one is hidden once it has come. */
  protected readonly derivedShown = signal('');
  /** A recording's cover, large, above the player; none when it carries none. */
  protected readonly cover = computed(() => {
    const e = this.entry();
    const share = this.browser.share();
    if (!e || !share || this.kind() !== 'audio' || !hasThumbnail(e.name) || this.broken().has(e.path))
      return null;
    return this.api.thumbnailUrl(share, e.path, 640, e.version);
  });
  protected readonly text = signal<string | null>(null);
  /** A workbook's first sheet, as the server read it. */
  private readonly sheet = signal<{ name: string; rows: string[][]; truncated: boolean } | null>(null);
  protected readonly sheetName = computed(() => this.sheet()?.name ?? '');
  /** A CSV's or TSV's first rows, or a workbook's, as cells: text, shown as text. */
  protected readonly table = computed(() => {
    const t = this.text();
    const e = this.entry();
    const sh = this.sheet();
    // A sheet's rows end at their last value; a table needs them as wide as the widest.
    if (sh) {
      const width = Math.max(0, ...sh.rows.map((r) => r.length));
      return {
        rows: sh.rows.map((r) => [...r, ...Array<string>(width - r.length).fill('')]),
        more: sh.truncated,
      };
    }
    return t === null || !e ? null : parseTable(t, e.name.toLowerCase().endsWith('.tsv'));
  });
  /** A ZIP's entries, for the preview; folders are left out, their files carry the path. */
  protected readonly zipEntries = signal<ZipEntry[] | null>(null);
  protected readonly zipError = signal<string | null>(null);
  protected readonly zipFiles = computed(() => (this.zipEntries() ?? []).filter((z) => z.type === 'file'));
  protected readonly truncated = signal(false);
  /** The video's subtitles, as WebVTT behind blob URLs that go when it does. */
  protected readonly tracks = signal<{ src: string; lang: string; label: string }[]>([]);
  private opener: HTMLElement | null = null;

  constructor() {
    effect(() => {
      const e = this.entry();
      const share = this.browser.share();
      this.text.set(null);
      this.sheet.set(null);
      this.truncated.set(false);
      this.zipEntries.set(null);
      this.zipError.set(null);
      if (e && share && previewKind(e.name) === 'archive') {
        this.api.zip(share, e.path).then(
          (z) => this.entry() === e && this.zipEntries.set(z),
          (err) => this.entry() === e && this.zipError.set(describe(err)),
        );
      }
      if (e && share && previewKind(e.name) === 'document') {
        this.api.documentText(share, e.path).then(
          (r) => {
            if (this.entry() === e) {
              this.text.set(r.text);
              this.truncated.set(r.truncated);
            }
          },
          (err) => this.entry() === e && this.text.set(describe(err)),
        );
      }
      if (e && share && isWorkbook(e.name)) {
        this.api.sheet(share, e.path).then(
          (sh) => {
            if (this.entry() === e) {
              this.sheet.set(sh);
              this.text.set('');
            }
          },
          (err) => this.entry() === e && this.text.set(describe(err)),
        );
      } else if (e && share && (previewKind(e.name) === 'text' || previewKind(e.name) === 'table')) {
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
    // Subtitle files next to a video become its tracks. They are read at the
    // snapshot the video is (Api.head), and only the browser's cue renderer
    // ever sees their text.
    effect((onCleanup) => {
      const e = this.entry();
      const share = this.browser.share();
      const urls: string[] = [];
      onCleanup(() => {
        urls.forEach((u) => URL.revokeObjectURL(u));
        this.tracks.set([]);
      });
      if (!e || !share || this.kind() !== 'video') return;
      // All of the folder's files, not only those a filter shows.
      const sizes = new Map(
        (this.browser.entries() ?? []).filter((f) => f.type === 'file').map((f) => [f.name, f.size]),
      );
      const subs = subtitlesFor(e.name, [...sizes.keys()], this.locale).filter(
        (s) => (sizes.get(s.name) ?? 0) <= MAX_SUBTITLE_BYTES,
      );
      const dir = e.path.slice(0, e.path.length - e.name.length);
      let live = true;
      onCleanup(() => (live = false));
      void Promise.all(
        subs.map((s) =>
          this.api.head(share, dir + s.name, MAX_SUBTITLE_BYTES).then(
            (r) => (r.truncated ? null : { s, text: toWebVTT(decodeSubtitle(r.bytes), s.name) }),
            () => null,
          ),
        ),
      ).then((read) => {
        if (!live) return;
        const tracks = read
          .filter((r) => r !== null)
          .map((r) => {
            const src = URL.createObjectURL(new Blob([r.text], { type: 'text/vtt' }));
            urls.push(src);
            return { src, lang: r.s.lang, label: r.s.label };
          });
        this.tracks.set(tracks);
      });
    });
    // The next and the previous image load ahead — after the one shown, so
    // that they do not hold it up; the preview closed, they are let go.
    effect(() => {
      const e = this.entry();
      const share = this.browser.share();
      if (!e || !share) {
        this.preloader.want([]);
        return;
      }
      if (this.kind() === 'image' && this.shown() !== this.url()) return;
      this.preloader.want(
        neighbours(this.files(), this.index()).map((f) => this.api.contentUrl(share, f.path, true)),
      );
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

  private derived(size: 320 | 640): string {
    const e = this.entry();
    const share = this.browser.share();
    return e && share && this.kind() === 'derived'
      ? this.api.thumbnailUrl(share, e.path, size, e.version)
      : '';
  }

  protected thumb(e: Entry): string | null {
    const share = this.browser.share();
    return share && hasThumbnail(e.name) && !this.broken().has(e.path)
      ? this.api.thumbnailUrl(share, e.path, 160, e.version)
      : null;
  }

  protected thumbFailed(e: Entry): void {
    this.broken.update((s) => new Set(s).add(e.path));
  }

  /** Whether the file can be edited here: text, small enough, the share as it is now. */
  protected editable(e: Entry): boolean {
    return previewKind(e.name) === 'text' && e.size <= MAX_EDIT_BYTES && !this.snapshot.at();
  }

  /** The editor closed; what it saved is shown, and the folder knows. */
  protected edited(saved: Entry | null): void {
    this.editing.set(false);
    if (saved) void this.browser.reload();
  }

  /** Leaves the editor first, asking about unsaved text; false when the person stays. */
  private async leaveEditor(): Promise<boolean> {
    const ed = this.editor();
    return !this.editing() || !ed || ed.leave();
  }

  protected async step(d: number): Promise<void> {
    if (!(await this.leaveEditor())) return;
    this.editing.set(false);
    const n = this.files().length;
    if (n) this.browser.preview.set((this.index() + d + n) % n);
  }

  protected async close(): Promise<void> {
    if (!(await this.leaveEditor())) return;
    this.editing.set(false);
    this.browser.preview.set(null);
    this.opener?.focus();
    this.opener = null;
  }

  protected async remove(e: Entry): Promise<void> {
    if (this.snapshot.at()) return;
    if (await this.actions.remove([e])) {
      const n = this.files().length;
      if (!n) this.close();
      else if (this.index() >= n) this.browser.preview.set(n - 1);
    }
  }

  protected key(ev: KeyboardEvent): void {
    if (this.browser.preview() === null || document.querySelector('dialog[open]')) return;
    const media = /VIDEO|AUDIO/.test((document.activeElement as HTMLElement | null)?.tagName ?? '');
    const typing = this.editing() && (document.activeElement as HTMLElement | null)?.tagName === 'TEXTAREA';
    if (ev.key === 'Escape' && !typing) void this.close();
    else if (typing) return;
    else if (ev.key === 'ArrowRight' && !media) void this.step(1);
    else if (ev.key === 'ArrowLeft' && !media) void this.step(-1);
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
