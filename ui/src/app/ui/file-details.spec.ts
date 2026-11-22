import { Component, LOCALE_ID, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Api, FileMetadata } from '../core/api';
import { duration, FileDetails } from './file-details';

@Component({
  imports: [FileDetails],
  template: `<f-file-details [share]="'alex'" [path]="path()" />`,
})
class Host {
  readonly path = signal('IMG_3002.jpg');
}

describe('FileDetails', () => {
  let answer: () => Promise<FileMetadata>;
  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        { provide: Api, useValue: { metadata: () => answer() } },
      ],
    });
  });

  const rows = async () => {
    const f = TestBed.createComponent(Host);
    f.detectChanges();
    await f.whenStable();
    f.detectChanges();
    const el = f.nativeElement as HTMLElement;
    const dt = [...el.querySelectorAll('dt')].map((d) => d.textContent?.trim());
    const dd = [...el.querySelectorAll('dd')].map((d) => d.textContent?.trim());
    return { el, map: Object.fromEntries(dt.map((k, i) => [k, dd[i]])) };
  };

  it("labels a photo's details and keeps its words as text", async () => {
    answer = async () => ({
      kind: 'image',
      format: 'JPEG',
      width: 3024,
      height: 4032,
      camera: '<b>Fictional</b> Optics',
      taken: '2025-01-29T17:47:00+01:00',
      fNumber: 2.8,
      exposure: '1/500',
      iso: 800,
      focalLength: 70,
      orientation: 6,
      location: { latitude: 54.611667, longitude: 11.349444 },
    });
    const { el, map } = await rows();
    expect(map['Dimensions']).toBe('3,024 × 4,032 (12.2 MP)');
    expect(map['Exposure']).toBe('f/2.8 · 1/500 s · ISO 800 · 70 mm');
    expect(map['Orientation']).toBe('Turned 90° clockwise');
    expect(map['GPS position']).toBe('54.611667, 11.349444');
    expect(map['Camera']).toBe('<b>Fictional</b> Optics');
    expect(map['Taken']).toBe('Jan 29, 2025, 5:47 PM'); // the camera's clock, not the viewer's zone
    expect(el.querySelector('b')).toBeNull();
  });

  it('describes recordings and texts', async () => {
    answer = async () => ({
      kind: 'audio',
      durationSeconds: 187.4,
      bitrate: 320000,
      sampleRate: 44100,
      channels: 2,
      author: 'Die Fiktiven',
    });
    let { map } = await rows();
    expect(map).toMatchObject({
      Duration: '3:07',
      Bitrate: '320 kbit/s',
      'Sample rate': '44.1 kHz',
      Channels: 'Stereo',
      Artist: 'Die Fiktiven',
    });

    answer = async () => ({
      kind: 'text',
      encoding: '8bit',
      lines: 1200,
      linesAtLeast: true,
      lineEndings: 'CRLF',
    });
    ({ map } = await rows());
    expect(map['Lines']).toBe('at least 1,200 (CRLF)');
    expect(map['Encoding']).toContain('Not UTF-8');
  });

  it('shows nothing when the server cannot tell', async () => {
    answer = async () => Promise.reject(new Error('refused'));
    const { el } = await rows();
    expect(el.querySelector('h4')).toBeNull();
  });

  it('writes lengths as clocks do', () => {
    expect(duration(59.6)).toBe('1:00');
    expect(duration(3765)).toBe('1:02:45');
  });
});
