import { decodeText, encodeText } from './text-file';

const bytes = (s: string) => new TextEncoder().encode(s);

describe('text files for editing', () => {
  it('give back what they were read from, line endings and mark included', () => {
    for (const raw of ['plain\nlines\n', 'win\r\nlines\r\n', '﻿marked\n', 'Grüße ☕\n']) {
      const f = decodeText(bytes(raw))!;
      expect(f.text).not.toContain('\r');
      expect(new TextDecoder('utf-8', { ignoreBOM: true }).decode(encodeText(f))).toBe(raw);
    }
  });

  it("keep the file's line endings for what is typed", () => {
    const f = decodeText(bytes('a\r\nb\r\n'))!;
    f.text += 'c\n';
    expect(new TextDecoder().decode(encodeText(f))).toBe('a\r\nb\r\nc\r\n');
  });

  it('refuse what is not UTF-8', () => {
    expect(decodeText(new Uint8Array([0x47, 0x72, 0xfc, 0xdf, 0x65]))).toBeNull(); // Grüße in Windows-1252
  });
});
