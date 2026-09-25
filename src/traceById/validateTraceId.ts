// Only the first token needs to look alphanumeric; anything after a space is Tempo's to validate.
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
