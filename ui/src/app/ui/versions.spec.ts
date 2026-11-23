import { fileTime } from './versions';

describe('fileTime', () => {
  it('is the interface language and a 24-hour clock, fit for a file name', () => {
    const t = new Date(2026, 10, 21, 16, 5);
    expect(fileTime(t, 'de')).toBe('21.11.2026 16.05');
    expect(fileTime(t, 'en-US')).toBe('11.21.2026 16.05');
    expect(fileTime(t, 'en-US')).not.toMatch(/[/:]|AM|PM/);
  });
});
