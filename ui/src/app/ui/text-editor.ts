import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { Api, ApiError, Entry } from '../core/api';
import { describe } from '../core/errors';
import { Format } from '../core/format';
import { MAX_EDIT_BYTES, TextFile, decodeText, encodeText } from '../core/text-file';
import { Dialogs } from './dialog';
import { Toasts } from './toast';

/**
 * A text file edited in the preview: read whole (1 MiB at most, UTF-8
 * only), saved over the version that was opened. When someone changed the
 * file in the meantime the save is refused, and the person decides: keep
 * their text and overwrite, or leave it. Nothing is saved without asking.
 */
@Component({
  selector: 'f-text-editor',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { style: 'display: contents' },
  template: `<div class="editor">
    @if (problem(); as p) {
      <p class="editor-problem" role="alert">{{ p }}</p>
    } @else if (file() === null) {
      <p class="loading" i18n="@@loading">Loading…</p>
    } @else {
      <textarea
        #area
        spellcheck="false"
        [value]="file()!.text"
        (input)="changed(area.value)"
        (keydown)="key($event)"
        aria-label="Text of the file"
        i18n-aria-label="@@edit.area"
      ></textarea>
    }
    <div class="editor-bar">
      <small>
        @if (dirty()) {
          <ng-container i18n="@@edit.unsaved">Not saved</ng-container>
        } @else if (file()) {
          <ng-container i18n="@@edit.saved">Saved</ng-container>
        }
      </small>
      <button type="button" class="btn" (click)="leave()" i18n="@@edit.close">Close editor</button>
      <button type="button" class="btn primary" [disabled]="!dirty() || saving()" (click)="save()">
        <ng-container i18n="@@edit.save">Save</ng-container>
      </button>
    </div>
  </div>`,
})
export class TextEditor {
  readonly share = input.required<string>();
  readonly entry = input.required<Entry>();
  /** The editor is done: saved or not, the person left it. */
  readonly done = output<Entry | null>();

  private readonly api = inject(Api);
  private readonly dialogs = inject(Dialogs);
  private readonly toasts = inject(Toasts);
  private readonly format = inject(Format);
  private readonly area = viewChild<ElementRef<HTMLTextAreaElement>>('area');

  protected readonly file = signal<TextFile | null>(null);
  protected readonly problem = signal<string | null>(null);
  protected readonly dirty = signal(false);
  protected readonly saving = signal(false);
  private etag = '';
  private saved: Entry | null = null;

  constructor() {
    effect(() => {
      const share = this.share();
      const e = this.entry();
      void this.load(share, e);
    });
    effect(() => this.area()?.nativeElement.focus());
  }

  private async load(share: string, e: Entry): Promise<void> {
    this.file.set(null);
    this.problem.set(null);
    this.dirty.set(false);
    try {
      const { bytes, etag } = await this.api.readFile(share, e.path, MAX_EDIT_BYTES);
      const f = decodeText(bytes);
      if (!f) {
        this.problem.set(
          $localize`:@@edit.notUtf8:This file is not UTF-8 text; saving it from here could change it. Download it to edit it.`,
        );
        return;
      }
      this.etag = etag;
      this.file.set(f);
    } catch (err) {
      this.problem.set(
        err instanceof ApiError && err.code === 'too_large'
          ? $localize`:@@edit.tooLarge:Only files up to ${this.format.size(MAX_EDIT_BYTES)}:max: are edited here.`
          : describe(err),
      );
    }
  }

  protected changed(text: string): void {
    const f = this.file();
    if (!f) return;
    f.text = text;
    this.dirty.set(true);
  }

  protected key(ev: KeyboardEvent): void {
    if ((ev.ctrlKey || ev.metaKey) && ev.key === 's') {
      ev.preventDefault();
      void this.save();
    }
    // Escape closes the preview elsewhere; here it stays in the editor.
    if (ev.key === 'Escape') ev.stopPropagation();
  }

  protected async save(force = false): Promise<void> {
    const f = this.file();
    if (!f || this.saving()) return;
    this.saving.set(true);
    try {
      const e = await this.api.saveFile(
        this.share(),
        this.entry().path,
        encodeText(f),
        force ? null : this.etag,
      );
      this.etag = `"${e.version}"`;
      this.saved = e;
      this.dirty.set(false);
      this.toasts.show($localize`:@@edit.done:Saved: ${e.name}:name:`);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'changed' && !force) {
        const overwrite = await this.dialogs.confirm({
          title: $localize`:@@edit.changedTitle:The file changed meanwhile`,
          text: $localize`:@@edit.changedText:Someone saved “${this.entry().name}:name:” since you opened it. Save your text over theirs, or keep editing and copy what you need first?`,
          ok: $localize`:@@edit.overwrite:Save over it`,
          danger: true,
        });
        this.saving.set(false);
        if (overwrite) await this.save(true);
        return;
      }
      this.toasts.error(describe(err));
    } finally {
      this.saving.set(false);
    }
  }

  /** Leaves the editor, after asking when there is unsaved text. */
  async leave(): Promise<boolean> {
    if (this.dirty()) {
      const discard = await this.dialogs.confirm({
        title: $localize`:@@edit.discardTitle:Discard your changes?`,
        text: $localize`:@@edit.discardText:What you changed in “${this.entry().name}:name:” is not saved.`,
        ok: $localize`:@@edit.discard:Discard`,
        danger: true,
      });
      if (!discard) return false;
    }
    this.done.emit(this.saved);
    return true;
  }
}
