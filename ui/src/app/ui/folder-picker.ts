import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  Injectable,
  computed,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { Api, Entry, join } from '../core/api';
import { describe } from '../core/errors';
import { Format } from '../core/format';
import { Icon } from './icon';

interface Pick {
  share: string;
  /** Folders that cannot be the target: the ones being moved, and what is inside them. */
  excluded: string[];
  count: number;
  /** Copying rather than moving: only what the dialog says differs. */
  copy: boolean;
  resolve: (path: string | null) => void;
}

/** Asks for a folder of a share to move or copy entries to. */
@Injectable({ providedIn: 'root' })
export class FolderPicker {
  readonly current = signal<Pick | null>(null);
  readonly start = signal('');

  pick(
    share: string,
    start: string,
    excluded: string[],
    count: number,
    copy = false,
  ): Promise<string | null> {
    this.current()?.resolve(null);
    this.start.set(start);
    return new Promise((resolve) => this.current.set({ share, excluded, count, copy, resolve }));
  }

  close(path: string | null): void {
    const c = this.current();
    this.current.set(null);
    c?.resolve(path);
  }
}

@Component({
  selector: 'f-folder-picker',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  template: `@if (picker.current(); as p) {
    <dialog #dlg class="fx-modal" aria-modal="true" (cancel)="$event.preventDefault(); picker.close(null)">
      <h2 class="fx-modal-title">
        @if (p.copy && p.count === 1) {
          <ng-container i18n="@@picker.copy1">Copy to …</ng-container>
        } @else if (p.copy) {
          <ng-container i18n="@@picker.copyN">Copy {{ p.count }} items to …</ng-container>
        } @else if (p.count === 1) {
          <ng-container i18n="@@picker.title1">Move to …</ng-container>
        } @else {
          <ng-container i18n="@@picker.titleN">Move {{ p.count }} items to …</ng-container>
        }
      </h2>
      <div class="picker">
        <nav class="crumbs">
          <button type="button" (click)="go('')">{{ p.share }}</button>
          @for (c of crumbs(); track c.path) {
            <f-icon name="right" [size]="14" /><button type="button" (click)="go(c.path)">
              {{ c.name }}
            </button>
          }
        </nav>
        @if (error(); as err) {
          <p class="fx-modal-error">{{ err }}</p>
        } @else if (folders(); as list) {
          @for (f of list; track f.path) {
            <button type="button" class="nav" [disabled]="blocked(f.path)" (click)="go(f.path)">
              <f-icon name="folder" [size]="19" />{{ f.name }}
            </button>
          } @empty {
            <p class="fx-modal-text" i18n="@@picker.empty">No folders in here.</p>
          }
        } @else {
          <p class="loading" i18n="@@loading">Loading…</p>
        }
      </div>
      <div class="fx-modal-actions">
        <button type="button" class="fx-btn" (click)="picker.close(null)" i18n="@@dialog.cancel">
          Cancel
        </button>
        <button
          type="button"
          class="fx-btn fx-btn-primary"
          [disabled]="blocked(path())"
          (click)="picker.close(path())"
        >
          @if (p.copy) {
            <ng-container i18n="@@picker.copyHere">Copy here</ng-container>
          } @else {
            <ng-container i18n="@@picker.here">Move here</ng-container>
          }
        </button>
      </div>
    </dialog>
  }`,
})
export class FolderPickerHost {
  protected readonly picker = inject(FolderPicker);
  private readonly api = inject(Api);
  private readonly format = inject(Format);
  private readonly dlg = viewChild<ElementRef<HTMLDialogElement>>('dlg');

  protected readonly path = signal('');
  protected readonly folders = signal<Entry[] | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly crumbs = computed(() => {
    const parts = this.path() ? this.path().split('/') : [];
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
  });

  constructor() {
    effect(() => {
      const p = this.picker.current();
      const el = this.dlg()?.nativeElement;
      if (p && el && !el.open) {
        el.showModal();
        void this.go(this.picker.start());
      }
    });
  }

  protected blocked(path: string): boolean {
    return (this.picker.current()?.excluded ?? []).some((x) => path === x || path.startsWith(x + '/'));
  }

  protected async go(path: string): Promise<void> {
    const p = this.picker.current();
    if (!p) return;
    this.path.set(path);
    this.folders.set(null);
    this.error.set(null);
    try {
      const list = await this.api.entries(p.share, path);
      this.folders.set(
        list.entries
          .filter((e) => e.type === 'folder')
          .map((e) => ({ ...e, path: join(path, e.name) }))
          .sort((a, b) => this.format.compare(a.name, b.name)),
      );
    } catch (e) {
      this.error.set(describe(e));
    }
  }
}
