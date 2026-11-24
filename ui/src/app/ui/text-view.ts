import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  input,
  signal,
  viewChild,
} from '@angular/core';
import { extension } from '../core/kinds';
import { MD_TAGS, MdElement, MdNode, parseMarkdown, safeHref } from '../core/markdown';
import { Token, familyOf, highlight } from '../core/highlight';

const ALLOWED = new Set<string>(MD_TAGS);
const TOKEN_CLASS: Record<Token['kind'], string> = {
  comment: 'tk-c',
  string: 'tk-s',
  number: 'tk-n',
  keyword: 'tk-k',
  tag: 'tk-t',
  plain: '',
};

/**
 * A text file in the preview: Markdown laid out, source code coloured, any
 * other text as it is. Everything is built from text nodes and a fixed set
 * of elements (core/markdown.ts, core/highlight.ts) — never from HTML — so
 * nothing in a file becomes part of ferry's page.
 */
@Component({
  selector: 'f-text-view',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@if (markdown()) {
      <div class="md-switch" role="group">
        <button
          type="button"
          class="btn"
          [class.on]="!source()"
          (click)="source.set(false)"
          i18n="@@md.formatted"
        >
          Formatted
        </button>
        <button type="button" class="btn" [class.on]="source()" (click)="source.set(true)" i18n="@@md.source">
          Source
        </button>
      </div>
    }
    <div #out></div>`,
})
export class TextView {
  readonly text = input.required<string>();
  readonly name = input.required<string>();
  protected readonly source = signal(false);
  protected readonly markdown = computed(() => ['md', 'markdown'].includes(extension(this.name())));
  private readonly out = viewChild.required<ElementRef<HTMLElement>>('out');

  constructor() {
    effect(() => {
      const el = this.out().nativeElement;
      const text = this.text();
      if (this.markdown() && !this.source()) {
        el.className = 'md';
        el.replaceChildren(...parseMarkdown(text).map((n) => build(n)));
        return;
      }
      const family = this.markdown() ? null : familyOf(extension(this.name()));
      el.className = family ? 'code' : '';
      el.replaceChildren(...tokens(highlight(text, family)));
    });
  }
}

function tokens(ts: Token[]): Node[] {
  return ts.map((t) => {
    if (t.kind === 'plain') return document.createTextNode(t.text);
    const s = document.createElement('span');
    s.className = TOKEN_CLASS[t.kind];
    s.textContent = t.text;
    return s;
  });
}

/** One node of the tree as DOM: an element from the fixed set, or text. */
export function build(n: MdNode): Node {
  if ('text' in n) return document.createTextNode(n.text);
  return element(n);
}

function element(n: MdElement): Node {
  if (!ALLOWED.has(n.tag)) return document.createTextNode('');
  const el = document.createElement(n.tag);
  if (n.tag === 'a') {
    // Checked again here: the tree is the parser's, the page is ours.
    const href = n.href ? safeHref(n.href) : null;
    if (!href) return document.createTextNode(n.children.map(textOf).join(''));
    el.setAttribute('href', href);
    el.setAttribute('rel', 'noopener noreferrer nofollow');
    el.setAttribute('target', '_blank');
    el.setAttribute('referrerpolicy', 'no-referrer');
  }
  if (n.tag === 'pre') {
    const code = document.createElement('code');
    code.append(...tokens(highlight(n.children.map(textOf).join(''), n.lang ? familyOf(n.lang) : null)));
    el.append(code);
    return el;
  }
  if (n.role === 'image') el.className = 'md-image';
  if (n.role === 'task' || n.role === 'done') {
    el.className = 'md-task';
    el.textContent = n.role === 'done' ? '☑ ' : '☐ ';
  }
  if (n.align) el.style.textAlign = n.align;
  el.append(...n.children.map((c) => build(c)));
  return el;
}

function textOf(n: MdNode): string {
  return 'text' in n ? n.text : n.children.map(textOf).join('');
}
