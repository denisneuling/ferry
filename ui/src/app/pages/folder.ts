import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { Api, Entry } from '../core/api';
import { Actions } from '../core/actions';
import { Browser, SortKey } from '../core/browser';
import { Format } from '../core/format';
import { hasThumbnail, kindIcon, kindOf } from '../core/kinds';
import { Prefs } from '../core/prefs';
import { Transfers } from '../core/transfers';
import { Icon } from '../ui/icon';
import { Menu, MenuItem } from '../ui/menu';

/**
 * A folder of a share: its entries as tiles or a list, with selection,
 * sorting, a filter, the context menu, keys, and uploads by picking or
 * dropping files.
 */
@Component({
  selector: 'f-folder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Icon, Menu],
  host: {
    '(document:keydown)': 'key($event)',
    '(window:dragenter)': 'dragEnter($event)',
    '(window:dragover)': 'dragOver($event)',
    '(window:dragleave)': 'dragLeave()',
    '(window:drop)': 'drop($event)',
  },
  templateUrl: './folder.html',
})
export class Folder {
  protected readonly browser = inject(Browser);
  protected readonly prefs = inject(Prefs);
  protected readonly format = inject(Format);
  protected readonly actions = inject(Actions);
  private readonly api = inject(Api);
  private readonly transfers = inject(Transfers);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  private readonly params = toSignal(this.route.paramMap);
  private readonly query = toSignal(this.route.queryParamMap);
  private readonly picker = viewChild<ElementRef<HTMLInputElement>>('picker');
  private readonly search = viewChild<ElementRef<HTMLInputElement>>('search');

  protected readonly dragging = signal(false);
  private dragDepth = 0;
  protected readonly menu = signal<{ x: number; y: number; items: MenuItem[] } | null>(null);
  /** Thumbnails the server could not make: shown as icons instead. */
  protected readonly broken = signal<ReadonlySet<string>>(new Set());

  protected readonly crumbs = computed(() => {
    const p = this.browser.path();
    const parts = p ? p.split('/') : [];
    return parts.slice(0, -1).map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
  });
  protected readonly selecting = computed(() => this.browser.selected().size > 0);
  protected readonly count = computed(() => (this.browser.entries() ?? []).length);

  constructor() {
    // The URL is the truth: /browse/<share>?path=<folder>.
    effect(() => {
      const share = this.params()?.get('share');
      const path = this.query()?.get('path') ?? '';
      if (share) {
        untracked(() => void this.browser.open(share, path));
      }
    });
  }

  protected link(path: string): { path: string } | Record<string, never> {
    return path ? { path } : {};
  }

  protected go(path: string): void {
    void this.router.navigate(['/browse', this.browser.share()], { queryParams: this.link(path) });
  }

  protected up(): void {
    const p = this.browser.path();
    if (p) this.go(p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
    else void this.router.navigate(['/']);
  }

  protected kind(e: Entry) {
    return kindOf(e.name, e.type === 'folder');
  }

  protected icon(e: Entry): string {
    return kindIcon[this.kind(e)];
  }

  protected thumb(e: Entry): string | null {
    const share = this.browser.share();
    if (!share || e.type !== 'file' || !hasThumbnail(e.name) || this.broken().has(e.path)) return null;
    return this.api.thumbnailUrl(share, e.path, 320);
  }

  protected thumbFailed(e: Entry): void {
    this.broken.update((s) => new Set(s).add(e.path));
  }

  /** A click on an entry: with a modifier or a selection running it selects, otherwise it opens. */
  protected activate(e: Entry, ev: MouseEvent): void {
    if (ev.shiftKey || ev.ctrlKey || ev.metaKey) {
      this.browser.toggle(e.name, ev.shiftKey);
      return;
    }
    if (this.selecting()) {
      this.browser.toggle(e.name);
      return;
    }
    this.open(e);
  }

  protected open(e: Entry): void {
    if (e.type === 'folder') {
      this.go(e.path);
      return;
    }
    this.browser.preview.set(this.browser.files().indexOf(e));
  }

  protected check(e: Entry, ev: Event): void {
    ev.stopPropagation();
    this.browser.toggle(e.name, (ev as MouseEvent).shiftKey);
  }

  protected toggleAll(): void {
    if (this.browser.selected().size === this.browser.visible().length) this.browser.clear();
    else this.browser.selectAll();
  }

  protected sortBy(k: SortKey): void {
    this.browser.setSort(k);
  }

  protected setFilter(v: string): void {
    this.browser.filter.set(v);
    this.browser.clear();
  }

  protected pick(): void {
    this.picker()?.nativeElement.click();
  }

  protected picked(input: HTMLInputElement): void {
    if (input.files?.length) {
      this.upload(input.files);
    }
    input.value = '';
  }

  private upload(files: FileList): void {
    const share = this.browser.share();
    if (share) {
      this.transfers.add(share, this.browser.path(), this.browser.title(), Array.from(files));
    }
  }

  // ── The context menu, and a long press on touch ──────────────────────────

  protected contextMenu(e: Entry, ev: MouseEvent): void {
    ev.preventDefault();
    if (!this.browser.selected().has(e.name)) this.browser.only(e.name);
    this.showMenu(ev.clientX, ev.clientY, e);
  }

  private showMenu(x: number, y: number, e: Entry): void {
    const sel = this.browser.selection();
    const one = sel.length === 1;
    const items: MenuItem[] = [];
    if (one) {
      items.push(
        e.type === 'folder'
          ? { icon: 'folder', label: $localize`:@@act.open:Open`, run: () => this.open(e) }
          : { icon: 'eye', label: $localize`:@@act.preview:Preview`, run: () => this.open(e) },
      );
    }
    items.push({
      icon: 'download',
      label: one
        ? $localize`:@@act.download:Download`
        : $localize`:@@act.downloadN:Download ${sel.length}:n:`,
      run: () => this.actions.download(sel),
    });
    if (one)
      items.push({
        icon: 'pencil',
        label: $localize`:@@act.rename:Rename`,
        run: () => this.actions.rename(e),
      });
    items.push({ icon: 'move', label: $localize`:@@act.move:Move`, run: () => this.actions.move(sel) });
    items.push({ separator: true });
    items.push({
      icon: 'trash',
      label: $localize`:@@act.delete:Delete`,
      danger: true,
      run: () => this.actions.remove(sel),
    });
    this.menu.set({ x, y, items });
  }

  private press: ReturnType<typeof setTimeout> | null = null;
  private swallow = false;

  protected pointerDown(e: Entry, ev: PointerEvent): void {
    if (ev.pointerType === 'mouse') return;
    this.press = setTimeout(() => {
      this.press = null;
      this.swallow = true;
      navigator.vibrate?.(10);
      this.browser.toggle(e.name);
    }, 480);
  }

  protected pointerEnd(): void {
    if (this.press) {
      clearTimeout(this.press);
      this.press = null;
    }
  }

  protected click(e: Entry, ev: MouseEvent): void {
    if (this.swallow) {
      this.swallow = false;
      ev.preventDefault();
      return;
    }
    this.activate(e, ev);
  }

  // ── Keys ──────────────────────────────────────────────────────────────────

  protected key(ev: KeyboardEvent): void {
    if (this.browser.preview() !== null || this.menu() || document.querySelector('dialog[open]')) return;
    const typing = /INPUT|TEXTAREA|SELECT/.test(
      (document.activeElement as HTMLElement | null)?.tagName ?? '',
    );
    if (ev.key === 'Escape') {
      if (typing && this.browser.filter()) this.setFilter('');
      else this.browser.clear();
      return;
    }
    if (typing) return;
    const list = this.browser.visible();
    const names = list.map((e) => e.name);
    const cur = [...this.browser.selected()].pop();
    const i = cur ? names.indexOf(cur) : -1;
    const sel = this.browser.selection();
    if ((ev.ctrlKey || ev.metaKey) && ev.key === 'a') {
      ev.preventDefault();
      this.browser.selectAll();
    } else if (ev.key === 'Delete' && sel.length) {
      void this.actions.remove(sel);
    } else if (ev.key === 'F2' && sel.length === 1) {
      ev.preventDefault();
      void this.actions.rename(sel[0]);
    } else if (ev.key === 'Backspace' || (ev.altKey && ev.key === 'ArrowUp')) {
      ev.preventDefault();
      this.up();
    } else if (ev.key === 'Enter' && cur && i >= 0) {
      this.open(list[i]);
    } else if (['ArrowDown', 'ArrowRight', 'j'].includes(ev.key) && names.length) {
      ev.preventDefault();
      this.browser.only(names[Math.min(names.length - 1, i + 1)]);
      this.scrollToSelection();
    } else if (['ArrowUp', 'ArrowLeft', 'k'].includes(ev.key) && names.length) {
      ev.preventDefault();
      this.browser.only(names[Math.max(0, i - 1)]);
      this.scrollToSelection();
    } else if (ev.key === '/') {
      ev.preventDefault();
      this.search()?.nativeElement.focus();
    }
  }

  private scrollToSelection(): void {
    requestAnimationFrame(() => document.querySelector('.is-sel')?.scrollIntoView({ block: 'nearest' }));
  }

  // ── Dropping files from the desktop ──────────────────────────────────────

  private hasFiles(ev: DragEvent): boolean {
    return !!ev.dataTransfer && Array.from(ev.dataTransfer.types).includes('Files');
  }

  protected dragEnter(ev: DragEvent): void {
    if (!this.hasFiles(ev)) return;
    ev.preventDefault();
    this.dragDepth++;
    this.dragging.set(true);
  }

  protected dragOver(ev: DragEvent): void {
    if (this.hasFiles(ev)) ev.preventDefault();
  }

  protected dragLeave(): void {
    if (--this.dragDepth <= 0) {
      this.dragDepth = 0;
      this.dragging.set(false);
    }
  }

  protected drop(ev: DragEvent): void {
    if (!this.hasFiles(ev)) return;
    ev.preventDefault();
    this.dragDepth = 0;
    this.dragging.set(false);
    if (ev.dataTransfer?.files.length) this.upload(ev.dataTransfer.files);
  }
}
