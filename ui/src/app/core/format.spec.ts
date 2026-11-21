import { LOCALE_ID } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Format } from './format';

describe('Format', () => {
  const make = (locale: string) => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [{ provide: LOCALE_ID, useValue: locale }] });
    return TestBed.inject(Format);
  };

  it('writes sizes in decimal units', () => {
    const f = make('en');
    expect(f.size(null)).toBe('—');
    expect(f.size(999)).toBe('999 B');
    expect(f.size(3_287_545)).toBe('3.3 MB');
    expect(f.size(1_610_612_736)).toBe('1.6 GB');
    expect(make('de').size(3_287_545)).toBe('3,3 MB');
  });

  it('says today and yesterday by time, older days by date', () => {
    const f = make('en');
    const now = new Date(2026, 10, 21, 18, 0);
    expect(f.date(new Date(2026, 10, 21, 9, 30).toISOString(), now)).toMatch(/^Today, /);
    expect(f.date(new Date(2026, 10, 20, 23, 0).toISOString(), now)).toMatch(/^Yesterday, /);
    expect(f.date(new Date(2026, 2, 4).toISOString(), now)).toBe('Mar 4');
    expect(f.date(new Date(2024, 2, 4).toISOString(), now)).toBe('Mar 4, 2024');
  });

  it('sorts names as people do', () => {
    const f = make('de');
    expect(['IMG_10', 'img_2', 'Äpfel', 'Zebra'].sort((a, b) => f.compare(a, b))).toEqual([
      'Äpfel',
      'img_2',
      'IMG_10',
      'Zebra',
    ]);
  });
});
