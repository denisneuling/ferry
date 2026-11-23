/**
 * Subtitles next to a video: `Film.srt`, `Film.vtt`, `Film.de.srt`,
 * `Film.en.forced.vtt`. They are read as text and handed to the video as
 * WebVTT tracks; the browser draws their cues itself, so nothing in them
 * becomes part of ferry's page.
 */

/** One subtitle file found for a video. */
export interface SubtitleFile {
  /** The subtitle file's name in the folder. */
  name: string;
  /** The language its name gives, `de` in `Film.de.srt`; empty when none. */
  lang: string;
  /** What the player's menu shows. */
  label: string;
}

const SUBTITLE_EXT = ['.vtt', '.srt'];

/** At most this many tracks per video: a folder of a series is not a menu. */
export const MAX_TRACKS = 8;

/**
 * The subtitle files among names that belong to the video called video: the
 * same name without its extension, then optionally a language and further
 * tags, then .srt or .vtt — case aside.
 */
export function subtitlesFor(video: string, names: readonly string[]): SubtitleFile[] {
  const dot = video.lastIndexOf('.');
  const base = (dot > 0 ? video.slice(0, dot) : video).toLowerCase();
  const found: SubtitleFile[] = [];
  for (const name of names) {
    const lower = name.toLowerCase();
    const ext = SUBTITLE_EXT.find((x) => lower.endsWith(x));
    if (!ext || !lower.startsWith(base)) continue;
    const middle = name.slice(base.length, name.length - ext.length);
    if (middle !== '' && !middle.startsWith('.')) continue; // Film2.srt is not Film's
    const tags = middle.split('.').filter((t) => t !== '');
    const lang = tags.find((t) => /^[a-z]{2,3}(-[a-z0-9]{2,8})?$/i.test(t)) ?? '';
    found.push({ name, lang: lang.toLowerCase(), label: tags.length ? tags.join(' ') : name });
    if (found.length === MAX_TRACKS) break;
  }
  return found;
}

/**
 * The text of a subtitle file as WebVTT: a .vtt as it is, a SubRip file with
 * the header added and its times written with a dot. A byte-order mark and
 * Windows line ends are dropped first.
 */
export function toWebVTT(text: string, name: string): string {
  const t = text.replace(/^﻿/, '').replace(/\r\n?/g, '\n');
  if (name.toLowerCase().endsWith('.vtt') || t.startsWith('WEBVTT')) return t;
  // A SubRip time is 00:01:02,345; WebVTT's is 00:01:02.345. Only the
  // timing lines change: a comma in a cue's text stays.
  const body = t.replace(
    /^(\d{1,2}:\d{2}:\d{2}),(\d{3}) --> (\d{1,2}:\d{2}:\d{2}),(\d{3})/gm,
    '$1.$2 --> $3.$4',
  );
  return 'WEBVTT\n\n' + body;
}

/** A subtitle file larger than this gets no track: no film needs more. */
export const MAX_SUBTITLE_BYTES = 2 * 1024 * 1024;

/**
 * A subtitle file's bytes as text: UTF-8 where they are UTF-8, and
 * Windows-1252 otherwise, which is what most older SubRip files are in.
 * A UTF-8 byte-order mark is dropped by the decoder.
 */
export function decodeSubtitle(bytes: Uint8Array): string {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
}
