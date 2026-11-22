import { ApiError } from './api';
import { describe as say } from './errors';

describe('error messages', () => {
  it('speak in everyday words, per code', () => {
    expect(say(new ApiError(409, 'busy'))).toContain('in use');
    expect(say(new ApiError(403, 'access_denied'))).toContain('does not allow');
    expect(say(new ApiError(0, 'network'))).toContain('cannot be reached');
  });

  it('never repeat what they were given', () => {
    expect(say(new Error('secret-path/with/name.txt'))).not.toContain('secret');
    expect(say(new ApiError(500, 'something new'))).toBe('Something went wrong.');
  });

  it('say which bound a too_large reached, per action', () => {
    const err = new ApiError(413, 'too_large', 200000);
    expect(say(err, 'list')).toContain('more than 200,000 entries');
    expect(say(err, 'delete')).toContain('in parts');
    expect(say(new ApiError(413, 'too_large', 4e9), 'upload')).toContain('4 GB');
    expect(say(new ApiError(413, 'too_large'), 'list')).toBe('This is larger than this ferry allows.');
  });
});
