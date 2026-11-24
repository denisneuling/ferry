import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  signal,
  viewChild,
} from '@angular/core';
import { Api, Book, BookNode } from '../core/api';
import { describe } from '../core/errors';

/** The elements a chapter's tree may hold; anything else is left out. */
const ALLOWED = new Set([
  'p',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'em',
  'strong',
  'u',
  's',
  'sub',
  'sup',
  'small',
  'blockquote',
  'ul',
  'ol',
  'li',
  'br',
  'hr',
  'pre',
  'code',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
  'figure',
  'figcaption',
  'dl',
  'dt',
  'dd',
  'div',
]);
/** At most this many pictures are shown in one chapter. */
const MAX_PICTURES = 300;

/**
 * An EPUB read chapter by chapter. The server reads each chapter out of its
 * XHTML into a tree of a few elements and text (internal/epub); here it is
 * built node by node, checked again, never from markup. A picture is shown
 * from the archive through the ZIP entry endpoint, which sends pictures as
 * pictures only.
 */
@Component({
  selector: 'f-book-view',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@if (error(); as err) {
      <p>{{ err }}</p>
    } @else if (book(); as b) {
      <div class="book-bar">
        <button
          type="button"
          class="btn"
          (click)="go(index() - 1)"
          [disabled]="index() === 0"
          i18n="@@book.prev"
        >
          Previous
        </button>
        <select
          [value]="index()"
          (change)="go(+$any($event.target).value)"
          aria-label="Chapter"
          i18n-aria-label="@@book.chapter"
        >
          @for (c of b.chapters; track $index) {
            <option [value]="$index">{{ c.title || chapterLabel($index) }}</option>
          }
        </select>
        <button
          type="button"
          class="btn"
          (click)="go(index() + 1)"
          [disabled]="index() >= b.chapters.length - 1"
          i18n="@@book.next"
        >
          Next
        </button>
      </div>
      @if (b.title) {
        <p class="book-title">{{ b.title }}</p>
      }
      <div #out class="book-text"></div>
      @if (truncated()) {
        <p i18n="@@book.truncated">… The rest of this chapter is not shown.</p>
      }
    } @else {
      <ng-container i18n="@@loading">Loading…</ng-container>
    }`,
})
export class BookView {
  readonly share = input.required<string>();
  readonly path = input.required<string>();
  private readonly api = inject(Api);
  protected readonly book = signal<Book | null>(null);
  protected readonly index = signal(0);
  protected readonly nodes = signal<BookNode[] | null>(null);
  protected readonly truncated = signal(false);
  protected readonly error = signal<string | null>(null);
  private readonly out = viewChild<ElementRef<HTMLElement>>('out');
  private readonly chapter = computed(() => this.book()?.chapters[this.index()] ?? null);
  private askedBook = 0;
  private askedChapter = 0;

  constructor() {
    effect(() => {
      const share = this.share();
      const path = this.path();
      const n = ++this.askedBook;
      this.book.set(null);
      this.error.set(null);
      this.index.set(0);
      this.api.book(share, path).then(
        (b) => n === this.askedBook && this.book.set(b),
        (e) => n === this.askedBook && this.error.set(describe(e)),
      );
    });
    effect(() => {
      const c = this.chapter();
      const share = this.share();
      const path = this.path();
      const n = ++this.askedChapter;
      this.nodes.set(null);
      if (!c) return;
      this.api.bookChapter(share, path, c.entry).then(
        (ch) => {
          if (n !== this.askedChapter) return;
          this.nodes.set(ch.nodes);
          this.truncated.set(ch.truncated);
        },
        (e) => n === this.askedChapter && this.error.set(describe(e)),
      );
    });
    effect(() => {
      const el = this.out()?.nativeElement;
      const nodes = this.nodes();
      if (!el) return;
      const pictures = { n: 0 };
      const url = (src: string) => this.api.zipEntryUrl(this.share(), this.path(), src, true);
      el.replaceChildren(...(nodes ?? []).map((x) => buildBookNode(x, url, pictures)));
      el.scrollIntoView({ block: 'start' });
    });
  }

  protected go(i: number): void {
    const b = this.book();
    if (b && i >= 0 && i < b.chapters.length) this.index.set(i);
  }

  protected chapterLabel(i: number): string {
    return $localize`:@@book.chapterN:Chapter ${i + 1}:n:`;
  }
}

/**
 * A chapter's node as DOM: text, an element from the set, or a picture
 * shown from the archive by url; anything else is nothing.
 */
export function buildBookNode(n: BookNode, url: (src: string) => string, pictures = { n: 0 }): Node {
  if (!n.tag) return document.createTextNode(n.text ?? '');
  if (n.tag === 'img') {
    if (!n.src || pictures.n++ >= MAX_PICTURES) return document.createTextNode(n.alt ?? '');
    const img = document.createElement('img');
    img.src = url(n.src);
    img.alt = n.alt ?? '';
    img.loading = 'lazy';
    return img;
  }
  if (!ALLOWED.has(n.tag)) return document.createTextNode('');
  const el = document.createElement(n.tag);
  el.append(...(n.children ?? []).map((c) => buildBookNode(c, url, pictures)));
  return el;
}
