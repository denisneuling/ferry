import { ChangeDetectionStrategy, Component, effect, inject, input, signal } from '@angular/core';
import { Api, Space as SpaceInfo } from '../core/api';
import { Format } from '../core/format';

/**
 * A volume's free space, as the SMB server reports it for the folder: a
 * quiet line and a thin bar of what is used. ferry works nothing out; when
 * the server does not answer, or refuses, the line is simply not there.
 */
@Component({
  selector: 'f-space',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { style: 'display: contents' },
  template: `@if (space(); as s) {
    <span class="space">
      <span class="space-bar" role="img" [attr.aria-label]="usedLabel(s)"
        ><i [style.width.%]="used(s)"></i
      ></span>
      <ng-container i18n="@@space.free"
        >{{ format.size(s.free) }} free of {{ format.size(s.total) }}</ng-container
      >
    </span>
  }`,
})
export class Space {
  readonly share = input.required<string>();
  readonly path = input('');
  protected readonly format = inject(Format);
  private readonly api = inject(Api);
  protected readonly space = signal<SpaceInfo | null>(null);
  private asked = 0;

  constructor() {
    effect(() => {
      const share = this.share();
      const path = this.path();
      const n = ++this.asked;
      this.api.space(share, path).then(
        (s) => n === this.asked && this.space.set(s.total > 0 ? s : null),
        () => n === this.asked && this.space.set(null),
      );
    });
  }

  protected used(s: SpaceInfo): number {
    return Math.min(100, Math.max(0, ((s.total - s.free) / s.total) * 100));
  }

  /** What the bar shows, for a screen reader: the bar is the used part. */
  protected usedLabel(s: SpaceInfo): string {
    return $localize`:@@space.used:${Math.round(this.used(s))}:percent: % used`;
  }
}
