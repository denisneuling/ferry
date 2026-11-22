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
    expect(hasThumbnail('a.svg')).toBe(false);
    expect(hasThumbnail('a.mp4')).toBe(false);
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
  });

  it('previews HTML and SVG as text, never rendered', () => {
    expect(previewKind('page.html')).toBe('text');
    expect(previewKind('logo.svg')).toBe('text');
    expect(previewKind('photo.png')).toBe('image');
    expect(previewKind('song.flac')).toBe('audio');
    expect(previewKind('archive.zip')).toBe('none');
  });

  it('guesses share icons from their names', () => {
    expect(shareIcon('alex', 'alex')).toBe('user');
    expect(shareIcon('Family', 'alex')).toBe('users');
    expect(shareIcon('Public', 'alex')).toBe('globe');
    expect(shareIcon('Scans', 'alex')).toBe('printer');
    expect(shareIcon('Backup', 'alex')).toBe('drive');
  });
});
