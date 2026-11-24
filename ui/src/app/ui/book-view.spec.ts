import { BookNode } from '../core/api';
import { buildBookNode } from './book-view';

describe('book view', () => {
  it('builds only known elements, and pictures only from the archive', () => {
    const tree = {
      tag: 'div',
      children: [
        {
          tag: 'p',
          children: [{ text: '<b>nicht fett</b>' }, { tag: 'em', children: [{ text: 'kursiv' }] }],
        },
        { tag: 'script' as never, children: [{ text: 'alert(1)' }] },
        { tag: 'img', src: 'OEBPS/images/a.jpg', alt: 'Bild' },
        { tag: 'img', alt: 'ohne Quelle' },
      ],
    } as BookNode;
    const root = buildBookNode(tree, (src) => `/zip/entry?entry=${encodeURIComponent(src)}`) as HTMLElement;
    expect(root.querySelectorAll('script, b').length).toBe(0);
    expect(root.textContent).toContain('<b>nicht fett</b>');
    expect(root.textContent).not.toContain('alert');
    const imgs = root.querySelectorAll('img');
    expect(imgs.length).toBe(1);
    expect(imgs[0].getAttribute('src')).toBe('/zip/entry?entry=OEBPS%2Fimages%2Fa.jpg');
    expect(root.textContent).toContain('ohne Quelle');
  });
});
