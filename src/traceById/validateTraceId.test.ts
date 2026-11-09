import { validateTraceId } from './validateTraceId';

describe('validateTraceId', () => {
  it.each([
    '',
    '   ',
    'abc123',
    '000000000000000060ba2abb44f13eae',
    'abc123 ',
    'abc123 hello world',
    'abc123 { status = error }',
    'not a trace id at all!', // first token "not" is alphanumeric -- everything after is unchecked
  ])('accepts valid query: %s', (query) => {
    expect(validateTraceId(query)).toBeUndefined();
  });

  it.each([' abc123', 'abc-123', '!abc123', '{ status = error }'])('rejects invalid query: %s', (query) => {
    expect(validateTraceId(query)).toEqual(expect.any(String));
  });
});
