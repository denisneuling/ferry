import { MD_TAGS, parseMarkdown } from '../core/markdown';
import { build } from './text-view';

describe('text view', () => {
  it('builds only elements from the set, with no attributes but a link’s own', () => {
    const md =
      '# t\n\n<b onclick=x>b</b> [a](https://x.example/) [j](javascript:alert(1)) ![i](https://x.example/i.png)\n\n| a |\n|---|\n| <i>x</i> |\n\n```html\n<script>alert(1)</script>\n```';
    const root = document.createElement('div');
    root.append(...parseMarkdown(md).map((n) => build(n)));
    const allowed = new Set<string>([...MD_TAGS, 'code']);
    for (const el of Array.from(root.querySelectorAll('*'))) {
      expect(allowed.has(el.tagName.toLowerCase())).toBe(true);
      for (const a of Array.from(el.attributes)) {
        expect(['href', 'rel', 'target', 'referrerpolicy', 'class', 'style']).toContain(a.name);
      }
    }
    expect(root.querySelectorAll('script, img, b, i').length).toBe(0);
    const links = Array.from(root.querySelectorAll('a'));
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['https://x.example/']);
    expect(links[0].rel).toContain('noopener');
    expect(root.textContent).toContain('<script>alert(1)</script>');
  });
});
