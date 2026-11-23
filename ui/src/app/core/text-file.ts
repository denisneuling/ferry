/**
 * A text file as an editor holds it: its text, and what it was written
 * with — a byte-order mark, Windows line endings — so that saving gives
 * the file back as it came, only with the changes made.
 */
export interface TextFile {
  text: string;
  bom: boolean;
  crlf: boolean;
}

/** The most a file may have to be edited in the browser. */
export const MAX_EDIT_BYTES = 1 << 20;

const BOM = [0xef, 0xbb, 0xbf];

/**
 * Reads a file's bytes as UTF-8, or null when they are not: a file in
 * another encoding would be changed by saving it, so it is not edited.
 * The browser's text area keeps only \n; a file that ends its lines with
 * \r\n is written back so.
 */
export function decodeText(bytes: Uint8Array): TextFile | null {
  const bom = bytes.length >= 3 && BOM.every((b, i) => bytes[i] === b);
  let text: string;
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bom ? bytes.subarray(3) : bytes);
  } catch {
    return null;
  }
  const crlf = text.includes('\r\n');
  return { text: crlf ? text.replace(/\r\n/g, '\n') : text, bom, crlf };
}

/** The bytes to save: the text, with the file's own line endings and mark. */
export function encodeText(f: TextFile): Uint8Array {
  const body = new TextEncoder().encode(f.crlf ? f.text.replace(/\r?\n/g, '\r\n') : f.text);
  if (!f.bom) return body;
  const out = new Uint8Array(body.length + 3);
  out.set(BOM);
  out.set(body, 3);
  return out;
}
