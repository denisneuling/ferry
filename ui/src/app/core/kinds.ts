/** What a file is, as the interface treats it: by its name, as the server gives no type. */
export type Kind = 'dir' | 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'sheet' | 'archive' | 'file';

const byExtension: Record<string, Kind> = {};
const add = (kind: Kind, exts: string[]) => exts.forEach((e) => (byExtension[e] = kind));
add('image', ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'avif', 'heic', 'heif', 'svg', 'tif', 'tiff']);
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
]);
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

export function hasThumbnail(name: string): boolean {
  return thumbnailed.has(extension(name));
}

/** How the preview shows a file: the browser's own viewers, or as text. */
export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'none';

const browserImages = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'avif']);
const browserVideo = new Set(['mp4', 'm4v', 'webm', 'mov', 'ogv']);
const browserAudio = new Set(['mp3', 'm4a', 'aac', 'flac', 'wav', 'ogg', 'oga', 'opus']);

export function previewKind(name: string): PreviewKind {
  const ext = extension(name);
  if (browserImages.has(ext)) return 'image';
  if (browserVideo.has(ext)) return 'video';
  if (browserAudio.has(ext)) return 'audio';
  if (ext === 'pdf') return 'pdf';
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
