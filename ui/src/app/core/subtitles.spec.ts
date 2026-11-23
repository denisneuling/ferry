import { MAX_TRACKS, decodeSubtitle, subtitlesFor, toWebVTT } from './subtitles';

describe('subtitles', () => {
  it('finds the subtitle files that belong to a video', () => {
    const names = [
      'Film.mkv',
      'Film.srt',
      'Film.de.srt',
      'film.EN.forced.vtt',
      'Film2.srt',
      'Film.de.txt',
      'Other.srt',
    ];
    expect(subtitlesFor('Film.mkv', names)).toEqual([
      { name: 'Film.srt', lang: '', label: 'Film.srt' },
      { name: 'Film.de.srt', lang: 'de', label: 'German' },
      { name: 'film.EN.forced.vtt', lang: 'en', label: 'English forced' },
    ]);
    expect(subtitlesFor('Film.mkv', ['Film.de.srt'], 'de')[0].label).toBe('Deutsch');
    // Three letters that are no language stay as they are.
    expect(subtitlesFor('Film.mkv', ['Film.xyz.srt'])[0].label).toBe('xyz');
    expect(subtitlesFor('Clip.mp4', names)).toEqual([]);
  });

  it('offers a bounded number of tracks', () => {
    const names = Array.from({ length: 20 }, (_, i) => `Film.t${i}x.srt`);
    expect(subtitlesFor('Film.mp4', names).length).toBe(MAX_TRACKS);
  });

  it('turns SubRip into WebVTT and leaves WebVTT as it is', () => {
    const srt =
      '﻿1\r\n00:00:01,500 --> 00:00:03,000\r\nHallo, Welt\r\n\r\n2\r\n01:02:03,004 --> 01:02:04,000\r\n<i>Tschüss</i>\r\n';
    expect(toWebVTT(srt, 'a.srt')).toBe(
      'WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.000\nHallo, Welt\n\n2\n01:02:03.004 --> 01:02:04.000\n<i>Tschüss</i>\n',
    );
    const vtt = 'WEBVTT\n\n00:01.000 --> 00:02.000\nx\n';
    expect(toWebVTT(vtt, 'a.vtt')).toBe(vtt);
  });

  it('reads UTF-8, and Windows-1252 where the bytes are not UTF-8', () => {
    expect(decodeSubtitle(new Uint8Array([0xef, 0xbb, 0xbf, 0x47, 0xc3, 0xbc]))).toBe('Gü');
    expect(decodeSubtitle(new Uint8Array([0x47, 0xfc, 0xdf, 0x80]))).toBe('Güß€');
  });
});
