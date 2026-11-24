/**
 * Markdown, read into a tree of a few known elements and plain text. The
 * tree is turned into DOM nodes one by one (ui/text-view.ts): no HTML is
 * ever parsed, so nothing a file holds becomes markup — inline HTML shows
 * as the text it is. Images are not loaded, only their description shown;
 * a link points only where URL parsing says http or https.
 *
 * It reads CommonMark's common part and GitHub's tables, task lists and
 * strikethrough; not every corner of either. Lists and quotes nest at most
 * MAX_DEPTH deep — deeper ones are shown as text — and the scan of a
 * paragraph's inline marks is linear, so no file makes it slow.
 */

/** The elements a tree may hold, and nothing else. */
export const MD_TAGS = [
  'p',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'ul',
  'ol',
  'li',
  'blockquote',
  'pre',
  'code',
  'em',
  'strong',
  'del',
  'a',
  'hr',
  'br',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
  'span',
] as const;
export type MdTag = (typeof MD_TAGS)[number];

export interface MdText {
  text: string;
}
export interface MdElement {
  tag: MdTag;
  children: MdNode[];
  /** a: the link, checked to be http or https. */
  href?: string;
  /** pre: the language its fence named. */
  lang?: string;
  /** span: what it is — an image's description, or a task's box. */
  role?: 'image' | 'task' | 'done';
  /** th, td: the column's alignment. */
  align?: 'left' | 'center' | 'right';
}
export type MdNode = MdText | MdElement;

export const MAX_DEPTH = 32;
/** The longest link destination read; a longer one is text. */
const MAX_DEST = 2048;

/** The link a destination makes, or null when it is not http(s). */
export function safeHref(dest: string): string | null {
  try {
    const u = new URL(dest.trim());
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : null;
  } catch {
    return null; // relative, or no URL at all: shown as text
  }
}

export function parseMarkdown(src: string): MdElement[] {
  const lines = src.replace(/\r\n?/g, '\n').replace(/\t/g, '    ').split('\n');
  return blocks(lines, 0);
}

const HR = /^ {0,3}([-*_])(?: *\1){2,} *$/;
const ATX = /^ {0,3}(#{1,6})(?: +(.*?))?(?: +#+)? *$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)[^`]*$/;
const QUOTE = /^ {0,3}> ?(.*)$/;
const BULLET = /^( {0,3})([-*+])( +)(.*)$/;
const ORDERED = /^( {0,3})(\d{1,9})([.)])( +)(.*)$/;
const TABLE_SEP = /^ *\|? *:?-{1,}:? *(\| *:?-{1,}:? *)*\|? *$/;

function blocks(lines: string[], depth: number): MdElement[] {
  const out: MdElement[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) out.push({ tag: 'p', children: inline(para.join('\n'), 0) });
    para = [];
  };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === '') {
      flush();
      i++;
      continue;
    }
    let m: RegExpMatchArray | null;
    if ((m = line.match(FENCE))) {
      flush();
      const fence = m[1];
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trimStart().startsWith(fence)) body.push(lines[i++]);
      i++; // the closing fence, or the end
      out.push({ tag: 'pre', lang: m[2].toLowerCase(), children: [{ text: body.join('\n') }] });
      continue;
    }
    if (para.length === 0 && /^ {4}/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && (/^ {4}/.test(lines[i]) || lines[i].trim() === ''))
        body.push(lines[i++].slice(4));
      while (body.length && body[body.length - 1].trim() === '') body.pop();
      out.push({ tag: 'pre', children: [{ text: body.join('\n') }] });
      continue;
    }
    if ((m = line.match(ATX))) {
      flush();
      out.push({ tag: `h${m[1].length}` as MdTag, children: inline(m[2] ?? '', 0) });
      i++;
      continue;
    }
    if (HR.test(line)) {
      flush();
      out.push({ tag: 'hr', children: [] });
      i++;
      continue;
    }
    if (para.length && /^ {0,3}(=+|-+) *$/.test(line)) {
      // A setext heading: the paragraph above, underlined.
      out.push({ tag: line.trim()[0] === '=' ? 'h1' : 'h2', children: inline(para.join('\n'), 0) });
      para = [];
      i++;
      continue;
    }
    if (QUOTE.test(line)) {
      flush();
      const body: string[] = [];
      while (i < lines.length && lines[i].trim() !== '' && (QUOTE.test(lines[i]) || body.length)) {
        const q = lines[i].match(QUOTE);
        body.push(q ? q[1] : lines[i]);
        i++;
      }
      out.push(
        depth < MAX_DEPTH
          ? { tag: 'blockquote', children: blocks(body, depth + 1) }
          : { tag: 'p', children: [{ text: body.join('\n') }] },
      );
      continue;
    }
    if (BULLET.test(line) || ORDERED.test(line)) {
      flush();
      const [list, next] = listAt(lines, i, depth);
      out.push(list);
      i = next;
      continue;
    }
    if (
      line.includes('|') &&
      i + 1 < lines.length &&
      TABLE_SEP.test(lines[i + 1]) &&
      lines[i + 1].includes('-')
    ) {
      flush();
      const [table, next] = tableAt(lines, i);
      out.push(table);
      i = next;
      continue;
    }
    para.push(line);
    i++;
  }
  flush();
  return out;
}

/** A list from line i: its items, each with the lines indented under it. */
function listAt(lines: string[], start: number, depth: number): [MdElement, number] {
  const first = lines[start];
  const ordered = ORDERED.test(first);
  const items: MdElement[] = [];
  let i = start;
  while (i < lines.length) {
    const m = ordered ? lines[i].match(ORDERED) : lines[i].match(BULLET);
    if (!m) break;
    const indent = ordered
      ? m[1].length + m[2].length + m[3].length + m[4].length
      : m[1].length + 1 + m[3].length;
    const body = [ordered ? m[5] : m[4]];
    i++;
    while (i < lines.length) {
      const l = lines[i];
      if (l.trim() === '') {
        // A blank line ends the item unless indented text follows it.
        if (i + 1 < lines.length && lines[i + 1].startsWith(' '.repeat(indent))) {
          body.push('');
          i++;
          continue;
        }
        break;
      }
      if (l.startsWith(' '.repeat(indent))) body.push(l.slice(indent));
      else if (!BULLET.test(l) && !ORDERED.test(l) && !HR.test(l) && !QUOTE.test(l) && !ATX.test(l))
        body.push(l);
      else break;
      i++;
    }
    items.push(item(body, depth));
    if (i < lines.length && lines[i].trim() === '') {
      const n = lines[i + 1];
      if (n !== undefined && (ordered ? ORDERED.test(n) : BULLET.test(n))) i++;
    }
  }
  return [{ tag: ordered ? 'ol' : 'ul', children: items }, i];
}

function item(body: string[], depth: number): MdElement {
  const task = body[0].match(/^\[([ xX])\] +(.*)$/);
  const box: MdNode[] = [];
  if (task) {
    box.push({ tag: 'span', role: task[1] === ' ' ? 'task' : 'done', children: [] });
    body = [task[2], ...body.slice(1)];
  }
  if (depth + 1 >= MAX_DEPTH) return { tag: 'li', children: [...box, { text: body.join('\n') }] };
  const inner = blocks(body, depth + 1);
  // A tight item — no blank line in it — holds its text, not paragraphs.
  const tight = !body.includes('');
  const kids: MdNode[] = tight ? inner.flatMap((b) => (b.tag === 'p' ? b.children : [b])) : inner;
  return { tag: 'li', children: [...box, ...kids] };
}

function cells(line: string): string[] {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);
  const out: string[] = [];
  let cur = '';
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '\\' && s[i + 1] === '|') {
      cur += '|';
      i++;
    } else if (s[i] === '|') {
      out.push(cur.trim());
      cur = '';
    } else cur += s[i];
  }
  out.push(cur.trim());
  return out;
}

const MAX_TABLE_COLS = 64;

function tableAt(lines: string[], start: number): [MdElement, number] {
  const head = cells(lines[start]).slice(0, MAX_TABLE_COLS);
  const aligns = cells(lines[start + 1]).map((c) =>
    c.startsWith(':') && c.endsWith(':')
      ? 'center'
      : c.endsWith(':')
        ? 'right'
        : c.startsWith(':')
          ? 'left'
          : undefined,
  );
  const row = (cs: string[], tag: 'th' | 'td'): MdElement => ({
    tag: 'tr',
    children: head.map((_, k) => ({ tag, align: aligns[k], children: inline(cs[k] ?? '', 0) })),
  });
  const body: MdElement[] = [];
  let i = start + 2;
  while (i < lines.length && lines[i].trim() !== '' && lines[i].includes('|'))
    body.push(row(cells(lines[i++]), 'td'));
  return [
    {
      tag: 'table',
      children: [
        { tag: 'thead', children: [row(head, 'th')] },
        { tag: 'tbody', children: body },
      ],
    },
    i,
  ];
}

/**
 * The inline marks of a paragraph. Each kind of closing mark is looked for
 * at most once past any point (`next` remembers where it is, or that it is
 * not there), so a paragraph is scanned in linear time however many marks
 * it holds that close nowhere.
 */
function inline(s: string, depth: number): MdNode[] {
  const out: MdNode[] = [];
  let text = '';
  const push = (n: MdNode) => {
    if (text) out.push({ text });
    text = '';
    out.push(n);
  };
  const seen = new Map<string, number>(); // closer → its next position, -1 for none
  const find = (closer: string, from: number): number => {
    const known = seen.get(closer);
    if (known !== undefined && (known === -1 || known >= from)) return known;
    const at = s.indexOf(closer, from);
    seen.set(closer, at);
    return at;
  };
  const deeper = (t: string): MdNode[] => (depth < MAX_DEPTH ? inline(t, depth + 1) : [{ text: t }]);
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length && /[!-/:-@[-`{-~]/.test(s[i + 1])) {
      text += s[i + 1];
      i += 2;
      continue;
    }
    if (c === '\\' && s[i + 1] === '\n') {
      push({ tag: 'br', children: [] });
      i += 2;
      continue;
    }
    if (c === '\n') {
      if (text.endsWith('  ')) {
        text = text.trimEnd();
        push({ tag: 'br', children: [] });
      } else text += '\n';
      i++;
      continue;
    }
    if (c === '`') {
      let n = 1;
      while (s[i + n] === '`') n++;
      const ticks = '`'.repeat(n);
      const end = find(ticks, i + n);
      if (end > 0) {
        push({
          tag: 'code',
          children: [
            {
              text:
                s
                  .slice(i + n, end)
                  .replace(/\n/g, ' ')
                  .trim() || ' ',
            },
          ],
        });
        i = end + n;
        continue;
      }
      text += ticks;
      i += n;
      continue;
    }
    if (c === '!' && s[i + 1] === '[') {
      const l = link(s, i + 1, find);
      if (l) {
        push({ tag: 'span', role: 'image', children: [{ text: l.label || l.dest }] });
        i = l.end;
        continue;
      }
    }
    if (c === '[') {
      const l = link(s, i, find);
      if (l) {
        const href = safeHref(l.dest);
        if (href) push({ tag: 'a', href, children: deeper(l.label) });
        else out.push(...flushText(), ...deeper(l.label));
        i = l.end;
        continue;
      }
    }
    if (c === '<') {
      const end = find('>', i + 1);
      const inside = end > 0 ? s.slice(i + 1, end) : '';
      const href = /^[a-z][a-z0-9+.-]*:[^\s<>]*$/i.test(inside) ? safeHref(inside) : null;
      if (href) {
        push({ tag: 'a', href, children: [{ text: inside }] });
        i = end + 1;
        continue;
      }
    }
    if ((c === '*' || c === '_' || c === '~') && s[i + 1] === c) {
      const mark = c + c;
      const end = find(mark, i + 2);
      if (end > i + 2 && s[i + 2] !== ' ') {
        push({ tag: c === '~' ? 'del' : 'strong', children: deeper(s.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
      text += mark;
      i += 2;
      continue;
    }
    if ((c === '*' || c === '_') && s[i + 1] !== ' ' && !(c === '_' && /\w/.test(s[i - 1] ?? ''))) {
      const end = find(c, i + 1);
      if (end > i + 1 && !(c === '_' && /\w/.test(s[end + 1] ?? ''))) {
        push({ tag: 'em', children: deeper(s.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }
    text += c;
    i++;
  }
  if (text) out.push({ text });
  return out;

  function flushText(): MdNode[] {
    const t = text;
    text = '';
    return t ? [{ text: t }] : [];
  }
}

/** A link or image at i: [label](dest "title"). */
function link(
  s: string,
  i: number,
  find: (closer: string, from: number) => number,
): { label: string; dest: string; end: number } | null {
  const close = find('](', i + 1);
  if (close < 0) return null;
  const label = s.slice(i + 1, close);
  // A bracket inside is another link's, or none: this one is text.
  if (label.includes('\n\n') || label.includes('[') || label.includes(']')) return null;
  // The destination ends at its own closing parenthesis: those inside it
  // pair up, as in (https://en.example/Foo_(bar)). Looked for within a
  // bounded stretch, so this stays linear.
  let end = -1;
  for (let j = close + 2, open = 0; j < s.length && j < close + 2 + MAX_DEST; j++) {
    const ch = s[j];
    if (ch === '\n') break;
    if (ch === '(') open++;
    else if (ch === ')' && open-- === 0) {
      end = j;
      break;
    }
  }
  if (end < 0) return null;
  const dest = s
    .slice(close + 2, end)
    .trim()
    .replace(/\s+"[^"]*"$/, '')
    .replace(/^<(.*)>$/, '$1');
  return { label, dest, end: end + 1 };
}
