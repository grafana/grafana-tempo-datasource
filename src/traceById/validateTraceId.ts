// Only the first whitespace-delimited token needs to look like a trace ID (letters and digits
// only) -- anything after a space is left for the backend to validate, since Tempo's own hex-only
// check (and whatever else is typed after it) is already enforced server-side.
const TRACE_ID_PATTERN = /^[A-Za-z0-9]+(\s.*)?$/;

export function validateTraceId(query: string): string | undefined {
  if (query.trim() === '') {
    return undefined;
  }

  if (!TRACE_ID_PATTERN.test(query)) {
    return 'Must start with a trace ID (letters and numbers only).';
  }

  return undefined;
}
