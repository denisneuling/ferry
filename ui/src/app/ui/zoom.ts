import { Directive, ElementRef, computed, inject, signal } from '@angular/core';
import { FIT, View, clampView, maxScale, zoomAt } from '../core/zoom';

/**
 * Zoom and pan for a picture in the preview: the wheel or a pinch zooms at
 * the pointer, a drag moves the zoomed picture, a double click goes to the
 * picture's own pixels and back. Only for pictures the browser shows from
 * the file itself — there, one to one is the original; the server's
 * 640-pixel previews of HEIC and raw photos are not zoomed.
 */
@Directive({
  selector: 'img[fZoom]',
  exportAs: 'fZoom',
  host: {
    draggable: 'false',
    '[class.zoomed]': 'view().s > 1',
    '[style.transform]': 'transform()',
    '[style.cursor]': "view().s > 1 ? (dragging() ? 'grabbing' : 'grab') : 'zoom-in'",
    '[style.touch-action]': "'none'",
    '(load)': 'reset()',
    '(wheel)': 'wheel($event)',
    '(dblclick)': 'toggle($event)',
    '(pointerdown)': 'down($event)',
    '(pointermove)': 'move($event)',
    '(pointerup)': 'up($event)',
    '(pointercancel)': 'up($event)',
  },
})
export class Zoom {
  private readonly el = inject<ElementRef<HTMLImageElement>>(ElementRef).nativeElement;
  protected readonly view = signal<View>(FIT);
  protected readonly dragging = signal(false);
  /** The turn the picture is shown at, in degrees: only shown, never saved. */
  readonly rotation = signal(0);
  protected readonly transform = computed(() => {
    const v = this.view();
    const r = this.rotation();
    if (v.s === 1 && r === 0) return '';
    return `translate(${v.x}px, ${v.y}px) rotate(${r}deg) scale(${v.s * this.turnFit(r)})`;
  });
  /** Whether the picture is zoomed in. */
  readonly zoomed = computed(() => this.view().s > 1);
  private readonly pointers = new Map<number, { x: number; y: number }>();

  reset(): void {
    this.view.set(FIT);
    this.rotation.set(0);
    this.pointers.clear();
    this.dragging.set(false);
  }

  /** Turns the picture a quarter to the right, fitted anew. */
  rotate(): void {
    this.rotation.update((r) => (r + 90) % 360);
    this.view.set(FIT);
  }

  /**
   * How much a picture turned sideways is scaled to fit the stage again: its
   * laid-out box is still upright, so a turned one would stick out — or,
   * a portrait on a wide stage, stay small. Never beyond its own pixels.
   */
  private turnFit(r: number): number {
    const w = this.el.clientWidth;
    const h = this.el.clientHeight;
    const stage = this.el.parentElement;
    if (r % 180 === 0 || !w || !h || !stage) return 1;
    const own = this.el.naturalWidth / w;
    return Math.min(stage.clientWidth / h, stage.clientHeight / w, Math.max(own, 1));
  }

  /** Zooms by a factor around the stage's centre, for the keyboard. */
  by(factor: number): void {
    this.to(this.view().s * factor, 0, 0);
  }

  protected wheel(ev: WheelEvent): void {
    ev.preventDefault();
    const dy = ev.deltaMode === WheelEvent.DOM_DELTA_LINE ? ev.deltaY * 16 : ev.deltaY;
    const [cx, cy] = this.fromCentre(ev.clientX, ev.clientY);
    this.to(this.view().s * Math.exp(-dy * 0.002), cx, cy);
  }

  protected toggle(ev: MouseEvent): void {
    if (this.view().s > 1) {
      this.reset();
      return;
    }
    const oneToOne = this.el.clientWidth > 0 ? this.el.naturalWidth / this.el.clientWidth : 1;
    const [cx, cy] = this.fromCentre(ev.clientX, ev.clientY);
    this.to(oneToOne > 1.05 ? oneToOne : 2, cx, cy);
  }

  protected down(ev: PointerEvent): void {
    if (ev.pointerType === 'mouse' && ev.button !== 0) return;
    this.el.setPointerCapture(ev.pointerId);
    this.pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
    this.dragging.set(this.view().s > 1);
  }

  protected move(ev: PointerEvent): void {
    const last = this.pointers.get(ev.pointerId);
    if (!last) return;
    const others = [...this.pointers].filter(([id]) => id !== ev.pointerId).map(([, p]) => p);
    const now = { x: ev.clientX, y: ev.clientY };
    if (others.length === 1) {
      // A pinch: the distance between the fingers scales, around their middle.
      const o = others[0];
      const before = Math.hypot(last.x - o.x, last.y - o.y);
      const after = Math.hypot(now.x - o.x, now.y - o.y);
      if (before > 0) {
        const [cx, cy] = this.fromCentre((now.x + o.x) / 2, (now.y + o.y) / 2);
        this.to(this.view().s * (after / before), cx, cy);
      }
    } else if (others.length === 0 && this.view().s > 1) {
      const v = this.view();
      this.view.set(this.clamped({ s: v.s, x: v.x + now.x - last.x, y: v.y + now.y - last.y }));
    }
    this.pointers.set(ev.pointerId, now);
  }

  protected up(ev: PointerEvent): void {
    this.pointers.delete(ev.pointerId);
    if (this.pointers.size === 0) this.dragging.set(false);
  }

  private to(s: number, cx: number, cy: number): void {
    const top = maxScale(this.el.naturalWidth, this.el.clientWidth);
    const scale = Math.min(Math.max(s, 1), top);
    if (scale === 1) {
      this.view.set(FIT); // the turn stays
      return;
    }
    this.view.set(this.clamped(zoomAt(this.view(), scale, cx, cy)));
  }

  private clamped(v: View): View {
    const stage = this.el.parentElement;
    if (!stage) return v;
    // Turned sideways, the picture's width on the screen is its height.
    const k = this.turnFit(this.rotation());
    const [w, h] =
      this.rotation() % 180 === 0
        ? [this.el.clientWidth, this.el.clientHeight]
        : [this.el.clientHeight * k, this.el.clientWidth * k];
    return clampView(v, w, h, stage.clientWidth, stage.clientHeight);
  }

  /** A point on the screen, relative to the centre of the picture's stage. */
  private fromCentre(x: number, y: number): [number, number] {
    const r = (this.el.parentElement ?? this.el).getBoundingClientRect();
    return [x - (r.left + r.width / 2), y - (r.top + r.height / 2)];
  }
}
