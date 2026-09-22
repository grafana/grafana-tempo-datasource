import { languageConfiguration } from '../traceql/traceql';

// Derived from the TraceQL tab's own Monaco autoClosingPairs config, so the two can't drift.
const AUTO_CLOSE_PAIRS: Record<string, string> = Object.fromEntries(
  (languageConfiguration.autoClosingPairs ?? []).map((pair) => [pair.open, pair.close])
);

const CLOSING_CHARS = new Set(Object.values(AUTO_CLOSE_PAIRS));

export interface BracketAutoCloseEdit {
  value: string;
  caret: number;
}

// Given a plain text input's current value and a collapsed caret position, decides what a
// bracket/quote keypress should do: step over an already-present closing character instead of
// duplicating it, or insert a matching pair with the caret left in between. Returns undefined for
// any key this behavior doesn't apply to, so the caller lets the keypress through normally.
export function computeBracketAutoClose(value: string, caret: number, key: string): BracketAutoCloseEdit | undefined {
  if (CLOSING_CHARS.has(key) && value[caret] === key) {
    return { value, caret: caret + 1 };
  }

  const closeChar = AUTO_CLOSE_PAIRS[key];
  if (closeChar) {
    return { value: value.slice(0, caret) + key + closeChar + value.slice(caret), caret: caret + 1 };
  }

  return undefined;
}
