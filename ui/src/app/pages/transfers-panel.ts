import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Format } from '../core/format';
import { Job, Transfers } from '../core/transfers';
import { Icon } from '../ui/icon';

/** Uploads and copies, bottom right: what runs, how far, what failed. */
@Component({
  selector: 'f-transfers',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  template: `@if (t.jobs().length) {
    <div class="transfers" role="region" aria-label="Uploads" i18n-aria-label="@@tr.label">
      <header>
        <f-icon [name]="t.running() ? 'upload' : 'check'" />
        <span aria-live="polite">
          @if (t.running()) {
            <ng-container i18n="@@tr.active">{t.running(), plural,
              =1 {1 transfer running}
              other {{{ t.running() }} transfers running}
            }</ng-container>
          } @else {
            <ng-container i18n="@@tr.finished">Transfers finished</ng-container>
          }
        </span>
        @if (!t.running()) {
          <button
            class="icon-btn"
            type="button"
            (click)="t.clearFinished()"
            title="Close"
            i18n-title="@@close"
          >
            <f-icon name="x" [size]="17" />
          </button>
        }
        <button
          class="icon-btn"
          type="button"
          (click)="t.open.set(!t.open())"
          [attr.aria-expanded]="t.open()"
          title="Show or hide"
          i18n-title="@@tr.toggle"
        >
          <f-icon [name]="t.open() ? 'down' : 'right'" [size]="17" />
        </button>
      </header>
      @if (t.open()) {
        <ul>
          @for (j of t.jobs(); track j.id) {
            <li [class.failed]="j.state === 'failed'">
              <div class="row1">
                <span>{{ j.name }}</span><span>{{ percent(j) }} %</span>
              </div>
              <div class="bar" [class.done]="j.state === 'done'"><i [style.width.%]="percent(j)"></i></div>
              <small>
                @switch (j.state) {
                  @case ('failed') {
                    {{ j.error }}
                  }
                  @case ('skipped') {
                    <ng-container i18n="@@tr.skipped"
                      >Skipped: a file of this name is already there.</ng-container
                    >
                  }
                  @case ('queued') {
                    <ng-container i18n="@@tr.queued">Waiting · to “{{ j.where }}”</ng-container>
                  }
                  @default {
                    <ng-container i18n="@@tr.of"
                      >{{ format.size(j.loaded) }} of {{ format.size(j.size) }} · to “{{
                        j.where
                      }}”</ng-container
                    >
                  }
                }
              </small>
            </li>
          }
        </ul>
      }
    </div>
  }`,
})
export class TransfersPanel {
  protected readonly t = inject(Transfers);
  protected readonly format = inject(Format);

  protected percent(j: Job): number {
    if (j.state === 'done') return 100;
    return j.size ? Math.min(100, Math.round((j.loaded / j.size) * 100)) : 0;
  }
}
