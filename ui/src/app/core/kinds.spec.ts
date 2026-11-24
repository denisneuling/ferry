import { hasThumbnail, kindOf, previewKind, setPreviews, shareIcon } from './kinds';

describe('kinds', () => {
  it('tells files apart by their extension, case aside', () => {
    expect(kindOf('IMG_0001.JPG', false)).toBe('image');
    expect(kindOf('Urlaub 🌊.mp4', false)).toBe('video');
    expect(kindOf('Steuern.pdf', false)).toBe('pdf');
    expect(kindOf('README', false)).toBe('file');
    expect(kindOf('.bashrc', false)).toBe('file');
    expect(kindOf('Photos', true)).toBe('dir');
  });

  it('asks the server for thumbnails only of what it can make them of', () => {
    expect(hasThumbnail('a.jpeg')).toBe(true);
    expect(hasThumbnail('a.webp')).toBe(true);
    expect(hasThumbnail('a.heic')).toBe(false);
    expect(hasThumbnail('DSC_0042.NEF')).toBe(true);
    expect(hasThumbnail('scan.tiff')).toBe(true);
    expect(previewKind('IMG_0001.CR2')).toBe('derived');
    expect(previewKind('DSCF1234.RAF')).toBe('derived');
    expect(previewKind('scan.tif')).toBe('derived');
    expect(hasThumbnail('Brief.odt')).toBe(true);
    expect(hasThumbnail('book.epub')).toBe(true);
    expect(hasThumbnail('film.mp4')).toBe(true);
    expect(hasThumbnail('clip.mkv')).toBe(false);
    expect(previewKind('Brief.docx')).toBe('document');
    expect(previewKind('Brief.odt')).toBe('document');
    expect(previewKind('slides.pptx')).toBe('document');
    expect(previewKind('Vortrag.odp')).toBe('document');
    expect(previewKind('alt.ppt')).toBe('none');
    expect(previewKind('book.epub')).toBe('book');
    expect(previewKind('Haushalt.XLSX')).toBe('table');
    expect(previewKind('Haushalt.ods')).toBe('table');
    expect(previewKind('alt.xls')).toBe('none');
    expect(hasThumbnail('a.svg')).toBe(false);
    // Recordings by their cover; the server answers 415 for one without.
    expect(hasThumbnail('a.mp3')).toBe(true);
    expect(hasThumbnail('a.FLAC')).toBe(true);
    expect(hasThumbnail('a.opus')).toBe(true);
    expect(hasThumbnail('a.wav')).toBe(false);
    // PDFs only when the server makes their previews.
    expect(hasThumbnail('a.pdf')).toBe(false);
    setPreviews(['pdf']);
    expect(hasThumbnail('a.PDF')).toBe(true);
    setPreviews(undefined);
    expect(hasThumbnail('a.pdf')).toBe(false);
    // HEIC photos: a thumbnail, and the server's 640 as their preview.
    expect(previewKind('IMG_2117.HEIC')).toBe('none');
    setPreviews(['heic']);
    expect(hasThumbnail('IMG_2117.HEIC')).toBe(true);
    expect(hasThumbnail('x.heif')).toBe(true);
    expect(previewKind('IMG_2117.HEIC')).toBe('derived');
    setPreviews([]);
  });

  it('previews HTML and SVG as text, never rendered', () => {
    expect(previewKind('page.html')).toBe('text');
    expect(previewKind('logo.svg')).toBe('text');
    expect(previewKind('photo.png')).toBe('image');
    expect(previewKind('song.flac')).toBe('audio');
    expect(previewKind('archive.zip')).toBe('archive'); // its contents, listed
    expect(previewKind('backup.7z')).toBe('none');
  });

  it('guesses share icons from their names', () => {
    expect(shareIcon('alex', 'alex')).toBe('user');
    expect(shareIcon('Family', 'alex')).toBe('users');
    expect(shareIcon('Public', 'alex')).toBe('globe');
    expect(shareIcon('Scans', 'alex')).toBe('printer');
    expect(shareIcon('Backup', 'alex')).toBe('drive');
  });
});
