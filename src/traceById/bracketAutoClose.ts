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

// Steps over an existing closing char instead of duplicating it, or inserts a matching pair with
// the caret in between. Returns undefined if the key isn't one of these bracket/quote chars.
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
