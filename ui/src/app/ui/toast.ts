import { ChangeDetectionStrategy, Component, Injectable, inject, signal } from '@angular/core';

interface Toast {
  id: number;
  text: string;
  bad: boolean;
  shown: boolean;
}

/** Short notes at the bottom of the screen, gone by themselves. */
@Injectable({ providedIn: 'root' })
export class Toasts {
  readonly list = signal<Toast[]>([]);
  private next = 1;

  show(text: string, bad = false): void {
    const t: Toast = { id: this.next++, text, bad, shown: false };
    this.list.update((l) => [...l.slice(-3), t]);
    requestAnimationFrame(() =>
      this.list.update((l) => l.map((x) => (x.id === t.id ? { ...x, shown: true } : x))),
    );
    setTimeout(
      () => {
        this.list.update((l) => l.map((x) => (x.id === t.id ? { ...x, shown: false } : x)));
        setTimeout(() => this.list.update((l) => l.filter((x) => x.id !== t.id)), 300);
      },
      bad ? 6000 : 3200,
    );
  }

  error(text: string): void {
    this.show(text, true);
  }
}

@Component({
  selector: 'f-toasts',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div class="toasts" role="status" aria-live="polite">
    @for (t of toasts.list(); track t.id) {
      <div class="fx-toast" [class.in]="t.shown" [class.bad]="t.bad">{{ t.text }}</div>
    }
  </div>`,
})
export class ToastHost {
  protected readonly toasts = inject(Toasts);
}
