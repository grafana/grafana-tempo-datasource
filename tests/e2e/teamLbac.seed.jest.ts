import { ALLOWED_SERVICE, DENIED_SERVICE, buildOtlpTraces } from './teamLbac.seed';

type Built = {
  resourceSpans: Array<{
    resource: { attributes: Array<{ key: string; value: { stringValue: string } }> };
    scopeSpans: Array<{ spans: Array<{ traceId: string; spanId: string; startTimeUnixNano: string }> }>;
  }>;
};

describe('buildOtlpTraces', () => {
  const built = buildOtlpTraces(1_700_000_000_000) as Built;

  it('has spans for the allowed service and one other', () => {
    const services = built.resourceSpans.map((r) => r.resource.attributes[0].value.stringValue);
    expect(services).toEqual([ALLOWED_SERVICE, DENIED_SERVICE]);
  });

  it('uses distinct trace ids of 32 and span ids of 16 hex characters', () => {
    const spans = built.resourceSpans.flatMap((r) => r.scopeSpans[0].spans);
    expect(spans.length).toBeGreaterThan(0);
    expect(new Set(spans.map((s) => s.traceId)).size).toBe(spans.length);
    for (const span of spans) {
      expect(span.traceId).toMatch(/^[0-9a-f]{32}$/);
      expect(span.spanId).toMatch(/^[0-9a-f]{16}$/);
    }
  });

  it('writes the start time in nanoseconds as a string', () => {
    expect(built.resourceSpans[0].scopeSpans[0].spans[0].startTimeUnixNano).toBe('1700000000000000000');
  });
});
