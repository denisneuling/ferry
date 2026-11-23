/** A table read from CSV or TSV: its rows, and whether there were more. */
export interface Table {
  rows: string[][];
  more: boolean;
}

/**
 * Reads CSV (comma or semicolon, as spreadsheets in German write it) or
 * TSV into rows of cells, as RFC 4180 has it: quoted cells may hold the
 * separator, quotes doubled, and line breaks. At most maxRows rows; the
 * cells are text, shown as text.
 */
export function parseTable(text: string, tsv: boolean, maxRows = 1000): Table {
  const sep = tsv ? '\t' : guessSeparator(text);
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  let i = 0;
  const end = () => {
    row.push(cell);
    cell = '';
  };
  while (i < text.length) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          cell += '"';
          i += 2;
          continue;
        }
        quoted = false;
      } else {
        cell += c;
      }
      i++;
      continue;
    }
    if (c === '"' && cell === '') {
      quoted = true;
    } else if (c === sep) {
      end();
    } else if (c === '\n' || c === '\r') {
      end();
      rows.push(row);
      row = [];
      if (c === '\r' && text[i + 1] === '\n') i++;
      if (rows.length === maxRows) {
        return { rows, more: i + 1 < text.length };
      }
    } else {
      cell += c;
    }
    i++;
  }
  if (cell !== '' || row.length) {
    end();
    rows.push(row);
  }
  return { rows, more: false };
}

/** A semicolon when the first line has more of them than commas. */
function guessSeparator(text: string): string {
  const first = text.slice(0, text.search(/\r?\n|$/));
  const count = (ch: string) => first.split(ch).length - 1;
  return count(';') > count(',') ? ';' : ',';
}
