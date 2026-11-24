import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  LOCALE_ID,
  computed,
  effect,
  inject,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { Places } from '../core/places';
import { Api, Entry } from '../core/api';
import { Actions } from '../core/actions';
import { Browser, SortKey } from '../core/browser';
import { Format } from '../core/format';
import { hasThumbnail, kindIcon, kindOf, previewKind } from '../core/kinds';
import { Prefs } from '../core/prefs';
import { Search } from '../core/search';
import { Snapshot, isToken, tokenTime } from '../core/snapshot';
import { MAX_TREE_FILES, TreeFile, Transfers } from '../core/transfers';
import { Session } from '../core/session';
import { Player } from '../core/player';
import { Icon } from '../ui/icon';
import { Menu, MenuItem } from '../ui/menu';
import { Space } from '../ui/space';
import { Versions } from '../ui/versions';

/** The type a drag within the page carries; a drag from the desktop has Files instead. */
const DRAG_TYPE = 'application/x-ferry-entries';

/**
 * The files of what was dropped, folders walked, each with its path below
 * the drop; more than max stops the walk, and the upload refuses it.
 */
async function walkDropped(entries: FileSystemEntry[], max: number): Promise<TreeFile[]> {
  const out: TreeFile[] = [];
  const walk = async (e: FileSystemEntry, rel: string, depth: number): Promise<void> => {
    if (out.length > max || depth > 32) return;
    if (e.isFile) {
      const file = await new Promise<File>((ok, fail) => (e as FileSystemFileEntry).file(ok, fail));
      out.push({ rel, file });
      return;
    }
    const reader = (e as FileSystemDirectoryEntry).createReader();
    for (;;) {
      // readEntries answers in batches until it answers none.
      const batch = await new Promise<FileSystemEntry[]>((ok, fail) => reader.readEntries(ok, fail));
      if (!batch.length) return;
      for (const child of batch) await walk(child, `${rel}/${child.name}`, depth + 1);
    }
  };
  for (const e of entries) await walk(e, e.name, 0);
  return out;
}

/** Entries rendered at once, and added each time the end comes near. */
const PAGE = 600;

/**
 * A folder of a share: its entries as tiles or a list, with selection,
 * sorting, a filter, the context menu, keys, and uploads by picking or
 * dropping files.
 */
@Component({
  selector: 'f-folder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Icon, Menu, Space],
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
  private readonly session = inject(Session);
  private readonly player = inject(Player);
  /** The folder's recordings, as shown: what Play plays. */
  protected readonly recordings = computed(() => Player.recordings(this.browser.files()));
  private readonly versions = inject(Versions);
  /** The search below this folder, while the field asks for one. */
  protected readonly finder = inject(Search);
  /** Set while the folder is shown as a snapshot holds it: then nothing changes. */
  protected readonly snapshot = inject(Snapshot);
  protected readonly pastWhen = computed(() => {
    const at = this.snapshot.at();
    const t = at ? tokenTime(at) : null;
    return t ? this.format.dateTime(t.toISOString()) : '';
  });
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly locale = inject(LOCALE_ID);
  protected readonly places = inject(Places);
  /** This folder as a place, for its star; none in a snapshot. */
  protected readonly here = computed(() => {
    const share = this.browser.share();
    return share && !this.snapshot.at() ? { share, path: this.browser.path(), folder: true } : null;
  });

  private readonly params = toSignal(this.route.paramMap);
  private readonly query = toSignal(this.route.queryParamMap);
  private readonly picker = viewChild<ElementRef<HTMLInputElement>>('picker');
  private readonly search = viewChild<ElementRef<HTMLInputElement>>('search');

  protected readonly dragging = signal(false);
  /** Entries dragged within the page, to be moved where they are dropped. */
  private dragged: Entry[] | null = null;
  /** The folder a drag hovers over, by path. */
  protected readonly dropTarget = signal<string | null>(null);
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
  protected readonly countText = computed(() => this.count().toLocaleString(this.locale));

  /**
   * How many entries are rendered. A folder of fifty thousand tiles would
   * stall any browser, so the page renders a window from the top and widens
   * it as the end comes into view; selection, sorting and the filter still
   * work on the whole folder.
   */
  protected readonly shown = signal(PAGE);
  protected readonly shownFolders = computed(() => this.browser.folders().slice(0, this.shown()));
  protected readonly shownFiles = computed(() =>
    this.browser.files().slice(0, Math.max(0, this.shown() - this.browser.folders().length)),
  );
  protected readonly shownRows = computed(() => this.browser.visible().slice(0, this.shown()));
  protected readonly more = computed(() => this.browser.visible().length > this.shown());
  private readonly sentinel = viewChild<ElementRef<HTMLElement>>('sentinel');

  constructor() {
    // The URL is the truth: /browse/<share>?path=<folder>[&at=<snapshot>].
    effect(() => {
      const share = this.params()?.get('share');
      const path = this.query()?.get('path') ?? '';
      const at = this.query()?.get('at');
      if (share) {
        untracked(() => {
          this.shown.set(PAGE);
          void this.browser.open(share, path, isToken(at) ? at : null);
        });
      }
    });
    // With "include subfolders", what is typed is searched for below the
    // folder, a moment after the typing stops; anything else ends it.
    effect((cleanup) => {
      const q = this.browser.filter().trim();
      const share = this.browser.share();
      const path = this.browser.path();
      this.snapshot.at(); // a snapshot browsed is searched as it is
      if (!this.finder.deep() || q.length < 2 || !share) {
        untracked(() => this.finder.stop());
        return;
      }
      const t = setTimeout(() => void this.finder.run(share, path, q), 400);
      cleanup(() => clearTimeout(t));
    });
    // The window widens when its end comes near the viewport.
    effect((cleanup) => {
      const el = this.sentinel()?.nativeElement;
      if (!el || typeof IntersectionObserver === 'undefined') return;
      const io = new IntersectionObserver(
        (seen) => {
          if (seen.some((s) => s.isIntersecting)) this.shown.update((n) => n + PAGE);
        },
        { rootMargin: '1200px 0px' },
      );
      io.observe(el);
      cleanup(() => io.disconnect());
    });
  }

  /** A folder's query: its path, and the snapshot while one is browsed. */
  protected link(path: string): Record<string, string> {
    const q: Record<string, string> = {};
    if (path) q['path'] = path;
    const at = this.snapshot.at();
    if (at) q['at'] = at;
    return q;
  }

  /** How many of the folder's files are pictures: sorting them by date taken needs two. */
  protected readonly pictureCount = computed(
    () => this.browser.files().filter((e) => ['image', 'derived'].includes(previewKind(e.name))).length,
  );

  /** Sorts by date taken, or back by name. */
  protected byTaken(): void {
    if (this.browser.sort() === 'taken') this.browser.setSort('name');
    else void this.browser.sortByTaken();
  }

  /** Plays the folder's recordings, from the one given or the first. */
  protected playFolder(from?: Entry): void {
    const share = this.browser.share();
    if (!share) return;
    this.player.play(
      { share, folder: this.browser.title(), at: this.snapshot.at(), tracks: this.recordings() },
      from,
    );
  }

  /** A search's hit, shown in its folder and selected there. */
  protected revealHit(e: Entry): void {
    const i = e.path.lastIndexOf('/');
    this.browser.reveal.set(e.name);
    this.setFilter('');
    this.go(i < 0 ? '' : e.path.slice(0, i));
  }

  /** Where a hit is, below the folder searched. */
  protected hitFolder(e: Entry): string {
    const base = this.browser.path();
    const i = e.path.lastIndexOf('/');
    const dir = i < 0 ? '' : e.path.slice(0, i);
    const rel = base && dir.startsWith(base) ? dir.slice(base.length).replace(/^\//, '') : dir;
    return rel || '.';
  }

  /** Back to the folder as it is now. */
  protected now(): void {
    const p = this.browser.path();
    void this.router.navigate(['/browse', this.browser.share()], { queryParams: p ? { path: p } : {} });
  }

  /** The previous versions of this folder, or of one entry in it. */
  protected showVersions(e?: Entry): void {
    const share = this.browser.share();
    if (!share) return;
    if (e) this.versions.show({ share, path: e.path, name: e.name, folder: e.type === 'folder' });
    else this.versions.show({ share, path: this.browser.path(), name: this.browser.title(), folder: true });
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
    return this.api.thumbnailUrl(share, e.path, 320, e.version);
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

  /** A folder picked to upload, with everything in it. */
  protected pickedFolder(input: HTMLInputElement): void {
    const share = this.browser.share();
    if (share && input.files?.length && !this.snapshot.at()) {
      const items: TreeFile[] = Array.from(input.files).map((file) => ({
        rel: file.webkitRelativePath || file.name,
        file,
      }));
      void this.transfers.addTree(share, this.browser.path(), items, this.maxTree());
    }
    input.value = '';
  }

  private maxTree(): number {
    return this.session.info()?.limits?.archive ?? MAX_TREE_FILES;
  }

  // ── Moving by dragging within the page ───────────────────────────────────

  protected dragStart(e: Entry, ev: DragEvent): void {
    if (this.snapshot.at() || !ev.dataTransfer) return;
    if (!this.browser.selected().has(e.name)) this.browser.only(e.name);
    this.dragged = this.browser.selection();
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData(DRAG_TYPE, this.dragged.map((d) => d.path).join('\n'));
  }

  protected dragEnd(): void {
    this.dragged = null;
    this.dropTarget.set(null);
  }

  /** Whether what is dragged may go into the folder at path: not into itself, nor below. */
  private canDrop(path: string): boolean {
    const d = this.dragged;
    if (!d?.length || path === this.browser.path()) return false;
    return !d.some((x) => x.path === path || path.startsWith(x.path + '/'));
  }

  protected dragOverPath(target: Entry | string, ev: DragEvent): void {
    const path = typeof target === 'string' ? target : target.type === 'folder' ? target.path : null;
    if (path === null || !this.canDrop(path)) return;
    ev.preventDefault();
    if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'move';
    this.dropTarget.set(path);
  }

  protected dropOnPath(target: Entry | string, ev: DragEvent): void {
    const path = typeof target === 'string' ? target : target.type === 'folder' ? target.path : null;
    this.dropTarget.set(null);
    if (path === null || !this.canDrop(path)) return;
    ev.preventDefault();
    ev.stopPropagation();
    const moving = this.dragged ?? [];
    this.dragged = null;
    void this.actions.moveTo(moving, path);
  }

  private upload(files: FileList): void {
    const share = this.browser.share();
    if (share && !this.snapshot.at()) {
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
    const share = this.browser.share();
    if (one && share && !this.snapshot.at()) {
      const place = { share, path: e.path, folder: e.type === 'folder' };
      items.push({
        icon: 'star',
        label: this.places.isFavourite(place)
          ? $localize`:@@fav.remove:Remove from favourites`
          : $localize`:@@fav.add:Add to favourites`,
        run: () => this.places.toggleFavourite(place),
      });
    }
    if (this.snapshot.at()) {
      // A previous version is read-only, and a ZIP is not made of one:
      // a single file downloads, and that is all.
      if (one && e.type === 'file')
        items.push({
          icon: 'download',
          label: $localize`:@@act.download:Download`,
          run: () => this.actions.download(sel),
        });
      if (items.length) this.menu.set({ x, y, items });
      return;
    }
    items.push({
      icon: 'download',
      label: one
        ? $localize`:@@act.download:Download`
        : $localize`:@@act.downloadN:Download ${sel.length}:n:`,
      run: () => this.actions.download(sel),
    });
    if (one && Player.recordings([e]).length)
      items.push({
        icon: 'play',
        label: $localize`:@@act.play:Play from here`,
        run: () => this.playFolder(e),
      });
    if (one)
      items.push({
        icon: 'clock',
        label: $localize`:@@act.versions:Previous versions`,
        run: () => this.showVersions(e),
      });
    if (one)
      items.push({
        icon: 'pencil',
        label: $localize`:@@act.rename:Rename`,
        run: () => this.actions.rename(e),
      });
    items.push({ icon: 'move', label: $localize`:@@act.move:Move`, run: () => this.actions.move(sel) });
    items.push({ icon: 'copy', label: $localize`:@@act.copy:Copy to…`, run: () => this.actions.copy(sel) });
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
    } else if (ev.key === 'Delete' && sel.length && !this.snapshot.at()) {
      void this.actions.remove(sel);
    } else if (ev.key === 'F2' && sel.length === 1 && !this.snapshot.at()) {
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
    return !this.snapshot.at() && !!ev.dataTransfer && Array.from(ev.dataTransfer.types).includes('Files');
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
    const dt = ev.dataTransfer;
    if (!dt) return;
    // Folders dropped come as entries to walk; plain files as files.
    const entries = Array.from(dt.items ?? [])
      .map((i) => (i.kind === 'file' ? i.webkitGetAsEntry?.() : null))
      .filter((e): e is FileSystemEntry => !!e);
    const share = this.browser.share();
    if (share && entries.some((e) => e.isDirectory)) {
      const max = this.maxTree();
      void walkDropped(entries, max).then(
        (items) => this.transfers.addTree(share, this.browser.path(), items, max),
        () => undefined,
      );
    } else if (dt.files.length) {
      this.upload(dt.files);
    }
  }
}
