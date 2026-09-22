import { computeBracketAutoClose } from './bracketAutoClose';

describe('computeBracketAutoClose', () => {
  it.each([
    ['{', '', 0, '{}', 1],
    ['(', '', 0, '()', 1],
    ['"', '', 0, '""', 1],
    ["'", '', 0, "''", 1],
    ['{', 'status = error ', 15, 'status = error {}', 16],
  ])('inserts a closing %s pair', (key, value, caret, expectedValue, expectedCaret) => {
    expect(computeBracketAutoClose(value, caret, key)).toEqual({ value: expectedValue, caret: expectedCaret });
  });

  it.each([
    ['}', '{}', 1],
    [')', '()', 1],
    ['"', '""', 1],
    ["'", "''", 1],
  ])('steps over an existing %s instead of duplicating it', (key, value, caret) => {
    expect(computeBracketAutoClose(value, caret, key)).toEqual({ value, caret: caret + 1 });
  });

  it('does not step over a closing char that is not immediately at the caret', () => {
    expect(computeBracketAutoClose('{ }', 0, '}')).toBeUndefined();
  });

  it.each(['a', ' ', '=', '.', ':'])('leaves ordinary keys alone: %s', (key) => {
    expect(computeBracketAutoClose('{ status = error }', 5, key)).toBeUndefined();
  });
});
