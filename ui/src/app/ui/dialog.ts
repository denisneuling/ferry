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
import { FormsModule } from '@angular/forms';

/** A question for the person: a confirmation, or a name to type. */
export interface Ask {
  title: string;
  text?: string;
  /** With a field: its label and starting value. */
  field?: string;
  value?: string;
  /** Select the name without its extension, as a rename does. */
  selectStem?: boolean;
  ok: string;
  danger?: boolean;
  /** Checks the typed value; a message refuses it. */
  check?: (value: string) => string | null;
}

interface Open extends Ask {
  resolve: (v: string | null) => void;
}

/**
 * One dialog at a time, on the native <dialog>: the browser keeps focus in
 * it, closes it on Escape and makes the rest inert.
 */
@Injectable({ providedIn: 'root' })
export class Dialogs {
  readonly current = signal<Open | null>(null);

  /** Resolves with the typed value (or '' for a confirmation), or null when cancelled. */
  ask(a: Ask): Promise<string | null> {
    this.current()?.resolve(null);
    return new Promise((resolve) => this.current.set({ ...a, resolve }));
  }

  async confirm(a: Omit<Ask, 'field' | 'value'>): Promise<boolean> {
    return (await this.ask(a)) !== null;
  }

  close(value: string | null): void {
    const c = this.current();
    this.current.set(null);
    c?.resolve(value);
  }
}

@Component({
  selector: 'f-dialogs',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule],
  template: `@if (dialogs.current(); as d) {
    <dialog #dlg class="fx-modal" aria-modal="true" (cancel)="$event.preventDefault(); dialogs.close(null)">
      <form (ngSubmit)="submit()">
        <h2 class="fx-modal-title">{{ d.title }}</h2>
        @if (d.text) {
          <p class="fx-modal-text">{{ d.text }}</p>
        }
        @if (d.field) {
          <label class="fx-field">
            <span>{{ d.field }}</span>
            <input #field name="value" [(ngModel)]="value" autocomplete="off" spellcheck="false" />
          </label>
          @if (problem()) {
            <p class="fx-modal-error" role="alert">{{ problem() }}</p>
          }
        }
        <div class="fx-modal-actions">
          <button type="button" class="fx-btn" (click)="dialogs.close(null)" i18n="@@dialog.cancel">
            Cancel
          </button>
          <button
            #ok
            type="submit"
            class="fx-btn"
            [class.fx-btn-danger]="d.danger"
            [class.fx-btn-primary]="!d.danger"
          >
            {{ d.ok }}
          </button>
        </div>
      </form>
    </dialog>
  }`,
})
export class DialogHost {
  protected readonly dialogs = inject(Dialogs);
  protected value = '';
  protected readonly problem = signal<string | null>(null);
  private readonly dlg = viewChild<ElementRef<HTMLDialogElement>>('dlg');
  private readonly field = viewChild<ElementRef<HTMLInputElement>>('field');
  private readonly ok = viewChild<ElementRef<HTMLButtonElement>>('ok');

  constructor() {
    effect(() => {
      const d = this.dialogs.current();
      const el = this.dlg()?.nativeElement;
      if (!d || !el) {
        return;
      }
      this.value = d.value ?? '';
      this.problem.set(null);
      if (!el.open) {
        el.showModal();
      }
      queueMicrotask(() => {
        const input = this.field()?.nativeElement;
        if (input) {
          input.value = this.value;
          input.focus();
          const dot = input.value.lastIndexOf('.');
          input.setSelectionRange(0, d.selectStem && dot > 0 ? dot : input.value.length);
        } else {
          this.ok()?.nativeElement.focus();
        }
      });
    });
  }

  protected submit(): void {
    const d = this.dialogs.current();
    if (!d) {
      return;
    }
    const v = d.field ? this.value.trim() : '';
    const p = d.field ? (d.check?.(v) ?? (v ? null : $localize`:@@dialog.empty:Enter a name.`)) : null;
    if (p) {
      this.problem.set(p);
      return;
    }
    this.dialogs.close(v);
  }
}

/** Refuses names a share cannot hold, before the server is asked. */
export function checkName(v: string): string | null {
  if (!v) {
    return $localize`:@@name.empty:Enter a name.`;
  }
  if (v === '.' || v === '..' || /[/\\\u0000]/.test(v)) {
    return $localize`:@@name.invalid:A name cannot contain / or \\ and cannot be . or ..`;
  }
  if (new TextEncoder().encode(v).length > 255) {
    return $localize`:@@name.long:This name is too long.`;
  }
  return null;
}
