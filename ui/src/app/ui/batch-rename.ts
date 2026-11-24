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
import { RenamePlan, planRename, steps } from '../core/batch-rename';
import { Browser } from '../core/browser';
import { describe } from '../core/errors';
import { previewKind } from '../core/kinds';
import { Toasts } from './toast';

/** What renaming several entries asks for: the share, the folder, and them. */
export interface BatchOf {
  share: string;
  dir: string;
  chosen: Entry[];
  folder: Entry[];
}

/** How a rename went that stopped part-way. */
export interface BatchReport {
  done: number;
  total: number;
  failed: { name: string; error: string } | null;
  /** Entries left under a temporary name: their old name, and the one they have now. */
  stranded: { original: string; now: string }[];
}

/** At most this many pictures are asked for when they were taken. */
const MAX_TAKEN = 500;

@Injectable({ providedIn: 'root' })
export class BatchRename {
  readonly current = signal<BatchOf | null>(null);

  show(of: BatchOf): void {
    this.current.set(of);
  }

  close(): void {
    this.current.set(null);
  }
}

/**
 * Renaming several entries by a pattern, with every new name shown before
 * anything is renamed. Renaming is the server's one move per entry; there
 * is no transaction over many, so what stops part-way is said exactly:
 * which got their new names, and any left under a temporary one.
 */
@Component({
  selector: 'f-batch-rename',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@if (batch.current(); as of) {
    <dialog #dlg class="fx-modal fx-batch" aria-modal="true" (cancel)="$event.preventDefault(); close()">
      <h2 class="fx-modal-title" i18n="@@batch.title">Rename {{ of.chosen.length }} entries</h2>
      @if (report(); as r) {
        <p class="fx-modal-error" role="alert" i18n="@@batch.stopped">
          Renaming stopped after {{ r.done }} of {{ r.total }} steps.
        </p>
        @if (r.failed; as fl) {
          <p class="fx-modal-text">
            <ng-container i18n="@@batch.failedAt">“{{ fl.name }}” could not be renamed:</ng-container>
            {{ fl.error }}
          </p>
        }
        @if (r.stranded.length) {
          <p class="fx-modal-text" i18n="@@batch.stranded">
            These now have a temporary name; rename them back:
          </p>
          <ul class="fx-batch-list">
            @for (s of r.stranded; track s.now) {
              <li>
                <span>{{ s.original }}</span><span>→ {{ s.now }}</span>
              </li>
            }
          </ul>
        }
        <div class="fx-modal-actions">
          <button type="button" class="fx-btn primary" (click)="close()" i18n="@@versions.close">
            Close
          </button>
        </div>
      } @else {
        <div class="fx-batch-fields">
          <label
            ><span i18n="@@batch.pattern">Pattern</span>
            <input
              type="text"
              [value]="pattern()"
              (input)="pattern.set(field($event))"
              [disabled]="running()"
          /></label>
          <label
            ><span i18n="@@batch.start">Start at</span>
            <input
              type="number"
              min="0"
              max="999999"
              [value]="start()"
              (input)="start.set(num($event, 1))"
              [disabled]="running()"
          /></label>
          <label
            ><span i18n="@@batch.digits">Digits</span>
            <input
              type="number"
              min="1"
              max="9"
              [value]="digits()"
              (input)="digits.set(num($event, 3))"
              [disabled]="running()"
          /></label>
        </div>
        <p class="fx-batch-help" i18n="@@batch.help">
          {{ '{' }}name{{ '}' }} old name · {{ '{' }}ext{{ '}' }} extension · {{ '{' }}n{{ '}' }} counter ·
          {{ '{' }}date{{ '}' }} day modified · {{ '{' }}taken{{ '}' }} day a photo was taken
        </p>
        @if (plan(); as p) {
          <ul class="fx-batch-list">
            @for (r of p.rows.slice(0, 300); track r.entry.name) {
              <li [class.bad]="r.problem">
                <span>{{ r.entry.name }}</span>
                <span
                  >→ {{ r.to }}
                  @switch (r.problem) {
                    @case ('invalid') {
                      <small i18n="@@batch.invalid">not a possible name</small>
                    }
                    @case ('twice') {
                      <small i18n="@@batch.twice">given twice</small>
                    }
                    @case ('taken') {
                      <small i18n="@@batch.taken">exists already</small>
                    }
                  }
                </span>
              </li>
            }
          </ul>
          @if (p.rows.length > 300) {
            <p class="fx-modal-text" i18n="@@batch.more">… and {{ p.rows.length - 300 }} more.</p>
          }
        }
        @if (running()) {
          <p class="fx-modal-text" i18n="@@batch.running">Renaming… {{ progress() }} of {{ total() }}</p>
        }
        <div class="fx-modal-actions">
          <button
            type="button"
            class="fx-btn"
            (click)="close()"
            [disabled]="running()"
            i18n="@@dialog.cancel"
          >
            Cancel
          </button>
          <button
            type="button"
            class="fx-btn primary"
            (click)="run(of)"
            [disabled]="!plan()?.ok || running()"
            i18n="@@rename.ok"
          >
            Rename
          </button>
        </div>
      }
    </dialog>
  }`,
})
export class BatchRenameHost {
  protected readonly batch = inject(BatchRename);
  private readonly api = inject(Api);
  private readonly browser = inject(Browser);
  private readonly toasts = inject(Toasts);
  private readonly dlg = viewChild<ElementRef<HTMLDialogElement>>('dlg');

  protected readonly pattern = signal('{name}.{ext}');
  protected readonly start = signal(1);
  protected readonly digits = signal(3);
  protected readonly taken = signal<ReadonlyMap<string, string>>(new Map());
  protected readonly running = signal(false);
  protected readonly progress = signal(0);
  protected readonly total = signal(0);
  protected readonly report = signal<BatchReport | null>(null);
  private takenFor: BatchOf | null = null;

  protected readonly plan = computed<RenamePlan | null>(() => {
    const of = this.batch.current();
    if (!of) return null;
    return planRename(
      of.chosen,
      of.folder,
      this.pattern(),
      { start: this.start(), digits: this.digits() },
      this.taken(),
    );
  });

  constructor() {
    effect(() => {
      const of = this.batch.current();
      const el = this.dlg()?.nativeElement;
      if (of && el && !el.open) el.showModal();
    });
    // When the pattern asks for {taken}, the pictures' capture days are read,
    // four at a time and at most MAX_TAKEN, once per dialog.
    effect(() => {
      const of = this.batch.current();
      if (!of || !this.pattern().includes('{taken') || this.takenFor === of) return;
      this.takenFor = of;
      void this.readTaken(of);
    });
  }

  private async readTaken(of: BatchOf): Promise<void> {
    const todo = of.chosen
      .filter((e) => e.type === 'file' && ['image', 'derived'].includes(previewKind(e.name)))
      .slice(0, MAX_TAKEN);
    const found = new Map<string, string>();
    let next = 0;
    const worker = async () => {
      while (next < todo.length && this.batch.current() === of) {
        const e = todo[next++];
        try {
          const m = await this.api.metadata(of.share, e.path, e.version);
          if (m.taken) found.set(e.name, m.taken);
        } catch {
          // No capture day: its modification day stands in.
        }
      }
    };
    await Promise.all([worker(), worker(), worker(), worker()]);
    if (this.batch.current() === of) this.taken.set(found);
  }

  protected field(ev: Event): string {
    return (ev.target as HTMLInputElement).value;
  }

  protected num(ev: Event, fallback: number): number {
    const n = Number((ev.target as HTMLInputElement).value);
    return Number.isInteger(n) && n >= 0 ? n : fallback;
  }

  protected close(): void {
    if (this.running()) return;
    this.dlg()?.nativeElement.close();
    this.batch.close();
    this.report.set(null);
    this.taken.set(new Map());
    this.takenFor = null;
    this.pattern.set('{name}.{ext}');
  }

  /**
   * Renames, step by step, as planned — and only a plan whose every name
   * may be. A step refused ends it there: nothing after it is tried, and the
   * dialog says what happened.
   */
  protected async run(of: BatchOf): Promise<void> {
    const plan = this.plan();
    if (!plan?.ok || this.running()) return;
    const tag = randomTag();
    const todo = steps(plan, (i) => `.ferry-tmp-${tag}-${i}`);
    this.running.set(true);
    this.total.set(todo.length);
    this.progress.set(0);
    const parked = new Map<string, string>(); // original → temporary name, while parked
    let failed: BatchReport['failed'] = null;
    let done = 0;
    for (const s of todo) {
      try {
        await this.api.move(of.share, join(of.dir, s.from), join(of.dir, s.to));
      } catch (e) {
        failed = { name: s.original, error: describe(e) };
        break;
      }
      done++;
      this.progress.set(done);
      if (s.temp) parked.set(s.original, s.to);
      else parked.delete(s.original);
    }
    this.running.set(false);
    await this.browser.reload();
    if (!failed) {
      this.toasts.show($localize`:@@batch.done:${plan.changes}:n: entries renamed.`);
      this.close();
      return;
    }
    this.report.set({
      done,
      total: todo.length,
      failed,
      stranded: [...parked].map(([original, now]) => ({ original, now })),
    });
  }
}

/** Eight random hex digits: temporary names no one else picks by chance. */
function randomTag(): string {
  const b = new Uint8Array(4);
  crypto.getRandomValues(b);
  return [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
}
