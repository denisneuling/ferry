import { checkName } from './dialog';

describe('checkName', () => {
  it('accepts what a share can hold', () => {
    for (const n of ['Neuer Ordner', 'Weihnachten 🎄 2025', 'a.b.c', '..hidden']) {
      expect(checkName(n)).toBeNull();
    }
  });

  it('refuses separators, dots and names too long', () => {
    for (const n of ['', '.', '..', 'a/b', 'a\\b', 'a\u0000b', 'ü'.repeat(200)]) {
      expect(checkName(n)).not.toBeNull();
    }
  });
});
