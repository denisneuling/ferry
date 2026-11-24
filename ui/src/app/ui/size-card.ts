import { ChangeDetectionStrategy, Component, LOCALE_ID, inject } from '@angular/core';
import { FolderSize } from '../core/folder-size';
import { Format } from '../core/format';
import { Icon } from './icon';

/** The size of a folder being added up, in a card at the bottom of the page. */
@Component({
  selector: 'f-size-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  template: `@if (sizes.current(); as c) {
    <div class="size-card" role="status" aria-live="polite">
      <f-icon name="folder" [size]="20" />
      <div>
        <b>{{ c.name }}</b>
        @if (c.error) {
          <small class="bad">{{ c.error }}</small>
        } @else if (c.usage; as u) {
          <small>
            <span class="sz">{{ format.size(u.bytes) }}</span> ·
            <ng-container i18n="@@size.counts"
              >{{ n(u.files) }} files in {{ n(u.folders) }} folders</ng-container
            >
            @if (c.running) {
              · <ng-container i18n="@@size.counting">counting…</ng-container>
            } @else if (u.truncated) {
              · <ng-container i18n="@@size.partial">at least: not everything was counted</ng-container>
            }
            @if (u.skipped) {
              · <ng-container i18n="@@size.skipped">{{ n(u.skipped) }} folders not open to you</ng-container>
            }
          </small>
        } @else {
          <small i18n="@@size.counting">counting…</small>
        }
      </div>
      <button
        class="icon-btn"
        type="button"
        (click)="sizes.stop()"
        [title]="c.running ? stopLabel : closeLabel"
      >
        <f-icon name="x" [size]="16" />
      </button>
    </div>
  }`,
})
export class SizeCard {
  protected readonly sizes = inject(FolderSize);
  protected readonly format = inject(Format);
  private readonly locale = inject(LOCALE_ID);
  protected readonly stopLabel = $localize`:@@size.stop:Stop counting`;
  protected readonly closeLabel = $localize`:@@size.close:Close`;

  protected n(v: number): string {
    return v.toLocaleString(this.locale);
  }
}
