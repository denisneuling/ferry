import { MAX_DEPTH, MD_TAGS, MdElement, MdNode, parseMarkdown, safeHref } from './markdown';

function walk(ns: MdNode[], visit: (e: MdElement, depth: number) => void, depth = 0): void {
  for (const n of ns) {
    if ('text' in n) continue;
    visit(n, depth);
    walk(n.children, visit, depth + 1);
  }
}

/** A compact rendering of a tree, for reading the expectations. */
function show(ns: MdNode[]): string {
  return ns
    .map((n) =>
      'text' in n ? n.text : `<${n.tag}${n.href ? ' ' + n.href : ''}>${show(n.children)}</${n.tag}>`,
    )
    .join('');
}

describe('markdown', () => {
  it('reads blocks and inline marks', () => {
    expect(show(parseMarkdown('# Einkauf\n\nMilch *und* **Brot**, `code` ~~alt~~'))).toBe(
      '<h1>Einkauf</h1><p>Milch <em>und</em> <strong>Brot</strong>, <code>code</code> <del>alt</del></p>',
    );
    expect(show(parseMarkdown('- a\n- [x] b\n  - c\n\n1. eins\n2. zwei'))).toBe(
      '<ul><li>a</li><li><span></span>b<ul><li>c</li></ul></li></ul><ol><li>eins</li><li>zwei</li></ol>',
    );
    expect(show(parseMarkdown('> zitat\n> weiter\n\n---\n\n```go\nfunc x() {}\n```'))).toBe(
      '<blockquote><p>zitat\nweiter</p></blockquote><hr></hr><pre>func x() {}</pre>',
    );
    expect(show(parseMarkdown('| a | b |\n|:--|--:|\n| 1 | 2 |'))).toBe(
      '<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>',
    );
    expect(show(parseMarkdown('Titel\n====='))).toBe('<h1>Titel</h1>');
  });

  it('shows HTML as text and loads no images', () => {
    expect(show(parseMarkdown('<script>alert(1)</script> <img src=x onerror=alert(1)>'))).toBe(
      '<p><script>alert(1)</script> <img src=x onerror=alert(1)></p>',
    );
    const img = parseMarkdown('![Ein Hafen](https://example.org/x.png)');
    expect(show(img)).toBe('<p><span>Ein Hafen</span></p>');
    expect((img[0].children[0] as MdElement).role).toBe('image');
  });

  it('links only to http and https, by what URL parsing says', () => {
    expect(show(parseMarkdown('[ok](https://nas.example.lan/a)'))).toBe(
      '<p><a https://nas.example.lan/a>ok</a></p>',
    );
    for (const bad of [
      'javascript:alert(1)',
      ' JavaScript:alert(1)',
      'JAVASCRIPT:alert(1)',
      'java\tscript:alert(1)',
      '&#106;avascript:alert(1)',
      'data:text/html,<b>x</b>',
      'vbscript:x',
      'file:///etc/passwd',
      '/relative',
      '//evil.example/x',
    ]) {
      const tree = parseMarkdown(`[x](${bad}) <${bad}>`);
      walk(tree, (e) => {
        if (e.tag === 'a') throw new Error(`a link to ${bad}: ${e.href}`);
      });
    }
    expect(show(parseMarkdown('[w](https://en.example/Foo_(bar)) [j](javascript:alert(1)).'))).toBe(
      '<p><a https://en.example/Foo_(bar)>w</a> j.</p>',
    );
    expect(safeHref('HTTPS://Example.org')).toBe('https://example.org/');
    expect(safeHref(' javascript:alert(1)')).toBeNull();
  });

  it('nests only so deep, and shows the rest as text', () => {
    const deep = parseMarkdown('>'.repeat(200) + ' x');
    let max = 0;
    walk(deep, (_, d) => (max = Math.max(max, d)));
    expect(max).toBeLessThanOrEqual(MAX_DEPTH + 1);
    const list = Array.from({ length: 100 }, (_, i) => ' '.repeat(i * 2) + '- x').join('\n');
    let maxList = 0;
    walk(parseMarkdown(list), (_, d) => (maxList = Math.max(maxList, d)));
    expect(maxList).toBeLessThanOrEqual(2 * MAX_DEPTH + 2);
  });

  it('stays quick on marks that close nowhere', () => {
    const t0 = performance.now();
    parseMarkdown('*_~`[<'.repeat(30000));
    parseMarkdown('['.repeat(50000) + '](' + ')'.repeat(50000));
    expect(performance.now() - t0).toBeLessThan(2000);
  });

  it('never yields an element outside the set or a link that is not http(s)', () => {
    // A property test: random text of Markdown's own characters.
    const alphabet = [
      '*',
      '_',
      '~',
      '`',
      '[',
      ']',
      '(',
      ')',
      '<',
      '>',
      '!',
      '#',
      '-',
      '|',
      ':',
      '\n',
      ' ',
      'a',
      '1',
      '.',
      '\\',
      'javascript:',
      'http://x.example/',
      '"',
      '&#',
    ];
    let seed = 42;
    const rnd = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
    for (let k = 0; k < 2000; k++) {
      let s = '';
      const len = Math.floor(rnd() * 80);
      for (let j = 0; j < len; j++) s += alphabet[Math.floor(rnd() * alphabet.length)];
      walk(parseMarkdown(s), (e) => {
        if (!(MD_TAGS as readonly string[]).includes(e.tag))
          throw new Error(`tag ${e.tag} from ${JSON.stringify(s)}`);
        if (e.tag === 'a' && !/^https?:$/.test(new URL(e.href!).protocol)) throw new Error(`href ${e.href}`);
      });
    }
  });
});
