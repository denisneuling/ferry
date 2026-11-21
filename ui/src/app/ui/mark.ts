import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** The ferry mark: a small boat on two waves. */
@Component({
  selector: 'f-mark',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { style: 'display: contents' },
  template: `<svg
    class="ferry-mark"
    [attr.width]="size()"
    [attr.height]="size()"
    viewBox="0 0 32 32"
    fill="none"
    aria-hidden="true"
  >
    <path d="M4.5 16.5h23l-3.2 6.1a2 2 0 0 1-1.77 1.07H9.47a2 2 0 0 1-1.77-1.07Z" fill="currentColor" />
    <path
      d="M10 16.5v-3.8a1.2 1.2 0 0 1 1.2-1.2h7.3l3 5"
      stroke="currentColor"
      stroke-width="2"
      stroke-linejoin="round"
    />
    <path d="M15 11.5V7.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    <path
      d="M3 27.5c2.2 0 2.2-1.6 4.4-1.6s2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6"
      style="stroke: var(--wave, currentColor)"
      stroke-width="2"
      stroke-linecap="round"
    />
  </svg>`,
})
export class Mark {
  readonly size = input(28);
}
