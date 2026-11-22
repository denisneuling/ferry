import { Component, LOCALE_ID, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Api } from '../core/api';
import { Space } from './space';

@Component({
  imports: [Space],
  template: `<f-space [share]="share()" [path]="path()" />`,
})
class Host {
  readonly share = signal('alex');
  readonly path = signal('');
}

describe('Space', () => {
  let answer: (share: string, path: string) => Promise<{ total: number; free: number }>;
  let asked: string[];
  beforeEach(() => {
    asked = [];
    TestBed.configureTestingModule({
      providers: [
        { provide: LOCALE_ID, useValue: 'en' },
        {
          provide: Api,
          useValue: {
            space: (share: string, path: string) => {
              asked.push(`${share}/${path}`);
              return answer(share, path);
            },
          },
        },
      ],
    });
  });

  const render = async () => {
    const f = TestBed.createComponent(Host);
    f.detectChanges();
    await f.whenStable();
    f.detectChanges();
    return f;
  };

  it('says how much is free of how much, with a bar of what is used', async () => {
    answer = async () => ({ total: 18e12, free: 3.2e12 });
    const f = await render();
    const el = f.nativeElement as HTMLElement;
    expect(el.textContent).toContain('3.2 TB free of 18 TB');
    expect((el.querySelector('.space-bar i') as HTMLElement).style.width).toMatch(/^82\.2/);
  });

  it('shows nothing when the server does not answer', async () => {
    answer = async () => Promise.reject(new Error('refused'));
    const f = await render();
    expect((f.nativeElement as HTMLElement).querySelector('.space')).toBeNull();
  });

  it('asks again for another folder', async () => {
    answer = async () => ({ total: 100, free: 50 });
    const f = await render();
    f.componentInstance.path.set('Photos');
    f.detectChanges();
    await f.whenStable();
    expect(asked).toEqual(['alex/', 'alex/Photos']);
  });
});
