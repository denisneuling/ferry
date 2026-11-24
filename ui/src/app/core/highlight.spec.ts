import { familyOf, highlight } from './highlight';

describe('highlight', () => {
  it('finds comments, strings, numbers and keywords', () => {
    const ts = highlight('const x = "a//b"; // note\nreturn 42;', familyOf('ts'));
    expect(ts.filter((t) => t.kind !== 'plain').map((t) => `${t.kind}:${t.text}`)).toEqual([
      'keyword:const',
      'string:"a//b"',
      'comment:// note',
      'keyword:return',
      'number:42',
    ]);
    expect(highlight('SELECT 1 -- x', familyOf('sql'))[0]).toEqual({ kind: 'keyword', text: 'SELECT' });
    expect(highlight('<a href="x">', familyOf('html')).map((t) => t.kind)).toEqual([
      'tag',
      'plain',
      'string',
      'plain',
    ]);
  });

  it('keeps every character, and leaves unknown languages plain', () => {
    const src = 'def f(x):\n  return "unterminated\n# c';
    expect(
      highlight(src, familyOf('py'))
        .map((t) => t.text)
        .join(''),
    ).toBe(src);
    expect(highlight('x', familyOf('nope'))).toEqual([{ kind: 'plain', text: 'x' }]);
  });

  it('is linear on what never closes', () => {
    const t0 = performance.now();
    highlight('/*' + '"'.repeat(200000), familyOf('js'));
    highlight('"\\'.repeat(100000), familyOf('js'));
    expect(performance.now() - t0).toBeLessThan(1500);
  });
});
