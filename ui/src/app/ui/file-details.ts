import {
  ChangeDetectionStrategy,
  Component,
  LOCALE_ID,
  computed,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';
import { Api, FileMetadata } from '../core/api';

interface Row {
  label: string;
  value: string;
}

/**
 * What a file says about itself, under the lightbox's details: an image's
 * size and camera, a PDF's pages, a recording's length. The labels are the
 * interface's and translated; the values come out of the file and are shown
 * as text, never as markup. Nothing is there until the server answers, and
 * nothing when it cannot.
 */
@Component({
  selector: 'f-file-details',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { style: 'display: contents' },
  template: `@if (rows().length) {
    <h4 class="lb-info-sub" i18n="@@meta.title">From the file</h4>
    <dl>
      @for (r of rows(); track r.label) {
        <dt>{{ r.label }}</dt>
        <dd>{{ r.value }}</dd>
      }
    </dl>
  }`,
})
export class FileDetails {
  readonly share = input.required<string>();
  readonly path = input.required<string>();
  private readonly api = inject(Api);
  private readonly locale = inject(LOCALE_ID);
  private readonly meta = signal<FileMetadata | null>(null);
  private asked = 0;

  constructor() {
    effect(() => {
      const share = this.share();
      const path = this.path();
      const n = ++this.asked;
      this.meta.set(null);
      this.api.metadata(share, path).then(
        (m) => n === this.asked && this.meta.set(m),
        () => undefined,
      );
    });
  }

  protected readonly rows = computed<Row[]>(() => {
    const m = this.meta();
    if (!m) return [];
    const rows: Row[] = [];
    const add = (label: string, value: string | null | undefined) => {
      if (value) rows.push({ label, value });
    };
    const num = (n: number, digits = 0) => n.toLocaleString(this.locale, { maximumFractionDigits: digits });

    add($localize`:@@meta.format:Format`, m.format);
    if (m.width && m.height) {
      const mp = (m.width * m.height) / 1e6;
      add(
        $localize`:@@meta.dimensions:Dimensions`,
        `${num(m.width)} × ${num(m.height)}` + (m.kind === 'image' && mp >= 1 ? ` (${num(mp, 1)} MP)` : ''),
      );
    }
    if (m.taken) add($localize`:@@meta.taken:Taken`, this.when(m.taken));
    add($localize`:@@meta.camera:Camera`, m.camera);
    add($localize`:@@meta.lens:Lens`, m.lens);
    const exposure = [
      m.fNumber ? `f/${num(m.fNumber, 1)}` : '',
      m.exposure ? `${m.exposure} s` : '',
      m.iso ? `ISO ${m.iso}` : '',
      m.focalLength ? `${num(m.focalLength, 1)} mm` : '',
    ].filter(Boolean);
    if (exposure.length) add($localize`:@@meta.exposure:Exposure`, exposure.join(' · '));
    if (m.orientation && m.orientation !== 1)
      add($localize`:@@meta.orientation:Orientation`, this.orientation(m.orientation));
    if (m.location) {
      add(
        $localize`:@@meta.gps:GPS position`,
        `${num(m.location.latitude, 6)}, ${num(m.location.longitude, 6)}`,
      );
    }
    if (m.pages) add($localize`:@@meta.pages:Pages`, num(m.pages));
    add($localize`:@@meta.docTitle:Title`, m.title);
    add(m.kind === 'pdf' ? $localize`:@@meta.author:Author` : $localize`:@@meta.artist:Artist`, m.author);
    add($localize`:@@meta.album:Album`, m.album);
    add($localize`:@@meta.producer:Made with`, m.producer);
    if (m.durationSeconds) add($localize`:@@meta.duration:Duration`, duration(m.durationSeconds));
    add($localize`:@@meta.codec:Codec`, m.codec);
    if (m.bitrate) {
      add(
        $localize`:@@meta.bitrate:Bitrate`,
        m.bitrate >= 1e6 ? `${num(m.bitrate / 1e6, 1)} Mbit/s` : `${num(m.bitrate / 1e3)} kbit/s`,
      );
    }
    if (m.sampleRate) add($localize`:@@meta.sampleRate:Sample rate`, `${num(m.sampleRate / 1000, 2)} kHz`);
    if (m.channels) add($localize`:@@meta.channels:Channels`, this.channels(m.channels));
    if (m.encoding) add($localize`:@@meta.encoding:Encoding`, this.encoding(m.encoding));
    if (m.lines != null) {
      const lines = m.linesAtLeast
        ? $localize`:@@meta.linesAtLeast:at least ${num(m.lines)}:n:`
        : num(m.lines);
      add($localize`:@@meta.lines:Lines`, m.lineEndings ? `${lines} (${m.lineEndings})` : lines);
    }
    return rows;
  });

  /**
   * A camera's time as the camera's clock showed it where the photo was
   * taken — not turned into the viewer's zone, which would move a sunset to
   * the afternoon.
   */
  private when(taken: string): string {
    const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)/.exec(taken);
    if (!m) return taken;
    const [, y, mo, d, h, mi, s] = m.map(Number);
    const wall = new Date(Date.UTC(y, mo - 1, d, h, mi, s));
    return wall.toLocaleString(this.locale, { dateStyle: 'medium', timeStyle: 'short', timeZone: 'UTC' });
  }

  private orientation(o: number): string {
    switch (o) {
      case 2:
        return $localize`:@@meta.or2:Mirrored`;
      case 3:
        return $localize`:@@meta.or3:Turned 180°`;
      case 4:
        return $localize`:@@meta.or4:Turned 180°, mirrored`;
      case 5:
        return $localize`:@@meta.or5:Turned 90° counter-clockwise, mirrored`;
      case 6:
        return $localize`:@@meta.or6:Turned 90° clockwise`;
      case 7:
        return $localize`:@@meta.or7:Turned 90° clockwise, mirrored`;
      case 8:
        return $localize`:@@meta.or8:Turned 90° counter-clockwise`;
    }
    return String(o);
  }

  private channels(n: number): string {
    if (n === 1) return $localize`:@@meta.mono:Mono`;
    if (n === 2) return $localize`:@@meta.stereo:Stereo`;
    return String(n);
  }

  private encoding(e: NonNullable<FileMetadata['encoding']>): string {
    switch (e) {
      case 'ascii':
        return 'ASCII';
      case 'utf-8':
        return 'UTF-8';
      case 'utf-8-bom':
        return $localize`:@@meta.utf8bom:UTF-8 with byte-order mark`;
      case 'utf-16le':
        return 'UTF-16 LE';
      case 'utf-16be':
        return 'UTF-16 BE';
      case '8bit':
        return $localize`:@@meta.8bit:Not UTF-8 — an 8-bit code page, likely Windows-1252`;
    }
  }
}

/** A length as clocks show it: 3:07, 1:02:45. */
export function duration(seconds: number): string {
  const s = Math.round(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}
