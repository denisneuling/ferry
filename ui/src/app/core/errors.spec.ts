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
});
