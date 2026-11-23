import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  Injectable,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { Router } from '@angular/router';
import { Actions } from '../core/actions';
import { Api, Version } from '../core/api';
import { describe } from '../core/errors';
import { Format } from '../core/format';
import { tokenTime } from '../core/snapshot';
import { Icon } from './icon';

/** Whose versions are shown: a file or a folder of a share. */
export interface VersionsOf {
  share: string;
  path: string;
  name: string;
  folder: boolean;
}

/**
 * The previous versions of one file or folder, in a dialog: what the
 * server's snapshots hold of it, newest first, by time and size — not who
 * changed it. A file's version opens or downloads; a folder's opens as it
 * was, read-only. A server without snapshots has none, and says so.
 */
@Injectable({ providedIn: 'root' })
export class Versions {
  readonly current = signal<VersionsOf | null>(null);

  show(of: VersionsOf): void {
    this.current.set(of);
  }

  close(): void {
    this.current.set(null);
  }
}

@Component({
  selector: 'f-versions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  template: `@if (versions.current(); as of) {
    <dialog
      #dlg
      class="fx-modal fx-versions"
      aria-modal="true"
      (cancel)="$event.preventDefault(); versions.close()"
    >
      <h2 class="fx-modal-title" i18n="@@versions.title">Previous versions of {{ of.name }}</h2>
      @if (error(); as err) {
        <p class="fx-modal-error" role="alert">{{ err }}</p>
      } @else if (list() === null) {
        <p class="fx-modal-text" i18n="@@versions.loading">Looking through the snapshots…</p>
      } @else if (!list()!.length) {
        <p class="fx-modal-text" i18n="@@versions.none">
          The server keeps no earlier state of this, or none that differs from now.
        </p>
      } @else {
        <ul class="fx-versions-list">
          @for (v of list(); track v.at) {
            <li>
              <span class="when">{{ when(v) }}</span>
              <span class="what">{{ of.folder ? '' : format.size(v.size) }}</span>
              <button
                type="button"
                class="fx-btn"
                (click)="restore(of, v)"
                title="Restore as a copy beside it"
                i18n-title="@@versions.restoreTitle"
              >
                <f-icon name="copy" [size]="16" /><span i18n="@@versions.restore">Restore as copy</span>
              </button>
              @if (of.folder) {
                <button type="button" class="fx-btn" (click)="browse(of, v)">
                  <f-icon name="folder" [size]="16" /><span i18n="@@versions.browse">Open as it was</span>
                </button>
              } @else {
                <a class="fx-btn" [href]="url(of, v, true)" target="_blank" rel="noopener">
                  <f-icon name="eye" [size]="16" /><span i18n="@@versions.open">Open</span>
                </a>
                <a class="fx-btn" [href]="url(of, v, false)" [attr.download]="of.name">
                  <f-icon name="download" [size]="16" /><span i18n="@@versions.download">Download</span>
                </a>
              }
            </li>
          }
        </ul>
        @if (truncated()) {
          <p class="fx-modal-text" i18n="@@versions.truncated">
            Only the newest snapshots were looked through; there may be older versions.
          </p>
        }
      }
      <div class="fx-modal-actions">
        <button type="button" class="fx-btn" (click)="versions.close()" i18n="@@versions.close">Close</button>
      </div>
    </dialog>
  }`,
})
export class VersionsHost {
  protected readonly versions = inject(Versions);
  protected readonly format = inject(Format);
  private readonly api = inject(Api);
  private readonly router = inject(Router);
  private readonly actions = inject(Actions);
  protected readonly list = signal<Version[] | null>(null);
  protected readonly truncated = signal(false);
  protected readonly error = signal<string | null>(null);
  private readonly dlg = viewChild<ElementRef<HTMLDialogElement>>('dlg');
  private asked = 0;

  constructor() {
    effect(() => {
      const of = this.versions.current();
      const el = this.dlg()?.nativeElement;
      if (!of || !el) return;
      if (!el.open) el.showModal();
    });
    effect(() => {
      const of = this.versions.current();
      const n = ++this.asked;
      this.list.set(null);
      this.truncated.set(false);
      this.error.set(null);
      if (!of) return;
      this.api.versions(of.share, of.path).then(
        (r) => {
          if (n !== this.asked) return;
          this.list.set(r.versions);
          this.truncated.set(r.truncated);
        },
        (e) => n === this.asked && this.error.set(describe(e)),
      );
    });
  }

  /** When the snapshot was taken, in the viewer's time. */
  protected when(v: Version): string {
    const t = tokenTime(v.at);
    return t ? this.format.dateTime(t.toISOString()) : v.at;
  }

  protected url(of: VersionsOf, v: Version, inline: boolean): string {
    return this.api.contentUrl(of.share, of.path, inline, v.at);
  }

  /** The version copied beside the original, named after its time. */
  protected restore(of: VersionsOf, v: Version): void {
    this.versions.close();
    const t = tokenTime(v.at);
    const when = t
      ? t
          .toLocaleString(undefined, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
          })
          .replace(/[/:]/g, '.')
          .replace(',', '')
      : v.at;
    void this.actions.restore(of.share, of.path, v.at, when);
  }

  /** A folder as the snapshot holds it, read-only, in the folder view. */
  protected browse(of: VersionsOf, v: Version): void {
    this.versions.close();
    const queryParams: Record<string, string> = { at: v.at };
    if (of.path) queryParams['path'] = of.path;
    void this.router.navigate(['/browse', of.share], { queryParams });
  }
}
