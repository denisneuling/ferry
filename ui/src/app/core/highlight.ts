/**
 * Colours for source code in the text preview: comments, strings, numbers
 * and keywords, found by one linear scan per language family — enough to
 * read by, not a parser. The tokens are text, shown as text with a class
 * from a fixed set (ui/text-view.ts); nothing of the file becomes markup.
 */

export type TokenKind = 'comment' | 'string' | 'number' | 'keyword' | 'tag' | 'plain';
export interface Token {
  kind: TokenKind;
  text: string;
}

interface Family {
  line: string[]; // line comments
  block: [string, string][]; // block comments
  quotes: string[];
  keywords: ReadonlySet<string>;
  /** Markup: <tags> and their attributes. */
  markup?: boolean;
}

const words = (s: string) => new Set(s.split(/\s+/).filter(Boolean));

const C_KEYWORDS = words(`
  if else for while do switch case default break continue return goto
  function var let const new delete typeof instanceof in of void this super class extends implements
  interface enum type import export from as async await yield static public private protected
  readonly abstract final try catch finally throw throws true false null undefined nil
  package func go chan select defer map struct range fallthrough
  fn mut pub impl trait use mod crate self Self match loop where unsafe move ref
  int long short char float double bool boolean string byte uint void sizeof typedef
  namespace using template typename virtual override operator friend inline extern
  val fun object when is companion data sealed init lateinit
`);

const FAMILIES: Record<string, Family> = {
  c: { line: ['//'], block: [['/*', '*/']], quotes: ['"', "'", '`'], keywords: C_KEYWORDS },
  hash: {
    line: ['#'],
    block: [],
    quotes: ['"', "'"],
    keywords: words(`
      if elif else fi then for while until do done case esac in function return local export
      def class import from as pass lambda with yield raise try except finally not and or is
      None True False self global nonlocal async await
      begin end module require unless rescue ensure true false nil
    `),
  },
  sql: {
    line: ['--'],
    block: [['/*', '*/']],
    quotes: ["'", '"'],
    keywords: words(`
      select from where and or not insert into values update set delete create table drop alter
      index view join left right inner outer on group by order having limit offset as distinct
      union all null is in like between case when then else end primary key foreign references
    `),
  },
  data: {
    line: ['#'],
    block: [],
    quotes: ['"', "'"],
    keywords: words('true false null yes no on off'),
  },
  json: { line: [], block: [], quotes: ['"'], keywords: words('true false null') },
  css: { line: [], block: [['/*', '*/']], quotes: ['"', "'"], keywords: words('important') },
  markup: { line: [], block: [['<!--', '-->']], quotes: ['"', "'"], keywords: new Set(), markup: true },
};

const BY_EXT: Record<string, string> = {
  js: 'c',
  mjs: 'c',
  cjs: 'c',
  ts: 'c',
  jsx: 'c',
  tsx: 'c',
  java: 'c',
  c: 'c',
  h: 'c',
  cpp: 'c',
  hpp: 'c',
  cc: 'c',
  cs: 'c',
  go: 'c',
  rs: 'c',
  kt: 'c',
  kts: 'c',
  swift: 'c',
  php: 'c',
  scala: 'c',
  dart: 'c',
  gradle: 'c',
  py: 'hash',
  sh: 'hash',
  bash: 'hash',
  zsh: 'hash',
  rb: 'hash',
  pl: 'hash',
  r: 'hash',
  ps1: 'hash',
  mk: 'hash',
  sql: 'sql',
  yaml: 'data',
  yml: 'data',
  toml: 'data',
  ini: 'data',
  conf: 'data',
  properties: 'data',
  env: 'data',
  json: 'json',
  css: 'css',
  scss: 'c',
  less: 'c',
  html: 'markup',
  htm: 'markup',
  xml: 'markup',
  svg: 'markup',
  vue: 'markup',
};

/** The language family a fence's name or a file's extension names, if any. */
export function familyOf(langOrExt: string): string | null {
  const k = langOrExt.toLowerCase();
  const alias: Record<string, string> = {
    javascript: 'js',
    typescript: 'ts',
    python: 'py',
    shell: 'sh',
    golang: 'go',
    rust: 'rs',
    kotlin: 'kt',
    ruby: 'rb',
    csharp: 'cs',
    'c++': 'cpp',
  };
  return BY_EXT[alias[k] ?? k] ?? null;
}

/** Above this the text is shown uncoloured: colouring is a help, not a duty. */
export const MAX_HIGHLIGHT = 512 * 1024;

export function highlight(src: string, family: string | null): Token[] {
  const f = family ? FAMILIES[family] : undefined;
  if (!f || src.length > MAX_HIGHLIGHT) return [{ kind: 'plain', text: src }];
  const out: Token[] = [];
  let plain = '';
  const emit = (kind: TokenKind, text: string) => {
    if (kind === 'plain') {
      plain += text;
      return;
    }
    if (plain) out.push({ kind: 'plain', text: plain });
    plain = '';
    out.push({ kind, text });
  };
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];
    const lc = f.line.find((m) => src.startsWith(m, i));
    if (lc !== undefined) {
      const end = src.indexOf('\n', i);
      const stop = end < 0 ? n : end;
      emit('comment', src.slice(i, stop));
      i = stop;
      continue;
    }
    const bc = f.block.find(([open]) => src.startsWith(open, i));
    if (bc) {
      const end = src.indexOf(bc[1], i + bc[0].length);
      const stop = end < 0 ? n : end + bc[1].length;
      emit('comment', src.slice(i, stop));
      i = stop;
      continue;
    }
    if (f.markup && c === '<' && /[a-zA-Z/!?]/.test(src[i + 1] ?? '')) {
      // A tag's name; its attributes' values come as strings below.
      let j = i + 1;
      while (j < n && /[\w:/!?.-]/.test(src[j])) j++;
      emit('tag', src.slice(i, j));
      i = j;
      continue;
    }
    if (f.quotes.includes(c)) {
      // To the closing quote, past escapes; a line's end ends all but backticks.
      let j = i + 1;
      while (j < n && src[j] !== c && (c === '`' || src[j] !== '\n')) j += src[j] === '\\' ? 2 : 1;
      const stop = Math.min(n, j + 1);
      emit('string', src.slice(i, stop));
      i = stop;
      continue;
    }
    if (/[0-9]/.test(c) && !/[\w$]/.test(src[i - 1] ?? '')) {
      let j = i + 1;
      while (j < n && /[0-9a-fA-FxXoObB._]/.test(src[j])) j++;
      emit('number', src.slice(i, j));
      i = j;
      continue;
    }
    if (/[A-Za-z_$]/.test(c) && !/[\w$]/.test(src[i - 1] ?? '')) {
      let j = i + 1;
      while (j < n && /[\w$]/.test(src[j])) j++;
      const w = src.slice(i, j);
      emit(f.keywords.has(family === 'sql' ? w.toLowerCase() : w) ? 'keyword' : 'plain', w);
      i = j;
      continue;
    }
    emit('plain', c);
    i++;
  }
  if (plain) out.push({ kind: 'plain', text: plain });
  return out;
}
