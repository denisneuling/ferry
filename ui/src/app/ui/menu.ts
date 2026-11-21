import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  afterNextRender,
  inject,
  input,
  output,
} from '@angular/core';
import { Icon } from './icon';

export type MenuItem =
  | { separator: true }
  | { separator?: false; icon: string; label: string; danger?: boolean; run: () => unknown };

/**
 * A context menu at a point: kept on screen, keyboard-operable (arrows,
 * Home, End, Escape), closed by a choice or a click elsewhere.
 */
@Component({
  selector: 'f-menu',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  host: {
    '(document:pointerdown)': 'outside($event)',
    '(document:keydown.escape)': 'closed.emit()',
    '(window:blur)': 'closed.emit()',
    '(window:resize)': 'closed.emit()',
  },
  template: `<div
    class="fx-menu"
    role="menu"
    [style.left.px]="x()"
    [style.top.px]="y()"
    (keydown)="keys($event)"
  >
    @for (item of items(); track $index) {
      @if (item.separator) {
        <div class="fx-menu-sep" role="separator"></div>
      } @else {
        <button
          type="button"
          class="fx-menu-item"
          [class.fx-danger]="item.danger"
          role="menuitem"
          (click)="choose(item)"
        >
          <f-icon [name]="item.icon" [size]="16" /><span>{{ item.label }}</span>
        </button>
      }
    }
  </div>`,
})
export class Menu {
  readonly x = input.required<number>();
  readonly y = input.required<number>();
  readonly items = input.required<MenuItem[]>();
  readonly closed = output<void>();
  private readonly host = inject(ElementRef<HTMLElement>);

  constructor() {
    afterNextRender(() => {
      const el = this.host.nativeElement.querySelector('.fx-menu') as HTMLElement;
      const r = el.getBoundingClientRect();
      if (r.right > innerWidth - 8) el.style.left = `${Math.max(8, innerWidth - r.width - 8)}px`;
      if (r.bottom > innerHeight - 8) el.style.top = `${Math.max(8, innerHeight - r.height - 8)}px`;
      (el.querySelector('button') as HTMLElement | null)?.focus();
    });
  }

  protected choose(item: MenuItem): void {
    this.closed.emit();
    if (!item.separator) {
      void item.run();
    }
  }

  protected outside(ev: PointerEvent): void {
    if (!this.host.nativeElement.contains(ev.target as Node)) {
      this.closed.emit();
    }
  }

  protected keys(ev: KeyboardEvent): void {
    const buttons = Array.from(this.host.nativeElement.querySelectorAll('button')) as HTMLElement[];
    const i = buttons.indexOf(document.activeElement as HTMLElement);
    const to = { ArrowDown: i + 1, ArrowUp: i - 1, Home: 0, End: buttons.length - 1 }[ev.key];
    if (to !== undefined) {
      ev.preventDefault();
      buttons[(to + buttons.length) % buttons.length]?.focus();
    } else if (ev.key === 'Tab') {
      this.closed.emit();
    }
  }
}
