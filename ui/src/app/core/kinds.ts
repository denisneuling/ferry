import { signal } from '@angular/core';

/** What a file is, as the interface treats it: by its name, as the server gives no type. */
export type Kind = 'dir' | 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'sheet' | 'archive' | 'file';

const byExtension: Record<string, Kind> = {};
const add = (kind: Kind, exts: string[]) => exts.forEach((e) => (byExtension[e] = kind));
/** A camera's raw photos, which the server shows by the JPEG preview they carry. */
const rawPhotos = ['cr2', 'nef', 'nrw', 'arw', 'srf', 'sr2', 'dng', 'pef', 'orf', 'rw2', 'raf'];
add('image', [
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'avif',
  'heic',
  'heif',
  'svg',
  'tif',
  'tiff',
  ...rawPhotos,
]);
add('video', ['mp4', 'm4v', 'mov', 'mkv', 'webm', 'avi', 'ogv']);
add('audio', ['mp3', 'm4a', 'aac', 'flac', 'wav', 'ogg', 'oga', 'opus']);
add('pdf', ['pdf']);
add('text', [
  'txt',
  'md',
  'markdown',
  'log',
  'json',
  'yaml',
  'yml',
  'toml',
  'ini',
  'conf',
  'xml',
  'html',
  'htm',
  'css',
  'js',
  'ts',
  'go',
  'py',
  'sh',
  'mjs',
  'jsx',
  'tsx',
  'java',
  'c',
  'h',
  'cpp',
  'hpp',
  'cs',
  'rs',
  'kt',
  'swift',
  'php',
  'rb',
  'pl',
  'sql',
  'ps1',
  'bat',
  'scss',
  'less',
  'vue',
  'lua',
  'properties',
  'env',
]);
add('text', ['docx', 'odt']); // shown as their text (previewKind 'document')
add('sheet', ['csv', 'tsv', 'xlsx', 'xls', 'ods']);
add('archive', ['zip', '7z', 'tar', 'gz', 'tgz', 'rar', 'iso']);

export function extension(name: string): string {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(i + 1).toLowerCase() : '';
}

export function kindOf(name: string, folder: boolean): Kind {
  return folder ? 'dir' : (byExtension[extension(name)] ?? 'file');
}

/**
 * Formats the server makes thumbnails of: images, and recordings by the
 * cover they carry — one without gets an icon, as do HEIC, SVG and video.
 */
const thumbnailed = new Set([
  ...rawPhotos,
  'tif',
  'tiff',
  // Videos by the cover an MP4 or QuickTime file carries (as music does);
  // most cameras write none, and those keep their icon.
  'mp4',
  'm4v',
  'mov',
  // Documents and books, by the picture they carry: OpenDocument almost
  // always, Office now and then, EPUB its cover. Without one, an icon.
  'odt',
  'ods',
  'odp',
  'docx',
  'xlsx',
  'pptx',
  'epub',
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'mp3',
  'm4a',
  'flac',
  'ogg',
  'oga',
  'opus',
]);

/**
 * Formats the server says it makes previews of (`Info.previews`): a
 * deployment may run without them. A signal, so that a folder shown before
 * the server answered asks again once it has.
 */
const previewed = signal<ReadonlySet<string>>(new Set());

/** The extensions each format of `Info.previews` covers. */
const previewExtensions: Record<string, string[]> = { pdf: ['pdf'], heic: ['heic', 'heif'] };

export function setPreviews(formats: readonly string[] | undefined): void {
  previewed.set(new Set((formats ?? []).flatMap((f) => previewExtensions[f] ?? [])));
}

export function hasThumbnail(name: string): boolean {
  const ext = extension(name);
  return thumbnailed.has(ext) || previewed().has(ext);
}

/**
 * How the preview shows a file: the browser's own viewers, or as text;
 * `derived` is a picture the browser cannot show itself (HEIC, TIFF and raw photos, outside
 * Safari), shown as the server's 640-pixel thumbnail.
 */
export type PreviewKind =
  'image' | 'video' | 'audio' | 'pdf' | 'text' | 'document' | 'archive' | 'table' | 'derived' | 'none';

const browserImages = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'avif']);
const browserVideo = new Set(['mp4', 'm4v', 'webm', 'mov', 'ogv']);
const browserAudio = new Set(['mp3', 'm4a', 'aac', 'flac', 'wav', 'ogg', 'oga', 'opus']);

/** An Excel workbook or an OpenDocument spreadsheet, whose first sheet the server reads. */
export function isWorkbook(name: string): boolean {
  const ext = extension(name);
  return ext === 'xlsx' || ext === 'ods';
}

export function previewKind(name: string): PreviewKind {
  const ext = extension(name);
  if (browserImages.has(ext)) return 'image';
  if (browserVideo.has(ext)) return 'video';
  if (browserAudio.has(ext)) return 'audio';
  if (ext === 'pdf') return 'pdf';
  // A Word or OpenDocument text, or a presentation's slides: their plain text, read by the server.
  if (ext === 'docx' || ext === 'odt' || ext === 'pptx' || ext === 'odp') return 'document';
  // CSV and TSV: a table of their first rows; a workbook's first sheet, read by the server.
  if (ext === 'csv' || ext === 'tsv' || isWorkbook(name)) return 'table';
  // A ZIP: what it holds, each entry to open or download.
  if (ext === 'zip') return 'archive';
  if ((ext === 'heic' || ext === 'heif') && previewed().has(ext)) return 'derived';
  // TIFF and raw photos: no browser but Safari shows TIFF, none a raw file.
  if (ext === 'tif' || ext === 'tiff' || rawPhotos.includes(ext)) return 'derived';
  const k = byExtension[ext];
  if (k === 'text' || ext === 'csv' || ext === 'tsv' || ext === 'svg') return 'text';
  return 'none';
}

export const kindIcon: Record<Kind, string> = {
  dir: 'folder',
  image: 'image',
  video: 'video',
  audio: 'audio',
  pdf: 'pdf',
  text: 'text',
  sheet: 'sheet',
  archive: 'archive',
  file: 'file',
};

export function kindLabel(kind: Kind): string {
  switch (kind) {
    case 'dir':
      return $localize`:@@kind.dir:Folder`;
    case 'image':
      return $localize`:@@kind.image:Image`;
    case 'video':
      return $localize`:@@kind.video:Video`;
    case 'audio':
      return $localize`:@@kind.audio:Audio`;
    case 'pdf':
      return $localize`:@@kind.pdf:PDF`;
    case 'text':
      return $localize`:@@kind.text:Text`;
    case 'sheet':
      return $localize`:@@kind.sheet:Spreadsheet`;
    case 'archive':
      return $localize`:@@kind.archive:Archive`;
    default:
      return $localize`:@@kind.file:File`;
  }
}

/** An icon for a share, guessed from its name: the server says nothing more. */
export function shareIcon(name: string, user: string | undefined): string {
  const n = name.toLowerCase();
  if (user && n === user.toLowerCase()) return 'user';
  if (/famil|home|haus|shared|gemeinsam/.test(n)) return 'users';
  if (/public|öffentlich|oeffentlich|guest|gast/.test(n)) return 'globe';
  if (/scan|print|druck/.test(n)) return 'printer';
  if (/photo|foto|bild|picture|image/.test(n)) return 'image';
  if (/music|musik|audio/.test(n)) return 'audio';
  if (/video|film|movie/.test(n)) return 'video';
  return 'drive';
}
