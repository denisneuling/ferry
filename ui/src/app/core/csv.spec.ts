import { parseTable } from './csv';

describe('parseTable', () => {
  it('reads cells, quotes, doubled quotes and line breaks in quotes', () => {
    const t = parseTable(
      'Monat,Betrag,Notiz\r\nJanuar,"1.200,50","sagt ""danke""\nund mehr"\nFebruar,90,\n',
      false,
    );
    expect(t.rows).toEqual([
      ['Monat', 'Betrag', 'Notiz'],
      ['Januar', '1.200,50', 'sagt "danke"\nund mehr'],
      ['Februar', '90', ''],
    ]);
    expect(t.more).toBe(false);
  });

  it('takes the semicolon a German spreadsheet writes, and tabs for TSV', () => {
    expect(parseTable('a;b,c;d\n1;2', false).rows).toEqual([
      ['a', 'b,c', 'd'],
      ['1', '2'],
    ]);
    expect(parseTable('a\tb\n1\t2', true).rows).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ]);
  });

  it('stops at the bound and says there is more', () => {
    const t = parseTable('x\n'.repeat(50), false, 10);
    expect(t.rows.length).toBe(10);
    expect(t.more).toBe(true);
  });
});
